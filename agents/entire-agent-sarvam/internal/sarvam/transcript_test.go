package sarvam

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rolloutFixture builds a synthetic rollout JSONL from the shapes verified
// against live sarvam-code sessions. Lines are marshaled from typed values,
// so the fixture can never drift into invalid JSON.
type rolloutFixture struct {
	lines []string
	// tokenTotals accumulates the running total_token_usage across the
	// tokenCount events appended so far, mirroring how sarvam records the
	// cumulative total alongside each per-call last_token_usage.
	tokenTotals rolloutTokenUsage
}

func (f *rolloutFixture) addRaw(t *testing.T, value any) *rolloutFixture {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture line: %v", err)
	}
	f.lines = append(f.lines, string(encoded))
	return f
}

func (f *rolloutFixture) user(t *testing.T, text string) *rolloutFixture {
	return f.addRaw(t, rolloutLine{
		Timestamp: "2026-11-16T06:00:00.000Z",
		Type:      "response_item",
		Payload:   mustMarshal(t, rolloutMessage{Type: "message", Role: "user", Content: []rolloutContentBlock{{Type: "input_text", Text: text}}}),
	})
}

func (f *rolloutFixture) assistant(t *testing.T, text string) *rolloutFixture {
	return f.addRaw(t, rolloutLine{
		Timestamp: "2026-11-16T06:00:01.000Z",
		Type:      "response_item",
		Payload:   mustMarshal(t, rolloutMessage{Type: "message", Role: "assistant", Content: []rolloutContentBlock{{Type: "output_text", Text: text}}}),
	})
}

func (f *rolloutFixture) functionCall(t *testing.T, name, callID, args string) *rolloutFixture {
	return f.addRaw(t, rolloutLine{
		Timestamp: "2026-11-16T06:00:02.000Z",
		Type:      "response_item",
		Payload:   mustMarshal(t, rolloutFunctionCall{Type: "function_call", Name: name, CallID: callID, Arguments: args}),
	})
}

func (f *rolloutFixture) callOutput(t *testing.T, callID, output string) *rolloutFixture {
	return f.addRaw(t, rolloutLine{
		Timestamp: "2026-11-16T06:00:02.500Z",
		Type:      "response_item",
		Payload:   mustMarshal(t, rolloutFunctionCallOutput{Type: "function_call_output", CallID: callID, Output: output}),
	})
}

// tokenCount appends a token_count event carrying the given per-call usage.
// Live rollouts record that usage under last_token_usage and the running sum
// of all calls under total_token_usage, with input_tokens inclusive of
// cached_input_tokens in both; the fixture reproduces that shape.
func (f *rolloutFixture) tokenCount(t *testing.T, input, cached, output int) *rolloutFixture {
	t.Helper()
	last := rolloutTokenUsage{InputTokens: input, CachedInputTokens: cached, OutputTokens: output, TotalTokens: input + cached + output}
	f.tokenTotals.InputTokens += last.InputTokens
	f.tokenTotals.CachedInputTokens += last.CachedInputTokens
	f.tokenTotals.OutputTokens += last.OutputTokens
	f.tokenTotals.TotalTokens += last.TotalTokens
	return f.addRaw(t, rolloutLine{
		Timestamp: "2026-11-16T06:00:03.000Z",
		Type:      "event_msg",
		Payload: mustMarshal(t, rolloutTokenCount{
			Type: "token_count",
			Info: &rolloutTokenInfo{TotalTokenUsage: f.tokenTotals, LastTokenUsage: last},
		}),
	})
}

func (f *rolloutFixture) emptyTokenCount(t *testing.T) *rolloutFixture {
	return f.addRaw(t, rolloutLine{
		Timestamp: "2026-11-16T06:00:03.100Z",
		Type:      "event_msg",
		Payload:   mustMarshal(t, rolloutTokenCount{Type: "token_count", Info: nil}),
	})
}

func (f *rolloutFixture) write(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout-test.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(f.lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture payload: %v", err)
	}
	return json.RawMessage(encoded)
}

