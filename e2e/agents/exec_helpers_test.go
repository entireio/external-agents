package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookPathAnyFindsFirstAvailableBinary(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "kiro-cli")
	if err := os.WriteFile(first, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	t.Setenv("PATH", dir)

	got, err := lookPathAny("kiro-cli-chat", "kiro-cli")
	if err != nil {
		t.Fatalf("lookPathAny() error = %v", err)
	}
	if got != first {
		t.Fatalf("lookPathAny() = %q, want %q", got, first)
	}
}

func TestLookPathAnyErrorsWhenNothingExists(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := lookPathAny("missing-a", "missing-b"); err == nil {
		t.Fatal("lookPathAny() error = nil, want non-nil")
	}
}
