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
	f.addRaw(t, rolloutLine{Timestamp: "2026-11-16T05:59:59.000Z", Type: "session_meta", Payload: json.RawMessage(`{"id":"s-1","cwd":"/repo","source":"cli"}`)})
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

func TestCompactTranscript_ProjectsEntireWireShapes(t *testing.T) {
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
	if len(lines) != 6 {
		t.Fatalf("projected %d lines, want 6 (user, assistant, tool_use, tool_result, tool_use, assistant):\n%s", len(lines), decoded)
	}

	var first struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	unmarshalStrict(t, lines[0], &first)
	if first.Type != "user" || first.Message.Role != "user" {
		t.Errorf("first projected line = %+v, want the human user message", first)
	}
	if len(first.Message.Content) != 1 || first.Message.Content[0].Text != "Create hello.txt with the word hello" {
		t.Errorf("first projected content = %+v", first.Message.Content)
	}

	var toolUse struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type  string         `json:"type"`
				ID    string         `json:"id"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	unmarshalStrict(t, lines[2], &toolUse)
	if toolUse.Type != "assistant" || toolUse.Message.Role != "assistant" {
		t.Errorf("tool_use line kind = %s/%s", toolUse.Type, toolUse.Message.Role)
	}
	if len(toolUse.Message.Content) != 1 || toolUse.Message.Content[0].Type != "tool_use" {
		t.Fatalf("tool_use content = %+v", toolUse.Message.Content)
	}
	block := toolUse.Message.Content[0]
	if block.Name != "write_file" || block.ID != "call-1" {
		t.Errorf("tool_use block = %+v", block)
	}
	if block.Input["path"] != "hello.txt" {
		t.Errorf("tool_use input = %+v, want parsed arguments", block.Input)
	}

	var toolResult struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type      string `json:"type"`
				ToolUseID string `json:"tool_use_id"`
				Content   string `json:"content"`
			} `json:"content"`
		} `json:"message"`
	}
	unmarshalStrict(t, lines[3], &toolResult)
	if toolResult.Type != "user" {
		t.Errorf("tool_result line type = %s", toolResult.Type)
	}
	if len(toolResult.Message.Content) != 1 || toolResult.Message.Content[0].ToolUseID != "call-1" {
		t.Fatalf("tool_result content = %+v", toolResult.Message.Content)
	}
	if !contains(toolResult.Message.Content[0].Content, "Success") {
		t.Errorf("tool_result content = %q, want the raw output", toolResult.Message.Content[0].Content)
	}

	// The harness-context user line must be gone.
	for _, line := range lines {
		if contains(line, "environment_context") {
			t.Errorf("harness context leaked into the projected transcript: %s", line)
		}
	}
}
