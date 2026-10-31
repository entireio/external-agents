package gemini

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-gemini/internal/protocol"
)

func TestInfo(t *testing.T) {
	a := New()
	info := a.Info()

	if info.Name != AgentName {
		t.Errorf("Name = %q, want %q", info.Name, AgentName)
	}
	if info.Type != AgentType {
		t.Errorf("Type = %q, want %q", info.Type, AgentType)
	}
	if !info.Capabilities.Hooks {
		t.Error("Hooks capability should be true")
	}
	if !info.Capabilities.TranscriptAnalyzer {
		t.Error("TranscriptAnalyzer capability should be true")
	}
	if !info.Capabilities.CompactTranscript {
		t.Error("CompactTranscript capability should be true")
	}
	if len(info.HookNames) != 8 {
		t.Errorf("HookNames count = %d, want 8", len(info.HookNames))
	}
}

func TestDetect(t *testing.T) {
	a := New()
	a.LookPath = func(name string) (string, error) {
		if name == geminiBinary {
			return "/usr/bin/gemini", nil
		}
		return "", os.ErrNotExist
	}
	if !a.Detect().Present {
		t.Error("Detect should return true when gemini is on PATH")
	}

	a.LookPath = func(name string) (string, error) {
		return "", os.ErrNotExist
	}
	if a.Detect().Present {
		t.Error("Detect should return false when gemini is not on PATH")
	}
}

func TestGetSessionIDFromInput(t *testing.T) {
	a := New()
	input := &protocol.HookInputJSON{SessionID: "abc-123"}
	sid := a.GetSessionID(input)
	if sid != "abc-123" {
		t.Errorf("GetSessionID = %q, want abc-123", sid)
	}
}

func TestGetSessionIDFromEnv(t *testing.T) {
	a := New()
	t.Setenv("GEMINI_SESSION_ID", "env-session-456")

	sid := a.GetSessionID(nil)
	if sid != "env-session-456" {
		t.Errorf("GetSessionID(nil) = %q, want env-session-456", sid)
	}
}

func TestGetSessionIDFallback(t *testing.T) {
	a := New()
	t.Setenv("GEMINI_SESSION_ID", "")

	sid := a.GetSessionID(nil)
	if sid != stubSessionID {
		t.Errorf("GetSessionID(nil) = %q, want %q", sid, stubSessionID)
	}
}

func TestGetSessionDir(t *testing.T) {
	a := New()
	dir, err := a.GetSessionDir("/home/user/myproject")
	if err != nil {
		t.Fatalf("GetSessionDir error: %v", err)
	}
	if dir == "" {
		t.Error("GetSessionDir returned empty string")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("GetSessionDir should return absolute path, got %q", dir)
	}
}

func TestResolveSessionFile(t *testing.T) {
	a := New()
	path := a.ResolveSessionFile("/tmp/entire-gemini/abc123", "my-session-001")
	expected := filepath.Join("/tmp/entire-gemini/abc123", "my-session-001.jsonl")
	if path != expected {
		t.Errorf("ResolveSessionFile = %q, want %q", path, expected)
	}
}

func TestFormatResumeCommand(t *testing.T) {
	a := New()
	cmd := a.FormatResumeCommand("sess-123")
	if cmd != "gemini --resume sess-123" {
		t.Errorf("FormatResumeCommand = %q, want %q", cmd, "gemini --resume sess-123")
	}

	cmd = a.FormatResumeCommand("")
	if cmd != "gemini --continue" {
		t.Errorf("FormatResumeCommand(\"\") = %q, want %q", cmd, "gemini --continue")
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"abc-123", "abc-123"},
		{"a b c", "a_b_c"},
		{"hello.txt", "hello.txt"},
		{"path/with/slashes", "path_with_slashes"},
		{"", ""},
	}
	for _, tc := range tests {
		got := safeFilename(tc.input)
		if got != tc.expect {
			t.Errorf("safeFilename(%q) = %q, want %q", tc.input, got, tc.expect)
		}
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("abc-123")
	if got != "abc-123" {
		t.Errorf("shellQuote(abc-123) = %q, want abc-123", got)
	}

	got = shellQuote("hello world")
	want := "'hello world'"
	if got != want {
		t.Errorf("shellQuote(hello world) = %q, want %q", got, want)
	}

	got = shellQuote("")
	if got != "''" {
		t.Errorf("shellQuote() = %q, want ''", got)
	}
}
