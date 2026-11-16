package dsh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeSegment(t *testing.T) {
	tests := map[string]string{
		"abc-123": "abc-123",
		"a/b":     "a~002Fb",
		"a b":     "a~0020b",
		"~":       "~007E",
		"é":       "~00E9",
		".":       "~002E",
		"..":      "~002E~002E",
	}
	for input, want := range tests {
		got, err := encodeSegment(input)
		if err != nil {
			t.Fatalf("encodeSegment(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("encodeSegment(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := encodeSegment(""); err == nil {
		t.Fatal("encodeSegment(\"\") should fail")
	}
}

func TestProjectKey(t *testing.T) {
	tests := map[string]string{
		"/home/user/repo": "--home-user-repo--",
		"/tmp//x":         "--tmp-x--",
		`C:\Users\x`:      "--C-Users-x--",
		"":                "--root--",
	}
	for input, want := range tests {
		if got := projectKey(input); got != want {
			t.Errorf("projectKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSafeSessionName(t *testing.T) {
	if got, hashed := safeSessionName("session-abc_123"); got != "session-abc_123" || hashed {
		t.Errorf("safe id changed: got %q hashed=%v", got, hashed)
	}
	for _, unsafe := range []string{"../evil", "a/b", "CON", strings.Repeat("a", 181)} {
		got, hashed := safeSessionName(unsafe)
		if !hashed {
			t.Errorf("safeSessionName(%q) should hash, got %q", unsafe, got)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("hashed name %q contains a separator", got)
		}
	}
}

func TestSessionIDFromRef(t *testing.T) {
	dir := t.TempDir()
	safeRef := filepath.Join(dir, "session-abc.jsonl")
	if got, ok := sessionIDFromRef(safeRef); !ok || got != "session-abc" {
		t.Errorf("sessionIDFromRef(safe) = %q,%v", got, ok)
	}

	hashedRef := filepath.Join(dir, mustHashName(t, "../evil"))
	if err := writeSessionIDSidecar(hashedRef, "../evil"); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	if got, ok := sessionIDFromRef(hashedRef); !ok || got != "../evil" {
		t.Errorf("sessionIDFromRef(hashed) = %q,%v", got, ok)
	}

	if _, ok := sessionIDFromRef(filepath.Join(dir, "transcript.json")); ok {
		t.Error("non-jsonl path should not resolve to a session ID")
	}
}

func mustHashName(t *testing.T, id string) string {
	t.Helper()
	name, hashed := safeSessionName(id)
	if !hashed {
		t.Fatalf("expected %q to hash", id)
	}
	return name + ".jsonl"
}

func TestFindNativeSessionDeterministic(t *testing.T) {
	session := newTestSession(t, "session-find", "/repo/one", nil)
	got, err := findNativeSession("session-find", session.CWD, "")
	if err != nil {
		t.Fatalf("findNativeSession: %v", err)
	}
	if got != session.NativePath {
		t.Errorf("got %q, want %q", got, session.NativePath)
	}
}

func TestFindNativeSessionScansProjectDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	parent := filepath.Join(home, "repo")
	subdir := filepath.Join(parent, "pkg")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	native := writeNativeSession(t, home, "session-scan", subdir, nil)
	got, err := findNativeSession("session-scan", parent, "")
	if err != nil {
		t.Fatalf("findNativeSession scan: %v", err)
	}
	if got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

func TestFindNativeSessionMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	if _, err := os.Stat(filepath.Join(home, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("expected no sessions dir: %v", err)
	}
	if _, err := findNativeSession("nope", "/repo", ""); err == nil {
		t.Fatal("expected an error for a missing session")
	}
}

func TestFindNativeSessionSymlinkedRepo(t *testing.T) {
	physical := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "repo-link")
	if err := os.Symlink(physical, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	native := writeNativeSession(t, home, "session-symlink", physical, nil)
	got, err := findNativeSession("session-symlink", link, "")
	if err != nil {
		t.Fatalf("findNativeSession through symlink: %v", err)
	}
	if got != native {
		t.Errorf("got %q, want %q", got, native)
	}
	linkDir, err := (&Agent{}).GetSessionDir(link)
	if err != nil {
		t.Fatalf("GetSessionDir(link): %v", err)
	}
	physicalDir, err := (&Agent{}).GetSessionDir(physical)
	if err != nil {
		t.Fatalf("GetSessionDir(physical): %v", err)
	}
	if linkDir != physicalDir {
		t.Errorf("cache dirs differ: link=%q physical=%q", linkDir, physicalDir)
	}
	if !pathWithin(link, physical) {
		t.Error("pathWithin should compare physical paths")
	}
}
