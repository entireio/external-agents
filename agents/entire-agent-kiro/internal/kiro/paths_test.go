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
	want := filepath.Join(sessionDir, "~e3b0c44298fc1c149afbf4c8996fb924.json")

	if got := New().ResolveSessionFile(sessionDir, ""); got != want {
		t.Fatalf("ResolveSessionFile(empty) = %q, want %q", got, want)
	}
}

func TestSafePathSessionID(t *testing.T) {
	cases := map[string]string{
		"":                  "~e3b0c44298fc1c149afbf4c8996fb924",
		"S-abc_123":         "S-abc_123",
		"path/with/slashes": "~0f5bd24a68a0f5fafb48b6af79fac130",
		"weird chars!@#$%":  "~cbedb3d0deb4371167f7c3f15dd5c053",
		"dotted.id.is.fine": "dotted.id.is.fine",
	}
	for sessionID, want := range cases {
		if got := safePathSessionID(sessionID); got != want {
			t.Errorf("safePathSessionID(%q) = %q, want %q", sessionID, got, want)
		}
	}
}

func TestSafePathSessionIDSeparatesSanitizationCollisions(t *testing.T) {
	for _, pair := range [][2]string{
		{"T/foo:bar", "T_foo_bar"},
		{"a/b", "a:b"},
		{"", "unknown"},
	} {
		first := safePathSessionID(pair[0])
		second := safePathSessionID(pair[1])
		if first == second {
			t.Fatalf("safePathSessionID(%q) and safePathSessionID(%q) both returned %q", pair[0], pair[1], first)
		}
	}
}
