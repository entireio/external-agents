package qwen

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sliceFromLine mirrors Entire's transcript.SliceFromLine
// (cmd/entire/cli/transcript/parse.go:130): return the bytes starting after the
// Nth newline, or nil when there aren't that many lines. This is what
// cmd/entire/cli/explain.go:1883 (scopeTranscriptForCheckpoint) and
// cmd/entire/cli/strategy/manual_commit_condensation.go:1006 apply to every
// agent that is not one of Entire's built-ins — Qwen included.
func sliceFromLine(content []byte, startLine int) []byte {
	if len(content) == 0 || startLine <= 0 {
		return content
	}
	count, offset := 0, 0
	for i, b := range content {
		if b == '\n' {
			count++
			if count == startLine {
				offset = i + 1
				break
			}
		}
	}
	if count < startLine || offset >= len(content) {
		return nil
	}
	return content[offset:]
}

// compactKind mirrors normalizeKind in Entire's
// cmd/entire/cli/transcript/compact/compact.go:197: the entry kind comes from
// the TOP-LEVEL "type", falling back to "role". A native sidecar record has
// neither — its discriminator is "event" — so every line was dropped.
func compactKind(line map[string]json.RawMessage) string {
	var kind string
	if json.Unmarshal(line["type"], &kind) == nil && kind != "" {
		return kind
	}
	if json.Unmarshal(line["role"], &kind) == nil {
		return kind
	}
	return ""
}

// compactMessageContent mirrors parseMessage (compact.go:617): content is read
// from a TOP-LEVEL "message" OBJECT. The sidecar's "message" was a STRING, so
// this half failed too.
func compactMessageContent(line map[string]json.RawMessage) []map[string]json.RawMessage {
	var msg map[string]json.RawMessage
	if json.Unmarshal(line["message"], &msg) != nil {
		return nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(msg["content"], &blocks) != nil {
		return nil
	}
	return blocks
}

func decodeLines(t *testing.T, data []byte) []map[string]json.RawMessage {
	t.Helper()
	var out []map[string]json.RawMessage
	for line := range bytes.SplitSeq(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("line is not valid JSON: %v (%s)", err, line)
		}
		out = append(out, m)
	}
	return out
}

func blockString(t *testing.T, block map[string]json.RawMessage, key string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(block[key], &s); err != nil {
		t.Fatalf("block[%q] = %s: %v", key, block[key], err)
	}
	return s
}

// hookSequence drives real hook payloads through ParseHook and returns the
// sidecar the agent wrote, so every assertion below exercises the live writer
// rather than a checked-in expected constant.
func hookSequence(t *testing.T, sessionID string, hooks ...[2]string) (*Agent, []byte) {
	t.Helper()
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	agent := New()
	for _, hook := range hooks {
		if _, err := agent.ParseHook(hook[0], []byte(hook[1])); err != nil {
			t.Fatalf("ParseHook(%s): %v", hook[0], err)
		}
	}
	data, err := os.ReadFile(agent.sidecarPath(sessionID))
	if err != nil {
		t.Fatal(err)
	}
	return agent, data
}

// A full turn: prompt, tool call, tool result, answer. Qwen fires PreToolUse
// before a call and PostToolUse after it, so the pair maps onto Entire's
// assistant tool_use / user tool_result and each hook still costs one line.
func fullTurn(t *testing.T) (*Agent, []byte) {
	t.Helper()
	return hookSequence(t, "qwen-shape",
		[2]string{HookNameUserPromptSubmit, `{"session_id":"qwen-shape","timestamp":"2026-05-20T12:00:00Z","prompt":"Create hello.txt"}`},
		[2]string{HookNamePreToolUse, `{"session_id":"qwen-shape","timestamp":"2026-05-20T12:00:01Z","tool_name":"write_file","tool_use_id":"tool-1","tool_input":{"file_path":"hello.txt"}}`},
		[2]string{HookNamePostToolUse, `{"session_id":"qwen-shape","timestamp":"2026-05-20T12:00:02Z","tool_name":"write_file","tool_use_id":"tool-1","tool_input":{"file_path":"hello.txt"},"tool_response":{"ok":true}}`},
		[2]string{HookNameStop, `{"session_id":"qwen-shape","timestamp":"2026-05-20T12:00:03Z","last_assistant_message":"Created hello.txt"}`},
	)
}