func sampleRollout(t *testing.T) string {
	t.Helper()
	f := &rolloutFixture{}
	f.addRaw(t, rolloutLine{Timestamp: "2026-11-16T05:59:59.000Z", Type: "session_meta", Payload: json.RawMessage(`{"id":"s-1","cwd":"/repo","source":"cli","cli_version":"0.58.0"}`)})
	f.user(t, "<environment_context>\n  <cwd>/repo</cwd>\n</environment_context>")
	f.user(t, "Create hello.txt with the word hello")
	f.assistant(t, "I'll create the file now.")
	f.functionCall(t, "write_file", "call-1", `{"contents":"hello","path":"hello.txt"}`)
	f.callOutput(t, "call-1", `{"output":"Success","metadata":{"exit_code":0}}`)
	f.functionCall(t, "exec_command", "call-2", `{"cmd":"echo x > notes.md && touch readme.md","command":"echo x > notes.md && touch readme.md"}`)
	f.assistant(t, "done")
	return f.write(t)
}

func TestGetTranscriptPosition(t *testing.T) {
	agent := New()
	path := sampleRollout(t)

	position, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	if position != 8 {
		t.Fatalf("position = %d, want 8 (one per physical line)", position)
	}

	position, err = agent.GetTranscriptPosition(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || position != 0 {
		t.Fatalf("missing file: got (%d, %v), want (0, nil)", position, err)
	}
}

func TestExtractModifiedFiles(t *testing.T) {
	agent := New()
	path := sampleRollout(t)

	files, position, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if position != 8 {
		t.Errorf("current_position = %d, want 8", position)
	}
	want := []string{"hello.txt", "notes.md", "readme.md"}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for i, file := range want {
		if files[i] != file {
			t.Errorf("files[%d] = %q, want %q", i, files[i], file)
		}
	}

	// Offset at the exec_command call leaves only its files.
	files, _, err = agent.ExtractModifiedFiles(path, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != "notes.md" || files[1] != "readme.md" {
		t.Fatalf("files from offset 6 = %v, want [notes.md readme.md]", files)
	}

	// Offset beyond the transcript yields nothing.
	files, _, err = agent.ExtractModifiedFiles(path, 999)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("files from offset 999 = %v, want none", files)
	}
}

func TestExtractPrompts_SkipsHarnessContext(t *testing.T) {
	agent := New()
	path := sampleRollout(t)

	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "Create hello.txt with the word hello" {
		t.Fatalf("prompts = %v, want the single human prompt", prompts)
	}
}

func TestExtractSummary_LastAssistantMessage(t *testing.T) {
	agent := New()
	path := sampleRollout(t)

	summary, has, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if !has || summary != "done" {
		t.Fatalf("summary = (%q, %v), want (\"done\", true)", summary, has)
	}
}

func TestCalculateTokens(t *testing.T) {
	agent := New()
	f := &rolloutFixture{}
	f.tokenCount(t, 1000, 500, 200)
	f.emptyTokenCount(t)
	f.tokenCount(t, 3000, 1500, 800)
	path := f.write(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	usage, err := agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	// input_tokens is inclusive of cached_input_tokens in the rollout; the
	// response reports the non-cached share (4000 - 2000) as input.
	if usage.InputTokens != 2000 {
		t.Errorf("input_tokens = %d, want 2000 (4000 total input minus 2000 cached)", usage.InputTokens)
	}
	if usage.CacheReadTokens != 2000 {
		t.Errorf("cache_read_tokens = %d, want 2000", usage.CacheReadTokens)
	}
	if usage.OutputTokens != 1000 {
		t.Errorf("output_tokens = %d, want 1000", usage.OutputTokens)
	}
	if usage.APICallCount != 2 {
		t.Errorf("api_call_count = %d, want 2 (null-info events do not count)", usage.APICallCount)
	}

	// A line-index offset skips the first line's API call — the unit the
	// analyzer reports and Entire feeds back; a byte offset would recount it.
	// The skipped line still establishes the cumulative total the window's
	// deltas are measured against.
	usage, err = agent.CalculateTokens(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 1500 {
		t.Errorf("input_tokens from offset = %d, want 1500 (3000 minus the 1500 new cached)", usage.InputTokens)
	}
	if usage.CacheReadTokens != 1500 {
		t.Errorf("cache_read_tokens from offset = %d, want 1500", usage.CacheReadTokens)
	}
	if usage.OutputTokens != 800 {
		t.Errorf("output_tokens from offset = %d, want 800 (only the second call's output)", usage.OutputTokens)
	}
	if usage.APICallCount != 1 {
		t.Errorf("api_call_count from offset = %d, want 1", usage.APICallCount)
	}

	// An offset at or past the line count counts nothing.
	usage, err = agent.CalculateTokens(data, len(f.lines))
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 0 || usage.APICallCount != 0 {
		t.Errorf("tokens past the transcript = %+v, want zeros", usage)
	}
}

func TestCalculateTokens_RepeatedTotalsCountedOnce(t *testing.T) {
	// Sarvam re-emits token_count events whose cumulative total repeats the
	// previous event's (432 of 911 in one sampled live session). Summing
	// per-event usage doubles the true totals, so only advancing totals
	// count.
	agent := New()
	f := &rolloutFixture{}
	f.tokenCount(t, 1000, 500, 200)
	f.lines = append(f.lines, f.lines[len(f.lines)-1])
	f.tokenCount(t, 3000, 1500, 800)
	path := f.write(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	usage, err := agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 2000 || usage.CacheReadTokens != 2000 || usage.OutputTokens != 1000 {
		t.Errorf("usage = %+v, want the same totals as without the repeated event (2000/2000/1000)", usage)
	}
	if usage.APICallCount != 2 {
		t.Errorf("api_call_count = %d, want 2 (the repeated event is not an API call)", usage.APICallCount)
	}
}

func TestCalculateTokens_TornFinalTokenCount(t *testing.T) {
	// A complete token_count record whose trailing newline has not landed
	// yet is outside the transcript position; counting it now would recount
	// it once the newline arrives.
	agent := New()
	f := &rolloutFixture{}
	f.tokenCount(t, 1000, 500, 200)
	path := f.write(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	torn := bytes.TrimSuffix(data, []byte("\n"))
	usage, err := agent.CalculateTokens(torn, 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 0 || usage.CacheReadTokens != 0 || usage.OutputTokens != 0 || usage.APICallCount != 0 {
		t.Errorf("usage from torn final line = %+v, want zeros (the line is not yet consumed)", usage)
	}

	usage, err = agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 500 || usage.CacheReadTokens != 500 || usage.OutputTokens != 200 || usage.APICallCount != 1 {
		t.Errorf("usage after completion = %+v, want 500/500/200 with one API call", usage)
	}
}

func TestCalculateTokens_OversizeRecord(t *testing.T) {
	// One record can embed a whole tool output. A fixed per-line cap made
	// calculate-tokens fail on the whole checkpoint — with bufio.ErrTooLong —
	// while every other reader handled the file, so the scanner grows the
	// way readRolloutLines does.
	agent := New()
	f := &rolloutFixture{}
	f.tokenCount(t, 1000, 500, 200)
	f.lines[len(f.lines)-1] = strings.TrimSuffix(f.lines[len(f.lines)-1], "}") + `,"padding":"` + strings.Repeat("x", 11*1024*1024) + `"}`
	path := f.write(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	usage, err := agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatalf("an oversize record must not fail the scan: %v", err)
	}
	if usage.InputTokens != 500 || usage.CacheReadTokens != 500 || usage.OutputTokens != 200 || usage.APICallCount != 1 {
		t.Errorf("usage = %+v, want 500/500/200 with one API call", usage)
	}
}

func TestCompactTranscript_KeepsUnterminatedFinalLine(t *testing.T) {
	// The compactor's input is a finished snapshot of the redacted
	// transcript; redaction does not guarantee the trailing newline
	// survives. Dropping the final line — usually the last assistant reply —
	// would cut the checkpoint transcript short, so an unterminated final
	// line that parses is kept. The live-transcript readers stay strict.
	agent := New()
	f := &rolloutFixture{}
	f.user(t, "Create hello.txt")
	f.assistant(t, "done")
	content := strings.Join(f.lines, "\n") // no trailing newline
	path := filepath.Join(t.TempDir(), "snapshot.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Position still excludes the unterminated line: that rule belongs to
	// the live offsets.
	position, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	if position != 1 {
		t.Fatalf("position = %d, want 1 (the live rule still skips the torn line)", position)
	}

	resp, err := agent.CompactTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64Decode(resp.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(string(decoded))
	if len(lines) != 2 {
		t.Fatalf("projected %d lines, want both the user prompt and the final assistant reply:\n%s", len(lines), decoded)
	}
	var last struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	unmarshalStrict(t, lines[1], &last)
	if last.Type != "assistant" || len(last.Content) != 1 ||
		last.Content[0].Type != "text" || last.Content[0].Text != "done" {
		t.Errorf("final projected line = %+v, want the unterminated assistant reply", last)
	}
}

func TestReadRolloutLines_TornFinalLine(t *testing.T) {
	agent := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "torn.jsonl")

	f := &rolloutFixture{}
	f.user(t, "Create hello.txt")
	promptLine := f.lines[0]
	// A record torn mid-write: no trailing newline, truncated JSON.
	torn := `{"timestamp":"2026-11-17T08:00:00.000Z","type":"response_item","payload":{"type":"mess`

	if err := os.WriteFile(path, []byte(promptLine+"\n"+torn), 0o600); err != nil {
		t.Fatal(err)
	}

	position, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	if position != 1 {
		t.Fatalf("position = %d, want 1 (a torn final line is not consumed)", position)
	}
	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 {
		t.Fatalf("prompts = %v, want only the complete line's prompt", prompts)
	}

	// The line completes; reading on from the saved position must see it.
	completed := torn + `age","role":"user","content":[{"type":"input_text","text":"Follow up"}]}}`
	if err := os.WriteFile(path, []byte(promptLine+"\n"+completed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompts, err = agent.ExtractPrompts(path, position)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "Follow up" {
		t.Fatalf("prompts from the saved offset = %v, want the completed line's prompt", prompts)
	}
}

func TestExtractModifiedFiles_DeviceRedirects(t *testing.T) {
	agent := New()
	f := &rolloutFixture{}
	f.functionCall(t, "exec_command", "call-1", `{"cmd":"echo hi > /dev/null && echo x > out.txt 2>/dev/null && touch /dev/null"}`)
	path := f.write(t)

	files, _, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "out.txt" {
		t.Fatalf("files = %v, want only out.txt (/dev/* is not a workspace file)", files)
	}
}

func TestExtractModifiedFiles_AppendRedirects(t *testing.T) {
	// ">>" must match as the append form — with (>|>>) the alternation
	// would capture ">" or ">file" and never report the appended file. The
	// anchor still requires start-of-line or whitespace before the redirect;
	// redirects attached directly after a word (y>>f) stay unmatched rather
	// than risk arithmetic-comparison false positives.
	agent := New()
	f := &rolloutFixture{}
	f.functionCall(t, "exec_command", "call-1", `{"cmd":"echo x >> append.log && echo y >>compact.log && printf z >> spaced.out"}`)
	path := f.write(t)

	files, _, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"append.log", "compact.log", "spaced.out"}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for i, file := range want {
		if files[i] != file {
			t.Errorf("files[%d] = %q, want %q", i, files[i], file)
		}
	}
}

func TestExtractModifiedFiles_ExecCommandKeyShapes(t *testing.T) {
	// Live rollouts carry the shell under "cmd"; older releases used
	// "command". Both must resolve to the same redirect extraction.
	agent := New()
	f := &rolloutFixture{}
	f.functionCall(t, "exec_command", "call-1", `{"cmd":"echo x > current.txt"}`)
	f.functionCall(t, "exec_command", "call-2", `{"command":"echo y > legacy.txt"}`)
	path := f.write(t)

	files, _, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"current.txt", "legacy.txt"}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for i, file := range want {
		if files[i] != file {
			t.Errorf("files[%d] = %q, want %q", i, files[i], file)
		}
	}
}

func TestExtractModifiedFiles_ReadToolsAreNotWrites(t *testing.T) {
	// A read is not a modification: read_file and read-only shell commands
	// must not report their targets. The generic path-key fallback used to
	// leak read_file's "path" into the modified-file set.
	agent := New()
	f := &rolloutFixture{}
	f.user(t, "Read the notes")
	f.functionCall(t, "read_file", "call-1", `{"path":"notes.md"}`)
	f.callOutput(t, "call-1", `{"output":"contents"}`)
	f.functionCall(t, "exec_command", "call-2", `{"cmd":"cat notes.md"}`)
	f.functionCall(t, "fetch_url", "call-3", `{"urls":["https://entire.com/docs"]}`)
	path := f.write(t)

	files, _, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none (reads are not modifications)", files)
	}
}

func TestCompactTranscript_ProjectsUnifiedRecords(t *testing.T) {
	// The server's unified gate (isUnifiedTranscript) admits the file only
	// when the first line carries a top-level user/assistant type and a
	// top-level content array — the check that rejected our earlier
	// {"type","message":{…}} projection and left the session page empty.
	agent := New()
	path := sampleRollout(t)

	resp, err := agent.CompactTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64Decode(resp.Transcript)
	if err != nil {
		t.Fatal(err)
	}

	lines := splitLines(string(decoded))
	if len(lines) != 5 {
		t.Fatalf("projected %d lines, want 5 (user prompt, assistant preamble, two tool_use records, final assistant reply):\n%s", len(lines), decoded)
	}

	var first struct {
		V       int    `json:"v"`
		Agent   string `json:"agent"`
		Version string `json:"cli_version"`
		Type    string `json:"type"`
		TS      string `json:"ts"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	unmarshalStrict(t, lines[0], &first)
	if first.V != 1 || first.Agent != AgentName || first.Type != "user" || first.TS == "" {
		t.Errorf("first record = %+v, want a v:1 sarvam user line with a timestamp", first)
	}
	if first.Version != "0.58.0" {
		t.Errorf("cli_version = %q, want the session_meta version", first.Version)
	}
	if len(first.Content) != 1 || first.Content[0].Text != "Create hello.txt with the word hello" {
		t.Errorf("first content = %+v, want the human prompt as [{text}]", first.Content)
	}

	var preamble struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	unmarshalStrict(t, lines[1], &preamble)
	if preamble.Type != "assistant" || len(preamble.Content) != 1 ||
		preamble.Content[0].Type != "text" || preamble.Content[0].Text != "I'll create the file now." {
		t.Errorf("assistant preamble = %+v, want the output_text projected as text", preamble)
	}

	type toolUseRecord struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Content []struct {
			Type   string         `json:"type"`
			ID     string         `json:"id"`
			Name   string         `json:"name"`
			Input  map[string]any `json:"input"`
			Result *struct {
				Output string `json:"output"`
				Status string `json:"status"`
			} `json:"result"`
		} `json:"content"`
	}
	var writeCall toolUseRecord
	unmarshalStrict(t, lines[2], &writeCall)
	if writeCall.Type != "assistant" || writeCall.ID != "call-1" {
		t.Errorf("write_file record = %+v, want the assistant tool_use line keyed by call id", writeCall)
	}
	if len(writeCall.Content) != 1 || writeCall.Content[0].Type != "tool_use" || writeCall.Content[0].Name != "write_file" {
		t.Fatalf("write_file block = %+v", writeCall.Content)
	}
	if writeCall.Content[0].Input["path"] != "hello.txt" {
		t.Errorf("write_file input = %+v, want parsed arguments", writeCall.Content[0].Input)
	}
	if writeCall.Content[0].Result == nil || writeCall.Content[0].Result.Output != "Success" || writeCall.Content[0].Result.Status != "success" {
		t.Errorf("write_file result = %+v, want the call output nested with success status", writeCall.Content[0].Result)
	}

	// The exec_command call has no output line in the fixture, so its
	// tool_use block carries no nested result. A fresh variable: json
	// merges into an existing map, so reusing writeCall would keep
	// call-1's input and result.
	var execCall toolUseRecord
	unmarshalStrict(t, lines[3], &execCall)
	if execCall.Content[0].Name != "exec_command" || execCall.Content[0].Result != nil {
		t.Errorf("exec_command block = %+v, want no nested result without an output record", execCall.Content[0])
	}

	var done struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	unmarshalStrict(t, lines[4], &done)
	if done.Type != "assistant" || len(done.Content) != 1 || done.Content[0].Text != "done" {
		t.Errorf("final record = %+v, want the closing assistant reply", done)
	}

	// The harness-context user line must be gone.
	for _, line := range lines {
		if contains(line, "environment_context") {
			t.Errorf("harness context leaked into the compacted transcript: %s", line)
		}
	}
}

func TestCompactTranscript_ToolResultStatus(t *testing.T) {
	// A non-zero exec exit code is reported the way qwen's compactor
	// reports it — empty output included, so a failed `false` or a silent
	// grep miss is not read as success — and a non-envelope output string
	// is kept verbatim.
	agent := New()
	f := &rolloutFixture{}
	f.functionCall(t, "exec_command", "call-1", `{"cmd":"make test"}`)
	f.callOutput(t, "call-1", `{"output":"make: *** [test] Error 1","metadata":{"exit_code":2}}`)
	f.functionCall(t, "exec_command", "call-2", `{"cmd":"echo hi"}`)
	f.callOutput(t, "call-2", "plain text output")
	f.functionCall(t, "exec_command", "call-3", `{"cmd":"false"}`)
	f.callOutput(t, "call-3", `{"output":"","metadata":{"exit_code":1}}`)
	path := f.write(t)

	resp, err := agent.CompactTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64Decode(resp.Transcript)
	if err != nil {
		t.Fatal(err)
	}

	lines := splitLines(string(decoded))
	if len(lines) != 3 {
		t.Fatalf("projected %d lines, want the three tool_use records:\n%s", len(lines), decoded)
	}
	var record struct {
		Content []struct {
			Result *struct {
				Output string `json:"output"`
				Status string `json:"status"`
			} `json:"result"`
		} `json:"content"`
	}
	unmarshalStrict(t, lines[0], &record)
	if len(record.Content) != 1 || record.Content[0].Result == nil ||
		record.Content[0].Result.Status != "error" || record.Content[0].Result.Output != "make: *** [test] Error 1" {
		t.Errorf("failed call result = %+v, want error status with the output text", record.Content[0].Result)
	}
	unmarshalStrict(t, lines[1], &record)
	if len(record.Content) != 1 || record.Content[0].Result == nil ||
		record.Content[0].Result.Status != "success" || record.Content[0].Result.Output != "plain text output" {
		t.Errorf("plain output result = %+v, want success with the raw string", record.Content[0].Result)
	}
	unmarshalStrict(t, lines[2], &record)
	if len(record.Content) != 1 || record.Content[0].Result == nil ||
		record.Content[0].Result.Status != "error" || record.Content[0].Result.Output != "" {
		t.Errorf("silent failure result = %+v, want error status with empty output, not the raw envelope", record.Content[0].Result)
	}
}

func TestCompactTranscript_NoProjectableContentErrors(t *testing.T) {
	// Mirrors qwen's compactor: an empty projection is an error, so the
	// CLI stores no transcript.jsonl rather than a file the server's gate
	// would reject.
	agent := New()
	f := &rolloutFixture{}
	f.addRaw(t, rolloutLine{Timestamp: "2026-11-16T06:00:00.000Z", Type: "session_meta", Payload: json.RawMessage(`{"id":"s-1","cwd":"/repo","source":"cli"}`)})
	f.addRaw(t, rolloutLine{Timestamp: "2026-11-16T06:00:01.000Z", Type: "event_msg", Payload: json.RawMessage(`{"type":"token_count"}`)})
	path := f.write(t)

	if _, err := agent.CompactTranscript(path); err == nil {
		t.Fatal("expected an error when nothing is projectable")
	}
}
