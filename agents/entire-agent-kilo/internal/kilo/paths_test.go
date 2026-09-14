package kilo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

func TestResolveSessionFile(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
	want := filepath.Join(sessionDir, "abc123.json")

	if got := New().ResolveSessionFile(sessionDir, "abc123"); got != want {
		t.Fatalf("ResolveSessionFile() = %q, want %q", got, want)
	}
}

func TestGetSessionDirResolveSessionFileMatchesParseHook(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()
	const sessionID = "session-123"

	sessionDir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatalf("GetSessionDir(): %v", err)
	}
	wantDir := filepath.Join(repo, ".entire", "tmp", transcriptSubdir)
	if sessionDir != wantDir {
		t.Fatalf("GetSessionDir() = %q, want %q", sessionDir, wantDir)
	}

	body, err := json.Marshal(sessionInfoRaw{SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	event, err := agent.ParseHook(HookNameSessionStart, body)
	if err != nil {
		t.Fatalf("ParseHook(): %v", err)
	}
	if event == nil {
		t.Fatal("ParseHook() returned no event")
	}
	if got := agent.ResolveSessionFile(sessionDir, sessionID); got != event.SessionRef {
		t.Fatalf("ResolveSessionFile() = %q, ParseHook SessionRef = %q", got, event.SessionRef)
	}
}

func TestResolveSessionFileEmptyFallback(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
	want := filepath.Join(sessionDir, "~e3b0c44298fc1c149afbf4c8996fb924.json")

	if got := New().ResolveSessionFile(sessionDir, ""); got != want {
		t.Fatalf("ResolveSessionFile() = %q, want %q", got, want)
	}
}

func TestResolveSessionFileContainsHostileIDs(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
	for _, id := range []string{
		"../etc/passwd",
		"..\\etc\\passwd",
		"subdir/../../etc/passwd",
		"/etc/passwd",
		".. ",
		" ..",
		" padded ",
		"C:outside",
		"id\x00.json",
	} {
		t.Run(id, func(t *testing.T) {
			got := New().ResolveSessionFile(sessionDir, id)
			if filepath.Dir(got) != sessionDir {
				t.Fatalf("ResolveSessionFile(%q) = %q, want a file directly in %q", id, got, sessionDir)
			}
			if strings.ContainsAny(filepath.Base(got), "/\\:\x00\t\r\n ") {
				t.Fatalf("ResolveSessionFile(%q) = %q, filename still contains an unsafe path character", id, got)
			}
			if filepath.Ext(got) != ".json" {
				t.Fatalf("ResolveSessionFile(%q) = %q, want .json extension", id, got)
			}
		})
	}
}

func TestResolveSessionFileDistinguishesHostileIDs(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp", transcriptSubdir)
	agent := New()
	first := agent.ResolveSessionFile(sessionDir, "../session/a")
	second := agent.ResolveSessionFile(sessionDir, `..\session\a`)

	if first == second {
		t.Fatalf("distinct hostile session IDs resolved to the same path %q", first)
	}
	for _, path := range []string{first, second} {
		if filepath.Dir(path) != sessionDir {
			t.Fatalf("resolved path escaped session directory: %q", path)
		}
	}
}

func TestResolveSessionFileWriteSessionCannotWriteOutside(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	outsidePath := filepath.Join(root, "outside", "pwned.json")
	if err := os.MkdirAll(filepath.Dir(outsidePath), 0o700); err != nil {
		t.Fatal(err)
	}
	const sentinel = "outside sentinel"
	if err := os.WriteFile(outsidePath, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}

	agent := New()
	sessionRef := agent.ResolveSessionFile(sessionDir, "../outside/pwned")
	const transcript = "attacker-influenced transcript"
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionRef: sessionRef,
		NativeData: []byte(transcript),
	}); err != nil {
		t.Fatalf("WriteSession(%q): %v", sessionRef, err)
	}

	outsideData, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideData) != sentinel {
		t.Fatalf("outside file was overwritten through resolver-produced session ref: got %q", outsideData)
	}
	written, err := os.ReadFile(sessionRef)
	if err != nil {
		t.Fatalf("read contained transcript: %v", err)
	}
	if string(written) != transcript {
		t.Fatalf("contained transcript = %q, want %q", written, transcript)
	}
}
