package kiro

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHookAgentSpawnCachesStableSessionID(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	event, err := New().ParseHook(HookNameAgentSpawn, []byte(`{"cwd":"/tmp/repo"}`))
	if err != nil {
		t.Fatalf("ParseHook(agent-spawn) error = %v", err)
	}
	if event == nil {
		t.Fatal("expected SessionStart event")
	}
	if event.Type != 1 {
		t.Fatalf("event.Type = %d, want %d", event.Type, 1)
	}
	if event.SessionID == "" || event.SessionID == "repo" {
		t.Fatalf("session_id = %q, want generated stable ID", event.SessionID)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".entire", "tmp", "kiro-active-session"))
	if err != nil {
		t.Fatalf("read cached session id: %v", err)
	}
	if string(data) != event.SessionID {
		t.Fatalf("cached session id = %q, want %q", string(data), event.SessionID)
	}
}

func TestParseHookUserPromptSubmitUsesCachedSessionID(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	spawn, err := New().ParseHook(HookNameAgentSpawn, []byte(`{"cwd":"/tmp/repo"}`))
	if err != nil {
		t.Fatalf("ParseHook(agent-spawn) error = %v", err)
	}

	event, err := New().ParseHook(HookNameUserPromptSubmit, []byte(`{"prompt":"write tests"}`))
	if err != nil {
		t.Fatalf("ParseHook(user-prompt-submit) error = %v", err)
	}
	if event == nil {
		t.Fatal("expected TurnStart event")
	}
	if event.Type != 2 {
		t.Fatalf("event.Type = %d, want %d", event.Type, 2)
	}
	if event.SessionID != spawn.SessionID {
		t.Fatalf("session_id = %q, want cached %q", event.SessionID, spawn.SessionID)
	}
	if event.Prompt != "write tests" {
		t.Fatalf("prompt = %q, want %q", event.Prompt, "write tests")
	}
}

func TestParseHookUserPromptSubmitSupportsIDEFallback(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	t.Setenv("USER_PROMPT", "ide prompt")

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, repoRoot)
	index := `[
  {"sessionId":"older","title":"Old","dateCreated":"2026-01-01T00:00:00Z"},
  {"sessionId":"latest","title":"New","dateCreated":"2026-02-01T00:00:00Z"}
]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}

	event, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("ParseHook(user-prompt-submit) error = %v", err)
	}
	if event == nil {
		t.Fatal("expected TurnStart event")
	}
	if event.Type != 2 {
		t.Fatalf("event.Type = %d, want %d", event.Type, 2)
	}
	if event.SessionID != "latest" {
		t.Fatalf("session_id = %q, want IDE session %q", event.SessionID, "latest")
	}
	if event.Prompt != "ide prompt" {
		t.Fatalf("prompt = %q, want %q", event.Prompt, "ide prompt")
	}
}

func TestParseHookUserPromptSubmitPrefersIDESessionOverStaleCache(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	t.Setenv("USER_PROMPT", "ide prompt")

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, repoRoot)
	index := `[{"sessionId":"latest","title":"New","dateCreated":"2026-02-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	seedSessionIDCache(t, repoRoot, "stale-session")

	event, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("ParseHook(user-prompt-submit) error = %v", err)
	}
	if event == nil {
		t.Fatal("expected TurnStart event")
	}
	if event.SessionID != "latest" {
		t.Fatalf("session_id = %q, want IDE session %q", event.SessionID, "latest")
	}
}

func TestParseHookPassThroughHooksReturnNil(t *testing.T) {
	for _, hookName := range []string{HookNamePreToolUse, HookNamePostToolUse} {
		event, err := New().ParseHook(hookName, []byte(`{"tool_name":"read"}`))
		if err != nil {
			t.Fatalf("ParseHook(%s) error = %v", hookName, err)
		}
		if event != nil {
			t.Fatalf("ParseHook(%s) = %#v, want nil", hookName, event)
		}
	}
}

func TestParseHookToolInputAsObject(t *testing.T) {
	payload := `{"hook_event_name":"pre-tool-use","tool_name":"write","tool_input":{"file_path":"/tmp/main.go","content":"package main"}}`
	for _, hookName := range []string{HookNamePreToolUse, HookNamePostToolUse} {
		event, err := New().ParseHook(hookName, []byte(payload))
		if err != nil {
			t.Fatalf("ParseHook(%s) with object tool_input error = %v", hookName, err)
		}
		if event != nil {
			t.Fatalf("ParseHook(%s) = %#v, want nil", hookName, event)
		}
	}
}

func TestParseHookStopUsesCachedSessionIDAndClearsCache(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	spawn, err := New().ParseHook(HookNameAgentSpawn, []byte(`{"cwd":"/tmp/repo"}`))
	if err != nil {
		t.Fatalf("ParseHook(agent-spawn) error = %v", err)
	}

	event, err := New().ParseHook(HookNameStop, []byte(`{"cwd":"/tmp/repo"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	if event == nil {
		t.Fatal("expected TurnEnd event")
	}
	if event.Type != 3 {
		t.Fatalf("event.Type = %d, want %d", event.Type, 3)
	}
	if event.SessionID != spawn.SessionID {
		t.Fatalf("session_id = %q, want cached %q", event.SessionID, spawn.SessionID)
	}

	wantRef := filepath.Join(repoRoot, ".entire", "tmp", spawn.SessionID+".json")
	if event.SessionRef != wantRef {
		t.Fatalf("session_ref = %q, want %q", event.SessionRef, wantRef)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".entire", "tmp", "kiro-active-session"))
	if err != nil {
		t.Fatalf("read cached session id after stop: %v", err)
	}
	if string(data) != spawn.SessionID {
		t.Fatalf("cached session id after stop = %q, want %q", string(data), spawn.SessionID)
	}
}

func TestParseHookStopWithoutCachedSessionIDUsesNonPredictableFallback(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	event, err := New().ParseHook(HookNameStop, []byte(`{"cwd":"/tmp/my-repo"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	if event == nil {
		t.Fatal("expected TurnEnd event")
	}
	if event.Type != 3 {
		t.Fatalf("event.Type = %d, want %d", event.Type, 3)
	}
	if event.SessionID == "" {
		t.Fatal("session_id should not be empty")
	}
	if event.SessionID == "my-repo" || event.SessionID == stubSessionID {
		t.Fatalf("session_id = %q, want generated non-predictable fallback", event.SessionID)
	}

	wantRef := filepath.Join(repoRoot, ".entire", "tmp", event.SessionID+".json")
	if event.SessionRef != wantRef {
		t.Fatalf("session_ref = %q, want %q", event.SessionRef, wantRef)
	}
}
