package kiro

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeIDESessionFile creates a workspace-sessions transcript file with a
// guaranteed mtime so tests can deterministically control which session is
// considered "most recently active" by the mtime-based resolver.
func writeIDESessionFile(t *testing.T, sessionsDir, sessionID string, content string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(sessionsDir, sessionID+".json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

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
	writeIDESessionFile(t, sessionsDir, "older", `{"history":[]}`, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	writeIDESessionFile(t, sessionsDir, "latest", `{"history":[]}`, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))

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
	writeIDESessionFile(t, sessionsDir, "latest", `{"history":[]}`, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
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
	// Each Kiro IDE chat is keyed by its UUID for the lifetime of the chat.
	// Two turns in the same chat (same `<id>.json` file getting touched)
	// must produce the same Entire session ID without rekey.
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
	writeIDESessionFile(t, sessionsDir, "chat-1", `{"history":[]}`, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))

	first, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("first ParseHook() error = %v", err)
	}
	if first.SessionID != "chat-1" {
		t.Fatalf("first session_id = %q, want %q", first.SessionID, "chat-1")
	}

	t.Setenv("USER_PROMPT", "second")
	// Simulate the IDE writing the second prompt — refresh chat-1's mtime.
	writeIDESessionFile(t, sessionsDir, "chat-1", `{"history":[]}`, time.Date(2026, 2, 1, 0, 1, 0, 0, time.UTC))

	second, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("second ParseHook() error = %v", err)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("second turn rekeyed session: got %q, want stable %q", second.SessionID, first.SessionID)
	}
}

func TestParseHookSwitchingKiroChatTabsResolvesEachTabIndependently(t *testing.T) {
	// Regression: a previous design tied the resolved Entire session to a
	// repo-global cache, which meant switching to an older tab — or having
	// two tabs interleaved — would silently rekey one chat's prompts onto
	// another chat's session ID. Each Kiro chat must produce a distinct,
	// deterministic Entire session ID derived from the active IDE
	// `<id>.json` file's mtime, so tab switches resolve correctly without
	// any cross-tab state.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)
	t.Setenv("USER_PROMPT", "in tab A")

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, repoRoot)
	if err := os.WriteFile(
		filepath.Join(sessionsDir, "sessions.json"),
		[]byte(`[
  {"sessionId":"chat-A","title":"A","dateCreated":"2026-02-01T00:00:00Z"},
  {"sessionId":"chat-B","title":"B","dateCreated":"2026-03-01T00:00:00Z"}
]`),
		0o600,
	); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	// User starts in tab A — A's transcript was just written.
	writeIDESessionFile(t, sessionsDir, "chat-A", `{"history":[]}`, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	writeIDESessionFile(t, sessionsDir, "chat-B", `{"history":[]}`, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	// Bump A again so it's unambiguously newest.
	writeIDESessionFile(t, sessionsDir, "chat-A", `{"history":[]}`, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))

	turnA, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("turn-A ParseHook() error = %v", err)
	}
	if turnA.SessionID != "chat-A" {
		t.Fatalf("turn-A session_id = %q, want %q", turnA.SessionID, "chat-A")
	}

	// User switches to tab B — Kiro updates B's transcript file.
	t.Setenv("USER_PROMPT", "in tab B")
	writeIDESessionFile(t, sessionsDir, "chat-B", `{"history":[]}`, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	turnB, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("turn-B ParseHook() error = %v", err)
	}
	if turnB.SessionID != "chat-B" {
		t.Fatalf("turn-B session_id = %q, want %q", turnB.SessionID, "chat-B")
	}

	// User goes back to the older tab A — Kiro updates A's transcript file
	// when the user types. The hook MUST resolve to A again, not stay
	// pinned to whichever tab was last seen.
	t.Setenv("USER_PROMPT", "back in tab A")
	writeIDESessionFile(t, sessionsDir, "chat-A", `{"history":[]}`, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	turnA2, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("turn-A2 ParseHook() error = %v", err)
	}
	if turnA2.SessionID != "chat-A" {
		t.Fatalf("returning to tab A: session_id = %q, want %q (stable per chat)", turnA2.SessionID, "chat-A")
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
