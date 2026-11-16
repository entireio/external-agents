package sarvam

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-sarvam/internal/protocol"
)

func TestInfo(t *testing.T) {
	agent := New()
	info := agent.Info()

	if info.ProtocolVersion != 1 {
		t.Errorf("protocol_version = %d, want 1", info.ProtocolVersion)
	}
	if info.Name != AgentName {
		t.Errorf("name = %q, want %q", info.Name, AgentName)
	}
	if info.Type != AgentType {
		t.Errorf("type = %q, want %q", info.Type, AgentType)
	}
	if info.IsPreview != true {
		t.Error("is_preview must be true while the integration is in preview")
	}
	if len(info.HookNames) != len(hookSpecs) {
		t.Fatalf("hook_names = %v, want one per installed event", info.HookNames)
	}
	caps := info.Capabilities
	if !caps.Hooks || !caps.TranscriptAnalyzer || !caps.CompactTranscript || !caps.TokenCalculator || !caps.UsesTerminal {
		t.Errorf("declared capabilities = %+v", caps)
	}
	if caps.TextGenerator || caps.HookResponseWriter || caps.SubagentAwareExtractor || caps.TranscriptPreparer {
		t.Errorf("undeclared capabilities must be false: %+v", caps)
	}
}

func TestDetect(t *testing.T) {
	// Isolate the store so detect does not see this machine's real
	// ~/.sarvam.
	t.Setenv("SARVAM_HOME", t.TempDir())

	agent := &Agent{LookPath: func(string) (string, error) {
		return "", errors.New("not found")
	}}
	if agent.Detect().Present {
		t.Error("detect must be false when the CLI is off PATH and SARVAM_HOME is absent")
	}

	agent = &Agent{LookPath: func(name string) (string, error) {
		if name == sarvamBinary {
			return "/usr/local/bin/sarvam-code", nil
		}
		return "", errors.New("not found")
	}}
	if !agent.Detect().Present {
		t.Error("detect must be true when the CLI is on PATH")
	}
}

func TestGetSessionDir_RespectsSarvamHome(t *testing.T) {
	t.Setenv("SARVAM_HOME", "/custom/home")
	agent := New()

	dir, err := agent.GetSessionDir("/some/repo")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join("/custom/home", "sessions") {
		t.Fatalf("session_dir = %q, want the SARVAM_HOME sessions tree", dir)
	}
}

func TestResolveSessionFile_FallbackContainsSessionID(t *testing.T) {
	agent := New()
	dir := t.TempDir()

	path := agent.ResolveSessionFile(dir, "test-session-123")
	if path == "" {
		t.Fatal("session_file must not be empty")
	}
	if !contains(path, "test-session-123") {
		t.Fatalf("session_file %q must contain the raw session-id", path)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("session_file %q must stay inside the session dir", path)
	}
}

func TestResolveSessionFile_FindsNativeRollout(t *testing.T) {
	agent := New()
	dir := t.TempDir()

	sessionID := "01a1e2d3-8a2b-7dc2-b125-885a43ce43f5"
	native := filepath.Join(dir, "2026", "11", "16", "rollout-2026-11-16T06-27-56-"+sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	path := agent.ResolveSessionFile(dir, sessionID)
	if path != native {
		t.Fatalf("session_file = %q, want the native rollout %q", path, native)
	}
}

func TestSessionWriteReadRoundTrip(t *testing.T) {
	agent := New()
	dir := t.TempDir()
	ref := agent.ResolveSessionFile(dir, "round-trip-session")

	session := protocol.AgentSessionJSON{
		SessionID:     "round-trip-session",
		AgentName:     AgentName,
		RepoPath:      dir,
		SessionRef:    ref,
		NativeData:    []byte(`{"test": true}`),
		ModifiedFiles: []string{"file1.go"},
		NewFiles:      []string{"file3.go"},
		DeletedFiles:  []string{},
	}
	if err := agent.WriteSession(session); err != nil {
		t.Fatal(err)
	}

	input := protocol.HookInputJSON{HookType: "session-start", SessionID: "round-trip-session", SessionRef: ref}
	readBack, err := agent.ReadSession(&input)
	if err != nil {
		t.Fatal(err)
	}
	if readBack.SessionID != session.SessionID {
		t.Errorf("session_id = %q", readBack.SessionID)
	}
	if readBack.AgentName != AgentName {
		t.Errorf("agent_name = %q", readBack.AgentName)
	}
	if readBack.SessionRef != ref {
		t.Errorf("session_ref = %q", readBack.SessionRef)
	}
	if string(readBack.NativeData) != string(session.NativeData) {
		t.Errorf("native_data = %q, want %q", readBack.NativeData, session.NativeData)
	}
	if readBack.ModifiedFiles == nil || readBack.NewFiles == nil || readBack.DeletedFiles == nil {
		t.Error("file lists must be initialized on readback")
	}
}

func TestWriteSession_WithoutRefUsesStore(t *testing.T) {
	t.Setenv("SARVAM_HOME", t.TempDir())
	agent := New()

	session := protocol.AgentSessionJSON{
		SessionID:  "no-ref-session",
		NativeData: []byte(`{"test": true}`),
	}
	if err := agent.WriteSession(session); err != nil {
		t.Fatal(err)
	}
	storePath := agent.ResolveSessionFile("", "no-ref-session")
	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("session without ref was not written to the store: %v", err)
	}
}

func TestChunkTranscript_RoundTrip(t *testing.T) {
	agent := New()
	original := []byte("The quick brown fox jumps over the lazy dog.")

	chunks, err := agent.ChunkTranscript(original, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 5 {
		t.Fatalf("chunks = %d, want 5", len(chunks))
	}

	reassembled, err := agent.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if string(reassembled) != string(original) {
		t.Fatalf("round-trip mismatch: %q != %q", reassembled, original)
	}

	if _, err := agent.ChunkTranscript(original, 0); err == nil {
		t.Error("max-size 0 must be rejected")
	}
	if _, err := agent.ChunkTranscript(original, -1); err == nil {
		t.Error("negative max-size must be rejected")
	}

	single, err := agent.ChunkTranscript(original, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(single) != 1 {
		t.Fatalf("small content chunks = %d, want 1", len(single))
	}
}

func TestFormatResumeCommand(t *testing.T) {
	agent := New()

	if command := agent.FormatResumeCommand(""); !contains(command, "resume") {
		t.Fatalf("empty id: %q", command)
	}
	if command := agent.FormatResumeCommand("abc-123"); !contains(command, "abc-123") {
		t.Fatalf("command %q must carry the session id", command)
	}
	// An ID with shell metacharacters must arrive quoted, never raw.
	if command := agent.FormatResumeCommand("a b;rm -rf /"); !strings.Contains(command, "'a b;rm -rf /'") {
		t.Fatalf("command %q must single-quote the id", command)
	}
	if command := agent.FormatResumeCommand("a b;rm -rf /"); strings.Contains(command, " resume a b") {
		t.Fatalf("command %q must not interpolate the raw id", command)
	}
}

func TestReadTranscript(t *testing.T) {
	agent := New()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := agent.ReadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("read-transcript = %q, want raw bytes", data)
	}
}
