package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

func TestExtractSessionIDFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{
			path: "/Users/test/.pi/agent/sessions/--Users-test--/2026-03-27T21-38-13-384Z_34567c89-98b3-4cc3-a76d-1a4a67193648.jsonl",
			want: "34567c89-98b3-4cc3-a76d-1a4a67193648",
		},
		{
			path: "session_abc123.jsonl",
			want: "abc123",
		},
		{
			path: "no-underscore.jsonl",
			want: "no-underscore",
		},
		{
			path: "",
			want: "",
		},
	}

	for _, tt := range tests {
		got := extractSessionIDFromPath(tt.path)
		if got != tt.want {
			t.Errorf("extractSessionIDFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestParseHook_SessionStart(t *testing.T) {
	agent := New()
	payload := `{"type":"session_start","cwd":"/test","session_file":"/tmp/2026-01-01T00-00-00-000Z_test-uuid.jsonl"}`

	event, err := agent.ParseHook("session_start", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != 1 {
		t.Errorf("Type = %d, want 1 (SessionStart)", event.Type)
	}
	if event.SessionID != "test-uuid" {
		t.Errorf("SessionID = %q, want %q", event.SessionID, "test-uuid")
	}
}

func TestParseHook_TurnStart(t *testing.T) {
	agent := New()
	payload := `{"type":"before_agent_start","cwd":"/test","session_file":"/tmp/2026-01-01T00-00-00-000Z_test-uuid.jsonl","prompt":"hello"}`

	event, err := agent.ParseHook("before_agent_start", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != 2 {
		t.Errorf("Type = %d, want 2 (TurnStart)", event.Type)
	}
	if event.Prompt != "hello" {
		t.Errorf("Prompt = %q, want %q", event.Prompt, "hello")
	}
}

func TestParseHook_TurnEnd(t *testing.T) {
	agent := New()
	payload := `{"type":"agent_end","cwd":"/test","session_file":"/tmp/2026-01-01T00-00-00-000Z_test-uuid.jsonl"}`

	event, err := agent.ParseHook("agent_end", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != 3 {
		t.Errorf("Type = %d, want 3 (TurnEnd)", event.Type)
	}
}

func TestParseHook_SessionShutdown(t *testing.T) {
	agent := New()
	payload := `{"type":"session_shutdown"}`

	event, err := agent.ParseHook("session_shutdown", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Error("expected nil event for session_shutdown")
	}
}

func TestParseHook_EmptyInput(t *testing.T) {
	agent := New()
	event, err := agent.ParseHook("session_start", nil)
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Error("expected nil event for empty input")
	}
}

func TestParseHook_UnknownHook(t *testing.T) {
	agent := New()
	event, err := agent.ParseHook("unknown", []byte(`{"type":"unknown"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Error("expected nil event for unknown hook")
	}
}

func TestGenerateExtension(t *testing.T) {
	got := generateExtension()
	requiredSnippets := []string{
		`import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";`,
		`pi.on("tool_call", async (event) => {`,
		`if (event.toolName !== "bash") {`,
		`input.command = "export GIT_TERMINAL_PROMPT=0\n" + input.command;`,
		`pi.on("session_start", async (_event, ctx) => {`,
		`pi.on("before_agent_start", async (event, ctx) => {`,
		`pi.on("agent_end", async (_event, ctx) => {`,
		`pi.on("session_shutdown", async () => {`,
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(got, snippet) {
			t.Fatalf("generateExtension() missing snippet %q\n--- got ---\n%s", snippet, got)
		}
	}
}

func TestInstallAndUninstallHooks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	agent := New()

	if agent.AreHooksInstalled() {
		t.Error("hooks should not be installed initially")
	}

	count, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Errorf("InstallHooks() = %d, want 4", count)
	}

	if !agent.AreHooksInstalled() {
		t.Error("hooks should be installed after InstallHooks")
	}

	extPath := filepath.Join(tmp, extensionFile)
	data, err := os.ReadFile(extPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != generateExtension() {
		t.Fatal("installed extension does not match generated extension")
	}

	// Idempotent install should return 0.
	count, err = agent.InstallHooks(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("idempotent InstallHooks() = %d, want 0", count)
	}

	// Force install should return 4.
	count, err = agent.InstallHooks(false, true)
	if err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Errorf("forced InstallHooks() = %d, want 4", count)
	}

	err = agent.UninstallHooks()
	if err != nil {
		t.Fatal(err)
	}

	if agent.AreHooksInstalled() {
		t.Error("hooks should not be installed after UninstallHooks")
	}
}

func TestParseHook_SessionIDFromInput(t *testing.T) {
	agent := New()
	payload, _ := json.Marshal(piHookPayload{
		Type:      "session_start",
		SessionID: "explicit-id",
	})

	event, err := agent.ParseHook("session_start", payload)
	if err != nil {
		t.Fatal(err)
	}
	if event.SessionID != "explicit-id" {
		t.Errorf("SessionID = %q, want %q", event.SessionID, "explicit-id")
	}
}

func TestInfo(t *testing.T) {
	agent := New()
	info := agent.Info()

	if info.ProtocolVersion != protocol.ProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", info.ProtocolVersion, protocol.ProtocolVersion)
	}
	if info.Name != "pi" {
		t.Errorf("Name = %q, want %q", info.Name, "pi")
	}
	if !info.Capabilities.Hooks {
		t.Error("expected hooks capability")
	}
	if !info.Capabilities.TranscriptAnalyzer {
		t.Error("expected transcript_analyzer capability")
	}
	if !info.Capabilities.TokenCalculator {
		t.Error("expected token_calculator capability")
	}
	if !info.Capabilities.CompactTranscript {
		t.Error("expected compact_transcript capability")
	}
}