// The regression this change exists for. Both halves are asserted because
// satisfying only the first yields a correctly-sized transcript.jsonl full of
// empty content — the failure mode a byte count alone would call fixed.
func TestSidecarLinesSatisfyEntireCompactContract(t *testing.T) {
	_, data := fullTurn(t)
	lines := decodeLines(t, data)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}

	wantKind := []string{"user", "assistant", "user", "assistant"}
	for i, line := range lines {
		if got := compactKind(line); got != wantKind[i] {
			t.Fatalf("line %d: compactKind = %q, want %q — Entire drops this line", i, got, wantKind[i])
		}
		if len(compactMessageContent(line)) == 0 {
			t.Fatalf("line %d: no content under the %q wrapper — Entire emits an empty line", i, "message")
		}
	}
	if got := blockString(t, compactMessageContent(lines[0])[0], "text"); got != "Create hello.txt" {
		t.Fatalf("prompt text = %q", got)
	}
	if got := blockString(t, compactMessageContent(lines[3])[0], "text"); got != "Created hello.txt" {
		t.Fatalf("answer text = %q", got)
	}
}

// A mid-session slice must keep satisfying the contract: Entire compacts the
// scoped bytes, not the whole file.
func TestScopedSliceSatisfiesEntireCompactContract(t *testing.T) {
	_, data := fullTurn(t)
	lines := decodeLines(t, sliceFromLine(data, 2))
	if len(lines) != 2 {
		t.Fatalf("scoped slice has %d lines, want 2", len(lines))
	}
	for i, line := range lines {
		if compactKind(line) == "" {
			t.Fatalf("scoped line %d has no top-level type/role", i)
		}
		if len(compactMessageContent(line)) == 0 {
			t.Fatalf("scoped line %d carries no message content", i)
		}
	}
	if got := blockString(t, compactMessageContent(lines[1])[0], "text"); got != "Created hello.txt" {
		t.Fatalf("scoped answer text = %q", got)
	}
}

// tool_use keeps type/id/name/input (compact.go:704), and a user tool_result
// uses snake_case tool_use_id with a string "content" (compact.go:665) so
// Entire inlines the output into the preceding assistant's matching tool_use
// (inlineToolResults, compact.go:454).
func TestToolBlocksProjectToEntireShapes(t *testing.T) {
	_, data := fullTurn(t)
	lines := decodeLines(t, data)

	toolUse := compactMessageContent(lines[1])[0]
	for key, want := range map[string]string{"type": "tool_use", "id": "tool-1", "name": "write_file"} {
		if got := blockString(t, toolUse, key); got != want {
			t.Fatalf("tool_use %q = %q, want %q", key, got, want)
		}
	}
	var input map[string]any
	if err := json.Unmarshal(toolUse["input"], &input); err != nil {
		t.Fatalf("tool_use input = %s: %v", toolUse["input"], err)
	}
	if input["file_path"] != "hello.txt" {
		t.Fatalf("tool_use input = %+v", input)
	}

	toolResult := compactMessageContent(lines[2])[0]
	for key, want := range map[string]string{"type": "tool_result", "tool_use_id": "tool-1", "content": `{"ok":true}`} {
		if got := blockString(t, toolResult, key); got != want {
			t.Fatalf("tool_result %q = %q, want %q", key, got, want)
		}
	}
}

// A failed call must be flagged so Entire records the result as an error rather
// than a success.
func TestPostToolUseFailureIsFlaggedAsError(t *testing.T) {
	_, data := hookSequence(t, "qwen-fail",
		[2]string{HookNamePreToolUse, `{"session_id":"qwen-fail","timestamp":"2026-05-20T12:00:01Z","tool_name":"write_file","tool_use_id":"tool-1","tool_input":{"file_path":"hello.txt"}}`},
		[2]string{HookNamePostToolUseFailure, `{"session_id":"qwen-fail","timestamp":"2026-05-20T12:00:02Z","tool_name":"write_file","tool_use_id":"tool-1","error":"EACCES","error_details":"permission denied"}`},
	)
	block := compactMessageContent(decodeLines(t, data)[1])[0]
	if got := blockString(t, block, "content"); got != "EACCES permission denied" {
		t.Fatalf("tool_result content = %q", got)
	}
	var isError bool
	if err := json.Unmarshal(block["is_error"], &isError); err != nil || !isError {
		t.Fatalf("tool_result is_error = %s (%v)", block["is_error"], err)
	}
}

// A tool_response that is already a JSON string must reach Entire as that
// string, not as its quoted JSON text.
func TestStringToolResponseIsNotDoubleEncoded(t *testing.T) {
	_, data := hookSequence(t, "qwen-str",
		[2]string{HookNamePostToolUse, `{"session_id":"qwen-str","timestamp":"2026-05-20T12:00:02Z","tool_name":"read_file","tool_use_id":"tool-1","tool_response":"file contents"}`},
	)
	block := compactMessageContent(decodeLines(t, data)[0])[0]
	if got := blockString(t, block, "content"); got != "file contents" {
		t.Fatalf("tool_result content = %q", got)
	}
}

