package qwen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSidecarOffsetsIncludeMalformedAndBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"event":"UserPromptSubmit","prompt":"first"}` + "\n" +
		`{"event":"UserPromptSubmit","prompt":"must not survive","v":"invalid"}` + "\n\n" +
		`{"event":"UserPromptSubmit","prompt":"second"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New()
	pos, err := a.GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 4 {
		t.Errorf("position = %d, want physical line count 4", pos)
	}
	prompts, err := a.ExtractPrompts(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "second" {
		t.Errorf("prompts after line 3 = %v, want second", prompts)
	}
	prompts, err = a.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Errorf("malformed record leaked partial data: %v", prompts)
	}
	_, current, err := a.ExtractModifiedFiles(path, 3)
	if err != nil || current != 4 {
		t.Errorf("current = %d, err = %v, want 4", current, err)
	}
}
