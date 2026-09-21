package goose

import (
	"path/filepath"
	"testing"
)

func TestResolveSessionFile(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	want := filepath.Join(sessionDir, "20260611_1.json")

	if got := New().ResolveSessionFile(sessionDir, "20260611_1"); got != want {
		t.Fatalf("ResolveSessionFile() = %q, want %q", got, want)
	}
}

func TestResolveSessionFileRefusesPathTraversal(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	for _, id := range []string{
		"../etc/passwd",
		"..\\etc\\passwd",
		"subdir/../../etc/passwd",
		"/etc/passwd",
		"absolute/path.json",
		"id\x00.json",
	} {
		got := New().ResolveSessionFile(sessionDir, id)
		if got != "" {
			t.Fatalf("ResolveSessionFile(%q) = %q, want rejection", id, got)
		}
	}
}

func TestResolveSessionFileRejectsEmpty(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	want := ""

	if got := New().ResolveSessionFile(sessionDir, ""); got != want {
		t.Fatalf("ResolveSessionFile(empty) = %q, want %q", got, want)
	}
}

func TestTranscriptPathRefusesPathTraversal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOOSE_PATH_ROOT", root)
	for _, id := range []string{
		"../etc/passwd",
		"subdir/../../etc/passwd",
		"/etc/passwd",
		"id\x00.json",
	} {
		got := transcriptPath(id)
		if got != "" {
			t.Fatalf("transcriptPath(%q) = %q, want rejection", id, got)
		}
	}
}

func TestTranscriptPathRejectsEmpty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOOSE_PATH_ROOT", root)
	if got := transcriptPath(""); got != "" {
		t.Fatalf("transcriptPath(empty) = %q, want rejection", got)
	}
}
