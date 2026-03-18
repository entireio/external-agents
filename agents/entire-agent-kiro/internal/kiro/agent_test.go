package kiro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"
)

func TestDetectUsesRepoRoot(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())

	ag := New()
	if ag.Detect().Present {
		t.Fatal("detect should be false when .kiro is absent")
	}

	kiroDir := filepath.Join(protocol.RepoRoot(), ".kiro")
	if err := os.MkdirAll(kiroDir, 0o750); err != nil {
		t.Fatalf("mkdir .kiro: %v", err)
	}

	if !ag.Detect().Present {
		t.Fatal("detect should be true when .kiro is present")
	}
}

func TestReadSessionLoadsNativeData(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	sessionRef := filepath.Join(repoRoot, ".entire", "tmp", "session-123.json")
	wantData := []byte(`{"conversation_id":"session-123","history":[]}`)
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o750); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(sessionRef, wantData, 0o600); err != nil {
		t.Fatalf("write session ref: %v", err)
	}

	got, err := New().ReadSession(&protocol.HookInputJSON{
		SessionID:  "session-123",
		SessionRef: sessionRef,
	})
	if err != nil {
		t.Fatalf("ReadSession() error = %v", err)
	}

	if got.SessionID != "session-123" {
		t.Fatalf("session_id = %q, want %q", got.SessionID, "session-123")
	}
	if got.AgentName != "kiro" {
		t.Fatalf("agent_name = %q, want %q", got.AgentName, "kiro")
	}
	if got.RepoPath != repoRoot {
		t.Fatalf("repo_path = %q, want %q", got.RepoPath, repoRoot)
	}
	if got.SessionRef != sessionRef {
		t.Fatalf("session_ref = %q, want %q", got.SessionRef, sessionRef)
	}
	if string(got.NativeData) != string(wantData) {
		t.Fatalf("native_data = %q, want %q", string(got.NativeData), string(wantData))
	}
	if got.ModifiedFiles == nil || got.NewFiles == nil || got.DeletedFiles == nil {
		t.Fatal("file lists should be initialized")
	}
}

func TestReadSessionReturnsErrorWhenTranscriptReadFails(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	_, err := New().ReadSession(&protocol.HookInputJSON{
		SessionID:  "session-123",
		SessionRef: filepath.Join(repoRoot, ".entire", "tmp", "missing.json"),
	})

	if err == nil || !strings.Contains(err.Error(), "failed to read transcript") {
		t.Fatalf("ReadSession() error = %v, want failed to read transcript", err)
	}
}

func TestWriteSessionPersistsNativeData(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	sessionRef := filepath.Join(repoRoot, ".entire", "tmp", "session-456.json")
	wantData := []byte(`{"conversation_id":"session-456","history":[]}`)

	err := New().WriteSession(protocol.AgentSessionJSON{
		SessionID:  "session-456",
		AgentName:  "kiro",
		RepoPath:   repoRoot,
		SessionRef: sessionRef,
		NativeData: wantData,
	})
	if err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	gotData, err := os.ReadFile(sessionRef)
	if err != nil {
		t.Fatalf("read session ref: %v", err)
	}
	if string(gotData) != string(wantData) {
		t.Fatalf("written data = %q, want %q", string(gotData), string(wantData))
	}
}

func TestFormatResumeCommand(t *testing.T) {
	if got := New().FormatResumeCommand("anything"); got != "kiro-cli chat --resume" {
		t.Fatalf("FormatResumeCommand() = %q, want %q", got, "kiro-cli chat --resume")
	}
}
