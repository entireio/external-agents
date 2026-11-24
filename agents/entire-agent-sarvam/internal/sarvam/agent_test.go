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
	if info.TranscriptFormat != "codex" {
		t.Errorf("transcript_format = %q, want codex (Sarvam writes Codex rollouts)", info.TranscriptFormat)
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
	// The store root, not today's day directory: the CLI treats this
	// directory as the store root and rejects resolved paths outside it,
	// while the store itself is date-partitioned beneath it.
	want := filepath.Join("/custom/home", "sessions")
	if dir != want {
		t.Fatalf("session_dir = %q, want the sessions root %q", dir, want)
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

	t.Run("day directory layout", func(t *testing.T) {
		dir := t.TempDir()
		sessionID := "01a1e2d3-8a2b-7dc2-b125-885a43ce43f5"
		native := filepath.Join(dir, "rollout-2026-11-16T06-27-56-"+sessionID+".jsonl")
		if err := os.WriteFile(native, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if path := agent.ResolveSessionFile(dir, sessionID); path != native {
			t.Fatalf("session_file = %q, want the native rollout %q", path, native)
		}
	})

	t.Run("sessions root layout", func(t *testing.T) {
		dir := t.TempDir()
		sessionID := "01a1e2d3-8a2b-7dc2-b125-885a43ce43f5"
		native := filepath.Join(dir, "2026", "11", "16", "rollout-2026-11-16T06-27-56-"+sessionID+".jsonl")
		if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(native, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if path := agent.ResolveSessionFile(dir, sessionID); path != native {
			t.Fatalf("session_file = %q, want the native rollout %q", path, native)
		}
	})
}

func TestResolveSessionFile_FindsSessionsFromEarlierDays(t *testing.T) {
	t.Setenv("SARVAM_HOME", t.TempDir())
	agent := New()

	sessionID := "01a1e2d3-8a2b-7dc2-b125-885a43ce43f5"
	native := filepath.Join(sarvamHome(), "sessions", "2026", "11", "15", "rollout-2026-11-15T06-27-56-"+sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A session still running from an earlier day lives in a date
	// partition under the store root; resolving from the root must find it
	// with the whole-tree glob instead of falling back to the synthetic
	// path — and the resolved path stays inside the store the CLI checks
	// containment against.
	storeRoot, err := agent.GetSessionDir("")
	if err != nil {
		t.Fatal(err)
	}
	if path := agent.ResolveSessionFile(storeRoot, sessionID); path != native {
		t.Fatalf("session_file = %q, want the earlier-day rollout %q", path, native)
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

func TestWriteSession_RefusesEmptyData(t *testing.T) {
	agent := New()
	dir := t.TempDir()
	native := filepath.Join(dir, "rollout-native.jsonl")
	if err := os.WriteFile(native, []byte("{\"type\":\"session_meta\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// An empty snapshot must never be written, wherever the ref points —
	// at the native rollout it would truncate sarvam's own transcript.
	for name, data := range map[string][]byte{"nil": nil, "zero-length": {}} {
		session := protocol.AgentSessionJSON{
			SessionID:  "empty-session",
			SessionRef: native,
			NativeData: data,
		}
		if err := agent.WriteSession(session); err == nil {
			t.Fatalf("%s native data must be refused", name)
		}
	}
	current, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "{\"type\":\"session_meta\"}\n" {
		t.Fatalf("native transcript was modified: %q", current)
	}

	// A real snapshot still writes.
	session := protocol.AgentSessionJSON{
		SessionID:  "real-session",
		SessionRef: filepath.Join(dir, "real-session.jsonl"),
		NativeData: []byte(`{"test": true}`),
	}
	if err := agent.WriteSession(session); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session.SessionRef); err != nil {
		t.Fatalf("real snapshot was not written: %v", err)
	}
}

func TestWriteSession_RefusesNativeRolloutTargets(t *testing.T) {
	t.Setenv("SARVAM_HOME", t.TempDir())
	agent := New()

	// A live native rollout for this session ID, directly under the store
	// root the CLI treats as the session directory.
	sessionID := "01a1e2d3-8a2b-7dc2-b125-885a43ce43f5"
	storeRoot, err := agent.GetSessionDir("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(storeRoot, "rollout-2026-11-17T08-00-00-"+sessionID+".jsonl")
	if err := os.WriteFile(native, []byte("{\"type\":\"session_meta\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A caller-supplied rollout-shaped ref must be refused, contents intact.
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  sessionID,
		SessionRef: native,
		NativeData: []byte(`{"captured": "snapshot"}`),
	}); err == nil {
		t.Fatal("writing to a native rollout path must be refused")
	}
	current, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "{\"type\":\"session_meta\"}\n" {
		t.Fatalf("native rollout was overwritten: %q", current)
	}

	// An empty ref resolves to the deterministic store path — not the
	// native rollout ResolveSessionFile would pick — and writes there.
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  sessionID,
		NativeData: []byte(`{"captured": "snapshot"}`),
	}); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(storeRoot, safeFilename(sessionID)+".jsonl")
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("snapshot was not written to the store path: %v", err)
	}
	current, err = os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "{\"type\":\"session_meta\"}\n" {
		t.Fatalf("native rollout was overwritten via the empty-ref path: %q", current)
	}
}

func TestWriteSession_RestoresToMissingRolloutRef(t *testing.T) {
	t.Setenv("SARVAM_HOME", t.TempDir())
	agent := New()

	// A rollout-shaped ref that does not exist is Entire restoring a
	// captured snapshot onto a fresh clone, not clobbering a live
	// transcript: the write must succeed.
	dir := t.TempDir()
	missing := filepath.Join(dir, "rollout-2026-11-17T08-00-00-01a1e2d3-8a2b-7dc2-b125-885a43ce43f5.jsonl")
	snapshot := protocol.AgentSessionJSON{
		SessionID:  "01a1e2d3-8a2b-7dc2-b125-885a43ce43f5",
		SessionRef: missing,
		NativeData: []byte(`{"type":"session_meta","restored":true}`),
	}
	if err := agent.WriteSession(snapshot); err != nil {
		t.Fatalf("restoring to a missing rollout-shaped ref failed: %v", err)
	}
	written, err := os.ReadFile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != `{"type":"session_meta","restored":true}` {
		t.Fatalf("restored snapshot = %q", written)
	}

	// The restored file now exists, so a second write is the clobber case
	// again and must be refused, contents intact.
	snapshot.NativeData = []byte(`{"type":"session_meta","clobber":true}`)
	if err := agent.WriteSession(snapshot); err == nil {
		t.Fatal("a second write to the now-existing rollout must be refused")
	}
	written, err = os.ReadFile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != `{"type":"session_meta","restored":true}` {
		t.Fatalf("the restored snapshot was overwritten: %q", written)
	}
}

func TestWriteSession_RestoreViaFallbackRefIsDiscoverable(t *testing.T) {
	// The CLI's restore resolves the deterministic fallback ref when no
	// rollout exists (RestoredSessionPathResolver is built-in-only). Written
	// verbatim that ref is a file sarvam-code resume never scans for, so the
	// write must land at the native-shaped path the snapshot's session_meta
	// implies, and resolve-session-file must then discover it.
	t.Setenv("SARVAM_HOME", t.TempDir())
	agent := New()

	sessionID := "01a1e2d3-8a2b-7dc2-b125-885a43ce43f5"
	storeRoot, err := agent.GetSessionDir("")
	if err != nil {
		t.Fatal(err)
	}
	fallback := agent.ResolveSessionFile(storeRoot, sessionID)
	if isNativeRolloutPath(fallback) {
		t.Fatalf("fixture: %q should be the deterministic fallback", fallback)
	}

	session := protocol.AgentSessionJSON{
		SessionID:  sessionID,
		SessionRef: fallback,
		NativeData: []byte(`{"timestamp":"2026-11-15T09:30:00.000Z","type":"session_meta","payload":{"id":"` + sessionID + `","cwd":"/repo","source":"cli"}}` + "\n" +
			`{"timestamp":"2026-11-15T09:30:05.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`),
	}
	if err := agent.WriteSession(session); err != nil {
		t.Fatalf("restore write failed: %v", err)
	}

	want := filepath.Join(storeRoot, "2026", "11", "15", "rollout-2026-11-15T09-30-00-"+sessionID+".jsonl")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("restored snapshot not at the discoverable rollout path %s: %v", want, err)
	}
	if _, err := os.Stat(fallback); err == nil {
		t.Fatalf("the fallback ref %s should not have been written verbatim", fallback)
	}
	if got := agent.ResolveSessionFile(storeRoot, sessionID); got != want {
		t.Fatalf("resolve-session-file = %q, want the restored rollout %q", got, want)
	}

	// A second restore attempt now collides with the restored rollout and
	// must be refused, not clobber it.
	if err := agent.WriteSession(session); err == nil {
		t.Fatal("a second write to the restored rollout must be refused")
	}
}

func TestSafeFilename_DeviceNamesAndColons(t *testing.T) {
	cases := []struct {
		id       string
		hashForm bool
	}{
		{"test-session-123", false}, // ordinary IDs stay greppable
		{"01a1e2d3-8a2b-7dc2-b125-885a43ce43f5", false},
		{"COM1", true},
		{"con", true},
		{"NUL", true},
		{"LPT9.", true}, // trailing dot trims to a bare device name
		{"a:b", true},   // colons are not filename-safe
		{"CON.", true},
	}
	for _, tc := range cases {
		name := safeFilename(tc.id)
		if tc.hashForm && !strings.HasPrefix(name, "~") {
			t.Errorf("safeFilename(%q) = %q, want the hashed form", tc.id, name)
		}
		if !tc.hashForm && name != tc.id {
			t.Errorf("safeFilename(%q) = %q, want passthrough", tc.id, name)
		}
	}
}
