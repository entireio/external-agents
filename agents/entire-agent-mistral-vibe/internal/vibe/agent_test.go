package vibe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

func TestInfo(t *testing.T) {
	agent := New()
	info := agent.Info()

	if info.ProtocolVersion != 1 {
		t.Errorf("protocol_version = %d, want 1", info.ProtocolVersion)
	}
	if info.Name != "mistral-vibe" {
		t.Errorf("name = %q, want %q", info.Name, "mistral-vibe")
	}
	if info.Type != "Mistral Vibe" {
		t.Errorf("type = %q, want %q", info.Type, "Mistral Vibe")
	}
	if !info.Capabilities.Hooks {
		t.Error("hooks capability should be true")
	}
	if !info.Capabilities.TranscriptAnalyzer {
		t.Error("transcript_analyzer capability should be true")
	}
	if !info.Capabilities.TokenCalculator {
		t.Error("token_calculator capability should be true")
	}
	if len(info.HookNames) != 5 {
		t.Errorf("hook_names count = %d, want 5", len(info.HookNames))
	}
	if len(info.ProtectedDirs) != 1 || info.ProtectedDirs[0] != ".vibe" {
		t.Errorf("protected_dirs = %v, want [.vibe]", info.ProtectedDirs)
	}
}

func TestDetect_Present(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".vibe"), 0o700)
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	resp := agent.Detect()
	if !resp.Present {
		t.Error("detect should return true when .vibe/ exists")
	}
}

func TestDetect_Absent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	resp := agent.Detect()
	if resp.Present {
		t.Error("detect should return false when .vibe/ is absent")
	}
}

func TestGetSessionID_FromInput(t *testing.T) {
	agent := New()
	input := &protocol.HookInputJSON{SessionID: "test-id-123"}
	got := agent.GetSessionID(input)
	if got != "test-id-123" {
		t.Errorf("session_id = %q, want %q", got, "test-id-123")
	}
}

func TestGetSessionID_Fallback(t *testing.T) {
	agent := New()
	got := agent.GetSessionID(nil)
	if got == "" {
		t.Error("session_id should not be empty for nil input")
	}
}

func TestGetSessionDir(t *testing.T) {
	agent := New()
	dir, err := agent.GetSessionDir("/tmp/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/tmp/repo/.entire/tmp"
	if dir != want {
		t.Errorf("session_dir = %q, want %q", dir, want)
	}
}

func TestResolveSessionFile(t *testing.T) {
	agent := New()
	got := agent.ResolveSessionFile("/tmp/sessions", "abc-123")
	want := "/tmp/sessions/abc-123.json"
	if got != want {
		t.Errorf("session_file = %q, want %q", got, want)
	}
}

func TestFormatResumeCommand(t *testing.T) {
	agent := New()
	got := agent.FormatResumeCommand("session-xyz")
	want := "vibe --resume session-xyz"
	if got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
}

func TestChunkTranscript(t *testing.T) {
	agent := New()

	t.Run("basic_chunking", func(t *testing.T) {
		content := []byte("abcdefghijklmnopqrst") // 20 bytes
		chunks, err := agent.ChunkTranscript(content, 8)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunks) != 3 {
			t.Errorf("chunks = %d, want 3", len(chunks))
		}
	})

	t.Run("exact_size", func(t *testing.T) {
		content := []byte("abcdefghij") // 10 bytes
		chunks, err := agent.ChunkTranscript(content, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunks) != 1 {
			t.Errorf("chunks = %d, want 1", len(chunks))
		}
	})

	t.Run("empty_content", func(t *testing.T) {
		chunks, err := agent.ChunkTranscript([]byte{}, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunks) != 1 {
			t.Errorf("chunks = %d, want 1 (empty chunk)", len(chunks))
		}
	})

	t.Run("invalid_max_size", func(t *testing.T) {
		_, err := agent.ChunkTranscript([]byte("test"), 0)
		if err == nil {
			t.Error("expected error for max-size 0")
		}
	})
}

func TestReassembleTranscript(t *testing.T) {
	agent := New()
	chunks := [][]byte{[]byte("hello"), []byte(" "), []byte("world")}
	got, err := agent.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("reassembled = %q, want %q", got, "hello world")
	}
}

func TestChunkReassembleRoundTrip(t *testing.T) {
	agent := New()
	original := []byte("The quick brown fox jumps over the lazy dog")
	chunks, err := agent.ChunkTranscript(original, 10)
	if err != nil {
		t.Fatalf("chunk error: %v", err)
	}
	reassembled, err := agent.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatalf("reassemble error: %v", err)
	}
	if string(reassembled) != string(original) {
		t.Errorf("round-trip failed: got %q, want %q", reassembled, original)
	}
}

func TestWriteAndReadSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)
	os.MkdirAll(filepath.Join(dir, ".entire", "tmp"), 0o700)

	agent := New()
	sessionRef := filepath.Join(dir, ".entire", "tmp", "test-session.json")

	err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID:  "test-session",
		AgentName:  "mistral-vibe",
		RepoPath:   dir,
		SessionRef: sessionRef,
		NativeData: []byte(`{"test":"data"}`),
	})
	if err != nil {
		t.Fatalf("write error: %v", err)
	}

	data, err := os.ReadFile(sessionRef)
	if err != nil {
		t.Fatalf("read file error: %v", err)
	}
	if string(data) != `{"test":"data"}` {
		t.Errorf("written data = %q, want %q", data, `{"test":"data"}`)
	}
}
