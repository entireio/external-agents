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
		if filepath.Dir(got) != sessionDir {
			t.Fatalf("ResolveSessionFile(%q) = %q places file outside session dir %q", id, got, sessionDir)
		}
		if filepath.Ext(got) != ".json" {
			t.Fatalf("ResolveSessionFile(%q) = %q missing .json extension", id, got)
		}
	}
}

func TestResolveSessionFileEmptyFallback(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	want := filepath.Join(sessionDir, "unknown.json")

	if got := New().ResolveSessionFile(sessionDir, ""); got != want {
		t.Fatalf("ResolveSessionFile(empty) = %q, want %q", got, want)
	}
}

func TestTranscriptPathRefusesPathTraversal(t *testing.T) {
	t.Setenv("GOOSE_PATH_ROOT", t.TempDir())
	wantDir := filepath.Join(t.TempDir(), "data", "sessions")
	for _, id := range []string{
		"../etc/passwd",
		"subdir/../../etc/passwd",
		"/etc/passwd",
		"id\x00.json",
	} {
		got := transcriptPath(id)
		if filepath.Dir(got) != wantDir {
			t.Fatalf("transcriptPath(%q) = %q places file outside session dir %q", id, got, wantDir)
		}
		if filepath.Ext(got) != ".json" {
			t.Fatalf("transcriptPath(%q) = %q missing .json extension", id, got)
		}
	}
}

func TestTranscriptPathEmptyFallback(t *testing.T) {
	t.Setenv("GOOSE_PATH_ROOT", t.TempDir())
	wantDir := filepath.Join(t.TempDir(), "data", "sessions")
	if got := transcriptPath(""); got != filepath.Join(wantDir, "unknown.json") {
		t.Fatalf("transcriptPath(empty) = %q, want %q", got, filepath.Join(wantDir, "unknown.json"))
	}
}
