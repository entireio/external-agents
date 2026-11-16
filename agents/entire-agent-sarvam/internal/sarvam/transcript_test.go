package sarvam

import (
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

func (f *rolloutFixture) tokenCount(t *testing.T, input, cached, output int) *rolloutFixture {
	usage := rolloutTokenUsage{InputTokens: input, CachedInputTokens: cached, OutputTokens: output, TotalTokens: input + cached + output}
	return f.addRaw(t, rolloutLine{
		Timestamp: "2026-11-16T06:00:03.000Z",
		Type:      "event_msg",
		Payload: mustMarshal(t, rolloutTokenCount{
			Type: "token_count",
			Info: &rolloutTokenInfo{TotalTokenUsage: usage, LastTokenUsage: usage},
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
	if usage.InputTokens != 4000 {
		t.Errorf("input_tokens = %d, want 4000", usage.InputTokens)
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

	// Byte offset after the first line skips that API call.
	offset := len(f.lines[0]) + 1
	usage, err = agent.CalculateTokens(data, offset)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 3000 {
		t.Errorf("input_tokens from offset = %d, want 3000", usage.InputTokens)
	}
	if usage.APICallCount != 1 {
		t.Errorf("api_call_count from offset = %d, want 1", usage.APICallCount)
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
