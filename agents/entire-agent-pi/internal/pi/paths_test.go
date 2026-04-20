package pi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPiRepoDirName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/Users/soph/Work/entire/devenv/go-git-api", "--Users-soph-Work-entire-devenv-go-git-api--"},
		{"/Users/test", "--Users-test--"},
		{"/a", "--a--"},
	}
	for _, tt := range tests {
		if got := piRepoDirName(tt.in); got != tt.want {
			t.Errorf("piRepoDirName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRepoRootFromSessionDir(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/some/repo/.entire/tmp", "/some/repo"},
		{"/some/repo/.entire/tmp/", "/some/repo"},
		{"", ""},
		{"/some/repo/.entire", ""},      // missing tmp suffix
		{"/some/repo/.other/tmp", ""},   // wrong parent name
		{"/some/repo/.entire/logs", ""}, // wrong leaf name
	}
	for _, tt := range tests {
		if got := repoRootFromSessionDir(tt.in); got != tt.want {
			t.Errorf("repoRootFromSessionDir(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFindPiSessionFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoPath := "/Users/tester/Work/demo"
	sessionsDir := filepath.Join(home, ".pi", "agent", "sessions", piRepoDirName(repoPath))
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write two files: one plain .jsonl and one timestamped.
	older := filepath.Join(sessionsDir, "2026-01-01T00-00-00-000Z_abc123.jsonl")
	newer := filepath.Join(sessionsDir, "2026-02-01T00-00-00-000Z_abc123.jsonl")
	for _, p := range []string{older, newer} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Force distinct mtimes so newest-wins is deterministic.
	past := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}

	t.Run("short uuid matches newest timestamped file", func(t *testing.T) {
		got := findPiSessionFile(repoPath, "abc123")
		if got != newer {
			t.Errorf("got %q, want %q", got, newer)
		}
	})

	t.Run("full filename stem matches exactly", func(t *testing.T) {
		got := findPiSessionFile(repoPath, "2026-01-01T00-00-00-000Z_abc123")
		if got != older {
			t.Errorf("got %q, want %q", got, older)
		}
	})

	t.Run("unknown session returns empty", func(t *testing.T) {
		if got := findPiSessionFile(repoPath, "does-not-exist"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("missing repo dir returns empty", func(t *testing.T) {
		if got := findPiSessionFile("/Users/tester/not/a/repo", "abc123"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

// TestResolveSessionFile_HonorsSessionDirOverEnv ensures the live-file
// fallback is scoped to the repo derived from sessionDir, not from
// ENTIRE_REPO_ROOT. Two repos have the same sessionID; the fallback must
// return the file under the sessionDir-derived repo.
func TestResolveSessionFile_HonorsSessionDirOverEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Process cwd / ENTIRE_REPO_ROOT points at repoA, but the caller asks
	// about repoB. The returned path must reflect repoB.
	repoA := t.TempDir()
	repoB := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoA)

	sessionID := "shared123"

	// Live file exists in repoA under the right pi sessions key.
	liveA := filepath.Join(home, ".pi", "agent", "sessions", piRepoDirName(repoA),
		"2026-01-01T00-00-00-000Z_"+sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(liveA), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveA, []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Live file also exists in repoB.
	liveB := filepath.Join(home, ".pi", "agent", "sessions", piRepoDirName(repoB),
		"2026-01-01T00-00-00-000Z_"+sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(liveB), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveB, []byte("B"), 0o600); err != nil {
		t.Fatal(err)
	}

	sessionDirB := filepath.Join(repoB, ".entire", "tmp")

	a := New()
	got := a.ResolveSessionFile(sessionDirB, sessionID)
	if got != liveB {
		t.Errorf("got %q, want %q (must follow sessionDir, not ENTIRE_REPO_ROOT)", got, liveB)
	}
}

// TestResolveSessionFile_NonStandardSessionDirReturnsCaptured covers the
// case where sessionDir doesn't decompose into <repo>/.entire/tmp. The
// resolver cannot infer a repo for the live-file search, so it returns the
// captured path unchanged rather than silently falling back to process
// state.
func TestResolveSessionFile_NonStandardSessionDirReturnsCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())

	// Non-standard sessionDir.
	sessionDir := filepath.Join(t.TempDir(), "custom")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	a := New()
	sessionID := "abc"
	got := a.ResolveSessionFile(sessionDir, sessionID)
	want := filepath.Join(sessionDir, sessionID+".json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveSessionFile_PrefersCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoPath := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoPath)

	sessionDir := filepath.Join(repoPath, ".entire", "tmp")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	captured := filepath.Join(sessionDir, "abc123.json")
	if err := os.WriteFile(captured, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Also place a live pi file — captured should still win.
	liveDir := filepath.Join(home, ".pi", "agent", "sessions", piRepoDirName(repoPath))
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(liveDir, "2026-01-01T00-00-00-000Z_abc123.jsonl")
	if err := os.WriteFile(live, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := New()
	got := a.ResolveSessionFile(sessionDir, "abc123")
	if got != captured {
		t.Errorf("got %q, want captured %q", got, captured)
	}
}

func TestResolveSessionFile_FallsBackToLive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Deliberately point ENTIRE_REPO_ROOT at an unrelated dir to prove the
	// fallback uses sessionDir, not the env var.
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())

	repoPath := t.TempDir()
	sessionDir := filepath.Join(repoPath, ".entire", "tmp")
	// Don't create captured file — force fallback.

	liveDir := filepath.Join(home, ".pi", "agent", "sessions", piRepoDirName(repoPath))
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(liveDir, "2026-01-01T00-00-00-000Z_abc123.jsonl")
	if err := os.WriteFile(live, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := New()
	got := a.ResolveSessionFile(sessionDir, "abc123")
	if got != live {
		t.Errorf("got %q, want live %q", got, live)
	}
}
