package dsh

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-dsh/internal/protocol"
)

func TestInfo(t *testing.T) {
	info := (&Agent{}).Info()
	if info.ProtocolVersion != 1 || info.Name != "dsh" || info.Type == "" || info.Description == "" {
		t.Fatalf("unexpected info: %+v", info)
	}
	if len(info.HookNames) != len(hookNames) {
		t.Errorf("hook names = %v", info.HookNames)
	}
	caps := info.Capabilities
	if !caps.Hooks || !caps.TranscriptAnalyzer || !caps.TranscriptPreparer || !caps.TokenCalculator || !caps.CompactTranscript {
		t.Errorf("missing required capabilities: %+v", caps)
	}
	if caps.TextGenerator || caps.HookResponseWriter || caps.SubagentAwareExtractor {
		t.Errorf("unexpected capability: %+v", caps)
	}
}

func TestWriteReadSessionRoundTrip(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("DSH_HOME", t.TempDir())

	agent := &Agent{}
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	ref := agent.ResolveSessionFile(dir, "session-roundtrip")
	session := protocol.AgentSessionJSON{
		SessionID:     "session-roundtrip",
		AgentName:     "dsh",
		RepoPath:      repo,
		SessionRef:    ref,
		StartTime:     "2026-01-01T00:00:00Z",
		NativeData:    []byte(`{"test":true}` + "\n"),
		ModifiedFiles: []string{},
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}
	if err := agent.WriteSession(session); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	read, err := agent.ReadSession(&protocol.HookInputJSON{SessionID: session.SessionID, SessionRef: ref})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if read.SessionID != session.SessionID || read.AgentName != "dsh" || read.RepoPath != repo || read.SessionRef != ref {
		t.Errorf("unexpected read session: %+v", read)
	}
	if !bytes.Equal(read.NativeData, session.NativeData) {
		t.Errorf("native data = %q, want %q", read.NativeData, session.NativeData)
	}
	if read.ModifiedFiles == nil || read.NewFiles == nil || read.DeletedFiles == nil {
		t.Error("file lists must be initialized")
	}
}

func TestWriteSessionRejectsOutsideRef(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("DSH_HOME", t.TempDir())

	agent := &Agent{}
	err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  "session-outside",
		AgentName:  "dsh",
		RepoPath:   repo,
		SessionRef: filepath.Join(t.TempDir(), "outside.jsonl"),
		NativeData: []byte("data"),
	})
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected containment error, got %v", err)
	}
}

func TestWriteSessionRejectsRelativeRef(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("DSH_HOME", t.TempDir())

	err := (&Agent{}).WriteSession(protocol.AgentSessionJSON{
		SessionID:  "session-relative",
		AgentName:  "dsh",
		RepoPath:   repo,
		SessionRef: "session-relative.jsonl",
		NativeData: []byte("data"),
	})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute-path error, got %v", err)
	}
}

func TestReadTranscriptArbitraryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.json")
	want := []byte(`{"test":"data"}`)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	got, err := (&Agent{}).ReadTranscript(path)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestChunkAndReassemble(t *testing.T) {
	agent := &Agent{}
	content := bytes.Repeat([]byte("hello\n"), 100)
	chunks, err := agent.ChunkTranscript(content, 64)
	if err != nil {
		t.Fatalf("ChunkTranscript: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	reassembled, err := agent.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatalf("ReassembleTranscript: %v", err)
	}
	if !bytes.Equal(reassembled, content) {
		t.Error("round-trip mismatch")
	}
	if _, err := agent.ChunkTranscript(content, 0); err == nil {
		t.Error("max-size 0 should fail")
	}
}

func TestFormatResumeCommand(t *testing.T) {
	got := (&Agent{}).FormatResumeCommand("session-123")
	if !strings.Contains(got, "session-123") || !strings.Contains(got, "dsh") {
		t.Errorf("resume command = %q", got)
	}

	hostile := (&Agent{}).FormatResumeCommand("session-1; echo PWNED")
	if hostile != "dsh web" {
		t.Errorf("hostile resume command = %q, want dsh web", hostile)
	}
	for _, id := range []string{"", "-rf", "a b", "session$(id)", "session|cat"} {
		if cmd := (&Agent{}).FormatResumeCommand(id); cmd != "dsh web" {
			t.Errorf("FormatResumeCommand(%q) = %q, want dsh web", id, cmd)
		}
	}
}

func TestPrepareTranscriptArbitraryFile(t *testing.T) {
	t.Setenv("DSH_HOME", t.TempDir())
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := (&Agent{}).PrepareTranscript(path); err != nil {
		t.Fatalf("PrepareTranscript on an ordinary jsonl file: %v", err)
	}
}

func TestWriteSessionWithoutRepoPathInsideCache(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("DSH_HOME", t.TempDir())
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := &Agent{}
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	ref := agent.ResolveSessionFile(dir, "session-no-repo")
	err = agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  "session-no-repo",
		AgentName:  "dsh",
		SessionRef: ref,
		NativeData: []byte("{}\n"),
	})
	if err != nil {
		t.Fatalf("WriteSession without RepoPath: %v", err)
	}
}

