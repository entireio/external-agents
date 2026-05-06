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

func TestParseHookSessionIDStableAcrossTurnsInSameIDEChat(t *testing.T) {
	// Regression: previously every turn re-resolved the IDE session ID and
	// could rekey the Entire session mid-conversation. Now the resolver
	// reuses the cached Entire session ID when the IDE session ID hasn't
	// changed, so all turns in the same chat share one Entire session.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	t.Setenv("USER_PROMPT", "first")

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, repoRoot)
	index := `[{"sessionId":"chat-1","title":"Chat","dateCreated":"2026-02-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(sessionsDir, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}

	first, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("first ParseHook() error = %v", err)
	}
	if first.PreviousSessionID != "" {
		t.Fatalf("first turn should not rekey, previous_session_id = %q", first.PreviousSessionID)
	}
	if first.SessionID != "chat-1" {
		t.Fatalf("first session_id = %q, want %q", first.SessionID, "chat-1")
	}

	t.Setenv("USER_PROMPT", "second")
	second, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("second ParseHook() error = %v", err)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("second turn rekeyed session: got %q, want stable %q", second.SessionID, first.SessionID)
	}
	if second.PreviousSessionID != "" {
		t.Fatalf("second turn should not rekey, previous_session_id = %q", second.PreviousSessionID)
	}
}

func TestParseHookRekeysAndEmitsPreviousSessionIDOnNewKiroChat(t *testing.T) {
	// Regression: when the user opens a new Kiro chat tab (the IDE session
	// ID on disk changes), the resolver must rekey the Entire session AND
	// emit the prior Entire session ID via PreviousSessionID so downstream
	// consumers can merge or close out the old session instead of orphaning
	// its history.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	t.Setenv("USER_PROMPT", "first")

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, repoRoot)
	if err := os.WriteFile(
		filepath.Join(sessionsDir, "sessions.json"),
		[]byte(`[{"sessionId":"chat-A","title":"A","dateCreated":"2026-02-01T00:00:00Z"}]`),
		0o600,
	); err != nil {
		t.Fatalf("write sessions.json (A): %v", err)
	}

	first, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("first ParseHook() error = %v", err)
	}
	if first.SessionID != "chat-A" {
		t.Fatalf("first session_id = %q, want %q", first.SessionID, "chat-A")
	}

	// User opens a new Kiro chat tab — the IDE writes a new sessionId entry
	// that's now the latest.
	if err := os.WriteFile(
		filepath.Join(sessionsDir, "sessions.json"),
		[]byte(`[
  {"sessionId":"chat-A","title":"A","dateCreated":"2026-02-01T00:00:00Z"},
  {"sessionId":"chat-B","title":"B","dateCreated":"2026-03-01T00:00:00Z"}
]`),
		0o600,
	); err != nil {
		t.Fatalf("write sessions.json (B): %v", err)
	}

	t.Setenv("USER_PROMPT", "second")
	second, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("second ParseHook() error = %v", err)
	}
	if second.SessionID != "chat-B" {
		t.Fatalf("second session_id = %q, want %q", second.SessionID, "chat-B")
	}
	if second.PreviousSessionID != first.SessionID {
		t.Fatalf("second previous_session_id = %q, want %q", second.PreviousSessionID, first.SessionID)
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
