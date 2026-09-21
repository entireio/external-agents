package amp

import (
	"path/filepath"
	"testing"
)

func TestResolveSessionFile(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
	want := filepath.Join(sessionDir, "abc123.json")

	if got := New().ResolveSessionFile(sessionDir, "abc123"); got != want {
		t.Fatalf("ResolveSessionFile() = %q, want %q", got, want)
	}
}

func TestResolveSessionFileRefusesPathTraversal(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
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
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
	want := filepath.Join(sessionDir, "unknown.json")

	if got := New().ResolveSessionFile(sessionDir, ""); got != want {
		t.Fatalf("ResolveSessionFile(empty) = %q, want %q", got, want)
	}
}
