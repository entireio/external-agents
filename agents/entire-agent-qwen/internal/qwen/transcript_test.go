package qwen

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A single Qwen hook record is not bounded by 64 KB: tool_response for a large
// file read, or tool_input for a large write, routinely exceeds it. Every
// transcript operation must still see the whole session.
func TestSidecarRecordLargerThanScannerDefault(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()

	big, err := json.Marshal(strings.Repeat("x", 128*1024))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []string{
		`{"session_id":"big","timestamp":"2026-05-20T12:00:00Z","prompt":"Read the file"}`,
		`{"session_id":"big","timestamp":"2026-05-20T12:00:01Z","tool_name":"write_file","tool_use_id":"tool-1","tool_input":{"file_path":"hello.txt"},"tool_response":` + string(big) + `}`,
		`{"session_id":"big","timestamp":"2026-05-20T12:00:02Z","last_assistant_message":"done"}`,
	}
	hooks := []string{HookNameUserPromptSubmit, HookNamePostToolUse, HookNameStop}
	for i, hook := range hooks {
		if _, err := agent.ParseHook(hook, []byte(inputs[i])); err != nil {
			t.Fatalf("ParseHook(%s): %v", hook, err)
		}
	}

	sessionRef := agent.sidecarPath("big")

	position, err := agent.GetTranscriptPosition(sessionRef)
	if err != nil {
		t.Fatalf("GetTranscriptPosition: %v", err)
	}
	if position != 3 {
		t.Fatalf("expected 3 records, got %d", position)
	}

	files, current, err := agent.ExtractModifiedFiles(sessionRef, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles: %v", err)
	}
	if current != 3 {
		t.Fatalf("expected current position 3, got %d", current)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Fatalf("unexpected modified files: %#v", files)
	}

	prompts, err := agent.ExtractPrompts(sessionRef, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts: %v", err)
	}
	if len(prompts) != 1 || prompts[0] != "Read the file" {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}

	summary, ok, err := agent.ExtractSummary(sessionRef)
	if err != nil {
		t.Fatalf("ExtractSummary: %v", err)
	}
	if !ok || summary != "done" {
		t.Fatalf("unexpected summary %q ok=%v", summary, ok)
	}
}

// The sidecar is append-only and is read while Qwen is still running, so a
// torn final line is normal. It must not destroy the records already written.
func TestSidecarSkipsUnparseableLine(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()

	if _, err := agent.ParseHook(HookNameUserPromptSubmit,
		[]byte(`{"session_id":"torn","timestamp":"2026-05-20T12:00:00Z","prompt":"Create hello.txt"}`)); err != nil {
		t.Fatal(err)
	}

	sessionRef := agent.sidecarPath("torn")
	f, err := os.OpenFile(sessionRef, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	// A record whose write was interrupted mid-line.
	if _, err := f.WriteString(`{"v":1,"agent":"qwen","event":"PostToolUse","tool_inp` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	prompts, err := agent.ExtractPrompts(sessionRef, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts: %v", err)
	}
	if len(prompts) != 1 || prompts[0] != "Create hello.txt" {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}

	if _, err := agent.GetTranscriptPosition(sessionRef); err != nil {
		t.Fatalf("GetTranscriptPosition: %v", err)
	}
}
