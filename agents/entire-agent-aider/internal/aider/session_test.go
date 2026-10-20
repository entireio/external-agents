package aider

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-aider/internal/protocol"
)

func TestSessionIDPersistsWithoutGit(t *testing.T) {
	repo := testRepo(t)
	first, err := New().GetSessionID(&protocol.HookInput{SessionID: "not-a-launch-SHA"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(first)
	if err != nil || len(decoded) != 16 {
		t.Fatalf("not random hex: %q", first)
	}
	second, err := New().GetSessionID(nil)
	if err != nil || first != second {
		t.Fatalf("persisted ID = %q, %v", second, err)
	}
	path := filepath.Join(repo, ".entire", "tmp", "aider-session")
	saved, err := os.ReadFile(path)
	if err != nil || string(bytes.TrimSpace(saved)) != first {
		t.Fatalf("saved = %q, %v", saved, err)
	}
	if err := os.WriteFile(path, []byte("  existing-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := New().GetSessionID(nil)
	if err != nil || got != "existing-id" {
		t.Fatalf("existing ID = %q, %v", got, err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = New().GetSessionID(nil)
	if err != nil || got == first || len(got) != 32 {
		t.Fatalf("regenerated ID = %q, %v", got, err)
	}
}
func TestSessionIDConcurrentCalls(t *testing.T) {
	testRepo(t)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			id, err := New().GetSessionID(nil)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- id
		})
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("IDs differ: %q, %q", first, id)
		}
	}
}
func TestSessionIDReportsPersistenceErrors(t *testing.T) {
	repo := testRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".entire"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New().GetSessionID(nil); err == nil {
		t.Fatal("expected storage error")
	}
}
func TestSessionReadWrite(t *testing.T) {
	repo := testRepo(t)
	a := New()
	dir, err := a.GetSessionDir(repo)
	if err != nil || dir != repo {
		t.Fatalf("dir = %q, %v", dir, err)
	}
	ref := a.ResolveSessionFile(dir, "../../escape")
	if ref != filepath.Join(repo, HistoryFile) {
		t.Fatal(ref)
	}
	data := []byte("#### Hello café\r\n\nResponse\n")
	if err := a.WriteSession(protocol.Session{RepoPath: repo, NativeData: data}); err != nil {
		t.Fatal(err)
	}
	session, err := a.ReadSession(&protocol.HookInput{Timestamp: "2026-09-14T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(session.NativeData, data) || session.SessionRef != ref || session.AgentName != "aider" || session.SessionID == "" {
		t.Fatalf("session = %+v", session)
	}
	if err := a.WriteSession(protocol.Session{SessionRef: ref}); err == nil {
		t.Fatal("empty data must not truncate history")
	}
	got, err := a.ReadTranscript(ref)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("history changed: %q, %v", got, err)
	}
	if _, err := a.ReadSession(&protocol.HookInput{SessionRef: filepath.Join(repo, "missing")}); err == nil {
		t.Fatal("missing history must report an error")
	}
}
