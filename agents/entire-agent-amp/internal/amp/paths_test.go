package amp

import (
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

func TestResolveSessionFile(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
	want := filepath.Join(sessionDir, "abc123.jsonl")

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
		if filepath.Ext(got) != ".jsonl" {
			t.Fatalf("ResolveSessionFile(%q) = %q missing .jsonl extension", id, got)
		}
	}
}

func TestResolveSessionFileEmptyFallback(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".entire", "tmp")
	want := filepath.Join(sessionDir, "~e3b0c44298fc1c149afbf4c8996fb924.jsonl")

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

func TestResolverMatchesHookTranscript(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T-123", "../../outside", "a/b", ""} {
		t.Run(id, func(t *testing.T) {
			got := agent.ResolveSessionFile(dir, id)
			if want := transcriptPath(id); got != want {
				t.Fatalf("resolver = %q, hook transcript = %q", got, want)
			}
			const data = `{"test":true}`
			if err := agent.WriteSession(protocol.AgentSessionJSON{SessionID: id, SessionRef: got, NativeData: []byte(data)}); err != nil {
				t.Fatal(err)
			}
			if id != "" {
				session, err := agent.ReadSession(&protocol.HookInputJSON{SessionID: id})
				if err != nil {
					t.Fatal(err)
				}
				if session.SessionRef != got || string(session.NativeData) != data {
					t.Fatalf("restored session = %+v", session)
				}
			}
		})
	}
}
