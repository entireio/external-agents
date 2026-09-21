package amp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyExportCanBePreparedAgain(t *testing.T) {
	runner := &emptyExportRunner{}
	a := New()
	a.CommandRunner = runner
	path := filepath.Join(t.TempDir(), "T-empty.jsonl")
	if err := a.exportThread("T-empty", path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := threadIDFromTranscriptData(data)
	if err != nil {
		t.Fatal(err)
	}
	if id != "T-empty" {
		t.Fatalf("thread ID = %q", id)
	}
	if err := a.PrepareTranscript(path); err != nil {
		t.Fatal(err)
	}
}

type emptyExportRunner struct{}

func (*emptyExportRunner) ExportThread(context.Context, string) ([]byte, error) {
	return []byte(`{"id":"T-empty","messages":[]}`), nil
}