func TestWriteSessionRestoresNativeTranscript(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := &Agent{}
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	id := "session-restore"
	ref := agent.ResolveSessionFile(dir, id)
	nativeData := []byte(
		`{"type":"session","version":4,"id":"` + id + `","createdAt":1700000000000,"cwd":"` + strings.ReplaceAll(repo, "\\", "\\\\") + `","isSeeded":false,"delegationDepth":0}` + "\n" +
			`{"type":"turn/start","seq":0,"time":1700000001000,"data":{"turn":1}}` + "\n",
	)
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  id,
		AgentName:  "dsh",
		RepoPath:   repo,
		SessionRef: ref,
		NativeData: nativeData,
	}); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	nativePath := filepath.Join(home, "sessions", projectKey(repo), id, "session.v4.jsonl.zstd")
	decoded, err := readTranscriptFile(nativePath)
	if err != nil {
		t.Fatalf("restored native transcript: %v", err)
	}
	if !bytes.Contains(decoded, []byte(`"type":"session"`)) || !bytes.Contains(decoded, []byte(`"type":"turn/start"`)) {
		t.Errorf("native transcript content = %q", string(decoded))
	}

	// An existing native generation belongs to a live DSH session and must not
	// be overwritten by a restore.
	if err := os.WriteFile(nativePath, []byte("owned-by-dsh"), 0o600); err != nil {
		t.Fatalf("seed native file: %v", err)
	}
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  id,
		AgentName:  "dsh",
		RepoPath:   repo,
		SessionRef: ref,
		NativeData: nativeData,
	}); err != nil {
		t.Fatalf("second WriteSession: %v", err)
	}
	after, err := os.ReadFile(nativePath)
	if err != nil {
		t.Fatalf("read native file: %v", err)
	}
	if string(after) != "owned-by-dsh" {
		t.Error("restore overwrote an existing native DSH generation")
	}
}

func TestWriteSessionSymlinkedDSHHome(t *testing.T) {
	realHome := t.TempDir()
	linkHome := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(realHome, linkHome); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("DSH_HOME", linkHome)
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := &Agent{}
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	ref := agent.ResolveSessionFile(dir, "session-symlink-write")
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  "session-symlink-write",
		AgentName:  "dsh",
		RepoPath:   repo,
		SessionRef: ref,
		NativeData: []byte("{}\n"),
	}); err != nil {
		t.Fatalf("WriteSession with symlinked DSH_HOME: %v", err)
	}
}

