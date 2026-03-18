package kiro

import (
	"path/filepath"
	"testing"
)

func TestGetSessionDir(t *testing.T) {
	repoPath := t.TempDir()
	want := filepath.Join(repoPath, ".entire", "tmp")

	got, err := New().GetSessionDir(repoPath)
	if err != nil {
		t.Fatalf("GetSessionDir() error = %v", err)
	}
	if got != want {
		t.Fatalf("GetSessionDir() = %q, want %q", got, want)
	}
}

func TestResolveSessionFile(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
	want := filepath.Join(sessionDir, "abc123.json")

	if got := New().ResolveSessionFile(sessionDir, "abc123"); got != want {
		t.Fatalf("ResolveSessionFile() = %q, want %q", got, want)
	}
}
