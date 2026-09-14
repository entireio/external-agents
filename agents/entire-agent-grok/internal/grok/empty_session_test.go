package grok

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

func TestEmptySessionIDPathsAgree(t *testing.T) {
	for _, id := range []string{"", " \t", "\n", "\u2003"} {
		t.Run(id, func(t *testing.T) {
			repo := t.TempDir()
			t.Setenv("ENTIRE_REPO_ROOT", repo)
			t.Setenv("GROK_HOME", t.TempDir())
			agent := New()
			input := &protocol.HookInputJSON{SessionID: id}
			canonical := agent.GetSessionID(input)
			dir, err := agent.GetSessionDir(repo)
			if err != nil {
				t.Fatal(err)
			}
			want := agent.ResolveSessionFile(dir, canonical)
			if got := agent.ResolveSessionFile(dir, id); got != want {
				t.Errorf("raw resolver = %q, canonical resolver = %q", got, want)
			}
			sessionID, ref := agent.sessionIDAndRef(input)
			if sessionID != canonical || ref != want {
				t.Errorf("sessionIDAndRef = (%q, %q), want (%q, %q)", sessionID, ref, canonical, want)
			}
			payload, err := json.Marshal(map[string]string{"session_id": id, "cwd": repo})
			if err != nil {
				t.Fatal(err)
			}
			event, err := agent.ParseHook(HookNameSessionStart, payload)
			if err != nil {
				t.Fatal(err)
			}
			if event == nil {
				t.Fatal("missing session-start event")
			}
			if event.SessionID != canonical || event.SessionRef != want {
				t.Errorf("hook = (%q, %q), want (%q, %q)", event.SessionID, event.SessionRef, canonical, want)
			}
			markerPath := filepath.Join(repo, ".entire", "tmp", safeFilename(id)+".json")
			data, err := os.ReadFile(markerPath)
			if err != nil {
				t.Fatal(err)
			}
			var marker protocol.AgentSessionJSON
			if err := json.Unmarshal(data, &marker); err != nil {
				t.Fatal(err)
			}
			if marker.SessionID != canonical || marker.SessionRef != want {
				t.Errorf("marker = (%q, %q), want (%q, %q)", marker.SessionID, marker.SessionRef, canonical, want)
			}
		})
	}
}