// get-transcript-position is what Entire records as the next checkpoint's start
// line, so it must equal the sidecar's line count.
func TestGetTranscriptPositionMatchesLineCount(t *testing.T) {
	agent, data := fullTurn(t)
	pos, err := agent.GetTranscriptPosition(agent.sidecarPath("qwen-shape"))
	if err != nil {
		t.Fatalf("GetTranscriptPosition: %v", err)
	}
	if lines := bytes.Count(data, []byte{'\n'}); pos != lines {
		t.Fatalf("position %d != newline count %d (Entire slices by line)", pos, lines)
	}
}

// The projection travels on the same record the adapter reads back. "message"
// carries an object now, so a record typed with a string "message" would fail
// to unmarshal and take the whole sidecar read with it.
func TestProjectedRecordsStillReadBack(t *testing.T) {
	agent, _ := fullTurn(t)
	ref := agent.sidecarPath("qwen-shape")

	records, err := readSidecarRecords(ref)
	if err != nil {
		t.Fatalf("readSidecarRecords: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("read %d records, want 4", len(records))
	}
	prompts, err := agent.ExtractPrompts(ref, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "Create hello.txt" {
		t.Fatalf("prompts = %#v", prompts)
	}
	files, _, err := agent.ExtractModifiedFiles(ref, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Fatalf("modified files = %#v", files)
	}
	summary, ok, err := agent.ExtractSummary(ref)
	if err != nil || !ok || summary != "Created hello.txt" {
		t.Fatalf("summary = (%q, %v, %v)", summary, ok, err)
	}
}

// A sidecar written before the projection existed carries neither key. It must
// still read, and later hooks append projected records alongside it.
func TestLegacySidecarWithoutProjectionStillReads(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	agent := New()
	ref := agent.sidecarPath("qwen-legacy")
	if err := os.MkdirAll(filepath.Dir(ref), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"v":1,"agent":"qwen","event":"UserPromptSubmit","session_id":"qwen-legacy","ts":"2026-05-20T12:00:00Z","prompt":"old prompt"}` + "\n"
	if err := os.WriteFile(ref, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ParseHook(HookNameStop, []byte(`{"session_id":"qwen-legacy","timestamp":"2026-05-20T12:00:01Z","last_assistant_message":"new answer"}`)); err != nil {
		t.Fatal(err)
	}

	prompts, err := agent.ExtractPrompts(ref, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "old prompt" {
		t.Fatalf("prompts = %#v", prompts)
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	lines := decodeLines(t, data)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if compactKind(lines[0]) != "" {
		t.Fatal("the legacy line must stay as written")
	}
	if compactKind(lines[1]) != "assistant" || len(compactMessageContent(lines[1])) == 0 {
		t.Fatalf("the appended line was not projected: %s", data)
	}
}

// Qwen's own notification text also lives under "message". It is only sent on
// events Entire has no representation for, so those records stay unprojected
// and the string is left exactly as it was written.
func TestNotificationMessageStaysAString(t *testing.T) {
	_, data := hookSequence(t, "qwen-note",
		[2]string{HookNameNotification, `{"session_id":"qwen-note","timestamp":"2026-05-20T12:00:00Z","notification_type":"info","message":"waiting for input"}`},
	)
	line := decodeLines(t, data)[0]
	var text string
	if err := json.Unmarshal(line["message"], &text); err != nil {
		t.Fatalf("message is no longer a string: %s (%v)", line["message"], err)
	}
	if text != "waiting for input" {
		t.Fatalf("message = %q", text)
	}
	if kind := compactKind(line); kind != "" {
		t.Fatalf("notification was projected as %q; Entire has no representation for it", kind)
	}
}

// Lifecycle events carry no conversation content, so they stay unprojected and
// Entire drops them rather than emitting empty lines.
func TestLifecycleEventsAreNotProjected(t *testing.T) {
	_, data := hookSequence(t, "qwen-life",
		[2]string{HookNameSessionStart, `{"session_id":"qwen-life","timestamp":"2026-05-20T12:00:00Z","source":"startup"}`},
		[2]string{HookNameUserPromptSubmit, `{"session_id":"qwen-life","timestamp":"2026-05-20T12:00:01Z","prompt":""}`},
		[2]string{HookNameSessionEnd, `{"session_id":"qwen-life","timestamp":"2026-05-20T12:00:02Z","reason":"exit"}`},
	)
	for i, line := range decodeLines(t, data) {
		if kind := compactKind(line); kind != "" {
			t.Fatalf("line %d projected as %q: %s", i, kind, line["event"])
		}
		if _, ok := line["message"]; ok {
			t.Fatalf("line %d carries a message wrapper: %s", i, line["event"])
		}
	}
}
