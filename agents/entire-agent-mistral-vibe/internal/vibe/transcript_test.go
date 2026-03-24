package vibe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Golden JSONL data based on real Vibe session captures.
const goldenJSONL = `{"role":"user","content":"hello","message_id":"0801cd87-2d71-4a2d-8085-a3104102f506"}
{"role":"assistant","content":"Hello! How can I assist you today?","message_id":"61c28fc6-f14e-4370-886d-5ee3c2f5e87a"}
{"role":"user","content":"what is 2+2","message_id":"81b13e7a-036e-42a0-a9f4-d314660618a2"}
{"role":"assistant","content":"4","message_id":"13c64d77-0c40-4014-adc8-dfe58d81cf1c"}
{"role":"user","content":"can you create hello world golang","message_id":"5bf535d7-cb5c-45f6-8872-3ba26703475a"}
{"role":"assistant","tool_calls":[{"id":"g4WVM7SD2","index":0,"function":{"name":"write_file","arguments":"{\"path\":\"hello.go\",\"content\":\"package main\\n\\nimport \\\"fmt\\\"\\n\\nfunc main() {\\n\\tfmt.Println(\\\"Hello, World!\\\")\\n}\"}"},"type":"function"}],"message_id":"61dec2f6-920a-4c66-9b67-a92dbff14fb5"}
{"role":"tool","content":"path: /Users/test/hello.go\nbytes_written: 73","name":"write_file","tool_call_id":"g4WVM7SD2"}
{"role":"assistant","content":"Created hello.go with Hello World in Go.","message_id":"3cac263a-e70d-4cf9-b765-170f4d54a830"}
`

func writeGoldenJSONL(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "messages.jsonl")
	if err := os.WriteFile(path, []byte(goldenJSONL), 0o600); err != nil {
		t.Fatalf("write golden JSONL: %v", err)
	}
	return path
}

func TestGetTranscriptPosition(t *testing.T) {
	dir := t.TempDir()
	path := writeGoldenJSONL(t, dir)

	agent := New()
	pos, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if pos != 8 {
		t.Errorf("position = %d, want 8", pos)
	}
}

func TestGetTranscriptPosition_MissingFile(t *testing.T) {
	agent := New()
	pos, err := agent.GetTranscriptPosition("/nonexistent/path.jsonl")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if pos != 0 {
		t.Errorf("position for missing file = %d, want 0", pos)
	}
}

func TestGetTranscriptPosition_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(path, []byte(""), 0o600)

	agent := New()
	pos, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if pos != 0 {
		t.Errorf("position for empty file = %d, want 0", pos)
	}
}

func TestExtractModifiedFiles(t *testing.T) {
	dir := t.TempDir()
	path := writeGoldenJSONL(t, dir)

	agent := New()
	files, pos, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("files count = %d, want 1: %v", len(files), files)
	}
	if len(files) > 0 && files[0] != "hello.go" {
		t.Errorf("files[0] = %q, want %q", files[0], "hello.go")
	}
	if pos != 8 {
		t.Errorf("current_position = %d, want 8", pos)
	}
}

func TestExtractModifiedFiles_WithOffset(t *testing.T) {
	dir := t.TempDir()
	path := writeGoldenJSONL(t, dir)

	agent := New()
	// Offset 7 skips past the tool call line
	files, _, err := agent.ExtractModifiedFiles(path, 7)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files count with offset 7 = %d, want 0: %v", len(files), files)
	}
}

func TestExtractModifiedFiles_MissingFile(t *testing.T) {
	agent := New()
	files, pos, err := agent.ExtractModifiedFiles("/nonexistent.jsonl", 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(files) != 0 || pos != 0 {
		t.Errorf("files=%v, pos=%d for missing file", files, pos)
	}
}

func TestExtractPrompts(t *testing.T) {
	dir := t.TempDir()
	path := writeGoldenJSONL(t, dir)

	agent := New()
	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(prompts) != 3 {
		t.Errorf("prompts count = %d, want 3: %v", len(prompts), prompts)
	}
	expected := []string{"hello", "what is 2+2", "can you create hello world golang"}
	for i, want := range expected {
		if i < len(prompts) && prompts[i] != want {
			t.Errorf("prompts[%d] = %q, want %q", i, prompts[i], want)
		}
	}
}

func TestExtractPrompts_WithOffset(t *testing.T) {
	dir := t.TempDir()
	path := writeGoldenJSONL(t, dir)

	agent := New()
	// Offset 4 skips first 4 lines (user, assistant, user, assistant)
	prompts, err := agent.ExtractPrompts(path, 4)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(prompts) != 1 {
		t.Errorf("prompts count = %d, want 1: %v", len(prompts), prompts)
	}
}

func TestExtractPrompts_MissingFile(t *testing.T) {
	agent := New()
	prompts, err := agent.ExtractPrompts("/nonexistent.jsonl", 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(prompts) != 0 {
		t.Errorf("prompts for missing file = %v", prompts)
	}
}

func TestExtractSummary(t *testing.T) {
	dir := t.TempDir()
	path := writeGoldenJSONL(t, dir)

	agent := New()
	summary, hasSummary, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !hasSummary {
		t.Error("has_summary should be true")
	}
	if summary != "Created hello.go with Hello World in Go." {
		t.Errorf("summary = %q, want %q", summary, "Created hello.go with Hello World in Go.")
	}
}

func TestExtractSummary_NoAssistantMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user-only.jsonl")
	os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o600)

	agent := New()
	summary, hasSummary, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if hasSummary {
		t.Error("has_summary should be false for user-only transcript")
	}
	if summary != "" {
		t.Errorf("summary = %q, want empty", summary)
	}
}

func TestExtractSummary_MissingFile(t *testing.T) {
	agent := New()
	_, hasSummary, err := agent.ExtractSummary("/nonexistent.jsonl")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if hasSummary {
		t.Error("has_summary should be false for missing file")
	}
}

func TestExtractVibeFilePath(t *testing.T) {
	tests := []struct {
		name     string
		argsStr  string
		want     string
	}{
		{"path_key", `{"path":"hello.go","content":"test"}`, "hello.go"},
		{"file_path_key", `{"file_path":"/src/main.py"}`, "/src/main.py"},
		{"empty_args", ``, ""},
		{"no_path_keys", `{"content":"test"}`, ""},
		{"malformed_json", `not json`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractVibeFilePath(tc.argsStr)
			if got != tc.want {
				t.Errorf("extractVibeFilePath(%q) = %q, want %q", tc.argsStr, got, tc.want)
			}
		})
	}
}

func TestIsVibeFileModificationTool(t *testing.T) {
	for _, tool := range []string{"write_file", "search_replace", "create_file", "edit_file"} {
		if !isVibeFileModificationTool(tool) {
			t.Errorf("%q should be a file modification tool", tool)
		}
	}
	for _, tool := range []string{"bash", "grep", "read_file", "web_search"} {
		if isVibeFileModificationTool(tool) {
			t.Errorf("%q should not be a file modification tool", tool)
		}
	}
}

func TestReadTranscript(t *testing.T) {
	dir := t.TempDir()
	path := writeGoldenJSONL(t, dir)

	agent := New()
	data, err := agent.ReadTranscript(path)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Error("transcript should contain 'hello'")
	}
}
