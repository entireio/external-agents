package vibe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHook_SessionStart(t *testing.T) {
	agent := New()
	payload := VibeHookPayload{
		HookEventName: "session_start",
		CWD:           "/tmp/test",
		SessionID:     "0e9f7293-0151-4178-ba58-2c48c5abb8df",
	}
	input, _ := json.Marshal(payload)

	event, err := agent.ParseHook(HookNameSessionStart, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil")
	}
	if event.Type != EventTypeSessionStart {
		t.Errorf("type = %d, want %d", event.Type, EventTypeSessionStart)
	}
	if event.SessionID != "0e9f7293-0151-4178-ba58-2c48c5abb8df" {
		t.Errorf("session_id = %q, want %q", event.SessionID, "0e9f7293-0151-4178-ba58-2c48c5abb8df")
	}
	if event.Timestamp == "" {
		t.Error("timestamp should be set")
	}
}

func TestParseHook_UserPromptSubmit(t *testing.T) {
	agent := New()
	payload := VibeHookPayload{
		HookEventName: "user_prompt_submit",
		CWD:           "/tmp/test",
		SessionID:     "test-session",
		Prompt:        "fix the login bug",
	}
	input, _ := json.Marshal(payload)

	event, err := agent.ParseHook(HookNameUserPromptSubmit, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil")
	}
	if event.Type != EventTypeTurnStart {
		t.Errorf("type = %d, want %d", event.Type, EventTypeTurnStart)
	}
	if event.Prompt != "fix the login bug" {
		t.Errorf("prompt = %q, want %q", event.Prompt, "fix the login bug")
	}
}

func TestParseHook_TurnEnd(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	payload := VibeHookPayload{
		HookEventName: "turn_end",
		CWD:           dir,
		SessionID:     "0e9f7293-0151-4178-ba58-2c48c5abb8df",
	}
	input, _ := json.Marshal(payload)

	event, err := agent.ParseHook(HookNameTurnEnd, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil")
	}
	if event.Type != EventTypeTurnEnd {
		t.Errorf("type = %d, want %d", event.Type, EventTypeTurnEnd)
	}
	if event.SessionID != "0e9f7293-0151-4178-ba58-2c48c5abb8df" {
		t.Errorf("session_id = %q", event.SessionID)
	}
	if event.SessionRef == "" {
		t.Error("session_ref should not be empty (placeholder should be created)")
	}
}

func TestCacheTranscriptForTurnEnd_Placeholder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	ref := agent.cacheTranscriptForTurnEnd("test-session-123")
	if ref == "" {
		t.Fatal("cache ref should not be empty")
	}

	data, err := os.ReadFile(ref)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("placeholder content = %q, want %q", data, "{}")
	}

	want := filepath.Join(dir, ".entire", "tmp", "test-session-123.json")
	if ref != want {
		t.Errorf("ref = %q, want %q", ref, want)
	}
}

func TestParseHook_PreToolUse_ReturnsNil(t *testing.T) {
	agent := New()
	payload := VibeHookPayload{
		HookEventName: "pre_tool_use",
		CWD:           "/tmp/test",
		SessionID:     "test",
		ToolName:      "write_file",
	}
	input, _ := json.Marshal(payload)

	event, err := agent.ParseHook(HookNamePreToolUse, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("pre-tool-use should return nil, got type=%d", event.Type)
	}
}

func TestParseHook_PostToolUse_ReturnsNil(t *testing.T) {
	agent := New()
	payload := VibeHookPayload{
		HookEventName: "post_tool_use",
		CWD:           "/tmp/test",
		SessionID:     "test",
		ToolName:      "write_file",
		ToolOutcome:   "success",
	}
	input, _ := json.Marshal(payload)

	event, err := agent.ParseHook(HookNamePostToolUse, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("post-tool-use should return nil, got type=%d", event.Type)
	}
}

func TestParseHook_EmptyInput(t *testing.T) {
	agent := New()
	event, err := agent.ParseHook(HookNameSessionStart, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("session-start with empty input should still return an event")
	}
	if event.SessionID == "" {
		t.Error("session_id should be generated")
	}
}

func TestParseHook_UnknownHook(t *testing.T) {
	agent := New()
	event, err := agent.ParseHook("unknown-hook", []byte("{}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Error("unknown hook should return nil")
	}
}

func TestParseHook_MalformedJSON(t *testing.T) {
	agent := New()
	_, err := agent.ParseHook(HookNameSessionStart, []byte("not json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestInstallHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	count, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("install error: %v", err)
	}
	if count != 5 {
		t.Errorf("hooks_installed = %d, want 5", count)
	}

	configPath := filepath.Join(dir, ".vibe", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)

	for _, hook := range []string{"session_start", "user_prompt_submit", "pre_tool_use", "post_tool_use", "turn_end"} {
		if !strings.Contains(content, "[[hooks."+hook+"]]") {
			t.Errorf("config should contain [[hooks.%s]]", hook)
		}
	}
	if !strings.Contains(content, "entire hooks mistral-vibe") {
		t.Error("config should contain hook command")
	}
}

func TestInstallHooks_Idempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	agent.InstallHooks(false, false)

	count, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("second install error: %v", err)
	}
	if count != 0 {
		t.Errorf("second install should return 0, got %d", count)
	}
}

func TestInstallHooks_LocalDev(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	agent.InstallHooks(true, true)

	data, err := os.ReadFile(filepath.Join(dir, ".vibe", "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "go run") {
		t.Error("local-dev mode should use 'go run' command")
	}
}

func TestUninstallHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	agent.InstallHooks(false, false)

	if !agent.AreHooksInstalled() {
		t.Fatal("hooks should be installed")
	}

	err := agent.UninstallHooks()
	if err != nil {
		t.Fatalf("uninstall error: %v", err)
	}

	if agent.AreHooksInstalled() {
		t.Error("hooks should not be installed after uninstall")
	}
}

func TestAreHooksInstalled_NoConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", dir)

	agent := New()
	if agent.AreHooksInstalled() {
		t.Error("should return false when no config exists")
	}
}