func TestWriteSessionRestoresNativeAtCurrentPath(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := &Agent{}
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	sessionID := "session-cross"
	ref := agent.ResolveSessionFile(dir, sessionID)
	nativeData := []byte(
		`{"type":"session","version":4,"id":"session-old","createdAt":1700000000000,"cwd":"/old/checkout/repo","isSeeded":false,"delegationDepth":0}` + "\n" +
			`{"type":"turn/start","seq":0,"time":1700000001000,"data":{"turn":1}}` + "\n",
	)
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  sessionID,
		AgentName:  "dsh",
		RepoPath:   repo,
		SessionRef: ref,
		NativeData: nativeData,
	}); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	nativePath := filepath.Join(home, "sessions", projectKey(repo), sessionID, "session.v4.jsonl.zstd")
	decoded, err := readTranscriptFile(nativePath)
	if err != nil {
		t.Fatalf("read restored native: %v", err)
	}
	header, err := parseSessionHeader(decoded)
	if err != nil {
		t.Fatalf("parse restored header: %v", err)
	}
	if header.ID != sessionID {
		t.Errorf("restored header id = %q, want %q", header.ID, sessionID)
	}
	if header.CWD != repo {
		t.Errorf("restored header cwd = %q, want %q", header.CWD, repo)
	}
	oldPath := filepath.Join(home, "sessions", projectKey("/old/checkout/repo"), "session-old", "session.v4.jsonl.zstd")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("restore wrote under the old project key: %v", err)
	}
}

func TestWriteSessionDoesNotShadowPlainNativeGeneration(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := &Agent{}
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	sessionID := "session-plain"
	ref := agent.ResolveSessionFile(dir, sessionID)
	nativeDir := filepath.Join(home, "sessions", projectKey(repo), sessionID)
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "session.v4.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("seed plain native: %v", err)
	}
	nativeData := []byte(
		`{"type":"session","version":4,"id":"` + sessionID + `","createdAt":1700000000000,"cwd":"` + repo + `","isSeeded":false,"delegationDepth":0}` + "\n",
	)
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  sessionID,
		AgentName:  "dsh",
		RepoPath:   repo,
		SessionRef: ref,
		NativeData: nativeData,
	}); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nativeDir, "session.v4.jsonl.zstd")); !os.IsNotExist(err) {
		t.Error("restore created a zstd generation beside a plain native generation")
	}
}

func TestWriteSessionRestoresNativeUnderPhysicalSymlinkPath(t *testing.T) {
	physical := t.TempDir()
	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(physical, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", link)
	agent := &Agent{}
	dir, err := agent.GetSessionDir(link)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	sessionID := "session-symlink-restore"
	ref := agent.ResolveSessionFile(dir, sessionID)
	nativeData := []byte(
		`{"type":"session","version":4,"id":"old-id","createdAt":1700000000000,"cwd":"/old/checkout","isSeeded":false,"delegationDepth":0}` + "\n",
	)
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  sessionID,
		AgentName:  "dsh",
		RepoPath:   link,
		SessionRef: ref,
		NativeData: nativeData,
	}); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	nativePath := filepath.Join(home, "sessions", projectKey(physicalPath(link)), sessionID, "session.v4.jsonl.zstd")
	decoded, err := readTranscriptFile(nativePath)
	if err != nil {
		t.Fatalf("read restored native: %v", err)
	}
	header, err := parseSessionHeader(decoded)
	if err != nil {
		t.Fatalf("parse header: %v", err)
	}
	if header.CWD != physicalPath(link) {
		t.Errorf("header cwd = %q, want physical %q", header.CWD, physicalPath(link))
	}
	if header.ID != sessionID {
		t.Errorf("header id = %q, want %q", header.ID, sessionID)
	}
}

func TestWriteSessionRecoversStaleRestoreLock(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := &Agent{}
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	sessionID := "session-stale-lock"
	ref := agent.ResolveSessionFile(dir, sessionID)
	nativeDir := filepath.Join(home, "sessions", projectKey(repo), sessionID)
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native: %v", err)
	}
	lockPath := filepath.Join(nativeDir, ".entire-restore.lock")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("age lock: %v", err)
	}
	nativeData := []byte(
		`{"type":"session","version":4,"id":"` + sessionID + `","createdAt":1700000000000,"cwd":"` + filepath.ToSlash(repo) + `","isSeeded":false,"delegationDepth":0}` + "\n",
	)
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  sessionID,
		AgentName:  "dsh",
		RepoPath:   repo,
		SessionRef: ref,
		NativeData: nativeData,
	}); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nativeDir, "session.v4.jsonl.zstd")); err != nil {
		t.Errorf("stale lock blocked native restore: %v", err)
	}
}
