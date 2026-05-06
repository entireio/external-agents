package kiro

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParseHookTurnIdentityStableAcrossTabSwitchBetweenPromptAndStop(t *testing.T) {
	// Regression: previously stop re-resolved the IDE session via mtime,
	// so if the user switched tabs after prompt-submit fired, stop would
	// rebind the turn to a different chat and emit the wrong session_id.
	// Now prompt-submit caches the resolved IDE session in
	// kiro-active-turn and stop reads from that cache before falling back.
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
	// User starts in tab A — A's transcript is the most recently modified.
	writeIDESessionFile(t, sessionsDir, "chat-B", `{"history":[]}`, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	writeIDESessionFile(t, sessionsDir, "chat-A", `{"history":[]}`, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))

	prompt, err := New().ParseHook(HookNameUserPromptSubmit, nil)
	if err != nil {
		t.Fatalf("prompt-submit error = %v", err)
	}
	if prompt.SessionID != "chat-A" {
		t.Fatalf("prompt-submit session_id = %q, want %q", prompt.SessionID, "chat-A")
	}

	// User switches to tab B — bump B's mtime so it would now win mtime
	// resolution, then fire stop for the original turn.
	writeIDESessionFile(t, sessionsDir, "chat-B", `{"history":[]}`, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	stop, err := New().ParseHook(HookNameStop, nil)
	if err != nil {
		t.Fatalf("stop error = %v", err)
	}
	if stop.SessionID != "chat-A" {
		t.Fatalf("stop session_id = %q, want %q (turn must stay bound to prompt-submit's chat)", stop.SessionID, "chat-A")
	}

	// And the turn cache should be cleared after stop so the next
	// prompt-submit re-resolves freshly.
	turnCache := filepath.Join(repoRoot, ".entire", "tmp", "kiro-active-turn")
	if _, err := os.Stat(turnCache); !os.IsNotExist(err) {
		t.Fatalf("kiro-active-turn should be removed after stop, got err=%v", err)
	}
}

func TestParseHookToolCallsAreIsolatedPerChat(t *testing.T) {
	// Regression: tool calls used to be appended to a single repo-global
	// .entire/tmp/kiro-tool-calls.jsonl, so in a multi-chat workspace one
	// chat's stop would consume another chat's tool history. Per-chat
	// keying (kiro-tool-calls-<sanitized-id>.jsonl) keeps each chat's
	// tool calls isolated.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

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

	// Bind turn to chat-A via prompt-submit.
	writeIDESessionFile(t, sessionsDir, "chat-B", `{"history":[]}`, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	writeIDESessionFile(t, sessionsDir, "chat-A", `{"history":[]}`, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	t.Setenv("USER_PROMPT", "tab A prompt")
	if _, err := New().ParseHook(HookNameUserPromptSubmit, nil); err != nil {
		t.Fatalf("prompt-submit A error = %v", err)
	}
	// Post-tool-use under chat A.
	if _, err := New().ParseHook(HookNamePostToolUse, []byte(`{"tool_name":"fs_write","tool_input":{"path":"a.txt"}}`)); err != nil {
		t.Fatalf("post-tool-use A error = %v", err)
	}
	// Finish chat A's turn.
	if _, err := New().ParseHook(HookNameStop, nil); err != nil {
		t.Fatalf("stop A error = %v", err)
	}

	// Now bind a turn to chat-B and emit its own tool call.
	writeIDESessionFile(t, sessionsDir, "chat-B", `{"history":[]}`, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	t.Setenv("USER_PROMPT", "tab B prompt")
	if _, err := New().ParseHook(HookNameUserPromptSubmit, nil); err != nil {
		t.Fatalf("prompt-submit B error = %v", err)
	}
	if _, err := New().ParseHook(HookNamePostToolUse, []byte(`{"tool_name":"fs_write","tool_input":{"path":"b.txt"}}`)); err != nil {
		t.Fatalf("post-tool-use B error = %v", err)
	}

	// Each chat must have its own tool-calls file. Chat A's was consumed
	// by stop A; chat B's must contain only the b.txt call (NOT a.txt).
	tmpDir := filepath.Join(repoRoot, ".entire", "tmp")
	if _, err := os.Stat(filepath.Join(tmpDir, "kiro-tool-calls-chat-A.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("chat-A tool-calls file should be cleared by stop A, got err=%v", err)
	}
	bData, err := os.ReadFile(filepath.Join(tmpDir, "kiro-tool-calls-chat-B.jsonl"))
	if err != nil {
		t.Fatalf("read chat-B tool-calls: %v", err)
	}
	if !strings.Contains(string(bData), "b.txt") {
		t.Fatalf("chat-B tool-calls = %q, want b.txt", bData)
	}
	if strings.Contains(string(bData), "a.txt") {
		t.Fatalf("chat-B tool-calls leaked chat-A's content: %q", bData)
	}
	// And the legacy global file should never have been written for an
	// IDE-backed turn.
	if _, err := os.Stat(filepath.Join(tmpDir, "kiro-tool-calls.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("global kiro-tool-calls.jsonl should not exist for IDE turns, got err=%v", err)
	}
}

func TestResolveStopIdentityPrefersConversationIDOverActiveTurnCache(t *testing.T) {
	// Regression: resolveStopIdentity used to consult the repo-global
	// active-turn IDE cache before honoring a CLI-only payload that
	// supplied conversation_id. With both an in-flight IDE turn and a
	// concurrent CLI turn in the same repo, the CLI stop would inherit
	// the IDE turn binding and capture the wrong transcript. Explicit
	// CLI signal in the payload must short-circuit the IDE turn cache.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	// Plant unrelated IDE workspace data so ensureIDETranscript could
	// silently fall back if the resolver leaks the IDE turn ID.
	sessionsDir := createIDEWorkspaceSessionsDir(t, home, repoRoot)
	if err := os.WriteFile(
		filepath.Join(sessionsDir, "sessions.json"),
		[]byte(`[{"sessionId":"ide-turn-in-flight","title":"in-flight","dateCreated":"2026-02-01T00:00:00Z"}]`),
		0o600,
	); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	writeIDESessionFile(t, sessionsDir, "ide-turn-in-flight", `{"history":[
		{"message":{"role":"user","content":"in-flight IDE prompt"}},
		{"message":{"role":"assistant","content":"in-flight IDE response"}}
	]}`, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))

	// Simulate an in-flight IDE turn: a previous prompt-submit on the IDE
	// side wrote the active-turn cache.
	tmpDir := filepath.Join(repoRoot, ".entire", "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "kiro-active-turn"), []byte("ide-turn-in-flight"), 0o600); err != nil {
		t.Fatalf("seed active-turn cache: %v", err)
	}

	// CLI stop arrives with explicit conversation_id.
	stop, err := New().ParseHook(HookNameStop, []byte(`{"conversation_id":"cli-conv"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	if stop.SessionID != "cli-conv" {
		t.Fatalf("session_id = %q, want %q (CLI conv must beat in-flight IDE turn cache)", stop.SessionID, "cli-conv")
	}
	if !strings.HasSuffix(stop.SessionRef, "cli-conv.json") {
		t.Fatalf("session_ref = %q, want suffix cli-conv.json", stop.SessionRef)
	}
	data, err := os.ReadFile(stop.SessionRef)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}
	if strings.Contains(string(data), "in-flight IDE") {
		t.Fatalf("CLI stop captured in-flight IDE transcript content: %s", data)
	}
}

func TestPostToolUseWithConversationIDDoesNotLeakIntoIDEChat(t *testing.T) {
	// Regression: post-tool-use used to resolve its tool-call file purely
	// via activeTurnOrLatestIDESessionID, which falls back to the
	// most-recently-active IDE chat. CLI tool calls in a repo with any
	// IDE workspace data therefore got appended to an IDE-scoped jsonl;
	// CLI stop only clears the global file, so the IDE-keyed file
	// survived and would later contaminate an IDE transcript.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	sessionsDir := createIDEWorkspaceSessionsDir(t, home, repoRoot)
	if err := os.WriteFile(
		filepath.Join(sessionsDir, "sessions.json"),
		[]byte(`[{"sessionId":"ide-chat","title":"ide","dateCreated":"2026-02-01T00:00:00Z"}]`),
		0o600,
	); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	writeIDESessionFile(t, sessionsDir, "ide-chat", `{"history":[]}`, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))

	// CLI post-tool-use: explicit conversation_id, no IDE session id.
	if _, err := New().ParseHook(HookNamePostToolUse, []byte(`{"conversation_id":"cli-conv","tool_name":"fs_write","tool_input":{"path":"cli.txt"}}`)); err != nil {
		t.Fatalf("ParseHook(post-tool-use) error = %v", err)
	}

	tmpDir := filepath.Join(repoRoot, ".entire", "tmp")
	// The IDE chat's tool-calls file must not exist — the CLI tool call
	// belonged to a CLI conversation.
	if _, err := os.Stat(filepath.Join(tmpDir, "kiro-tool-calls-ide-chat.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("CLI tool call leaked into IDE chat's tool-calls file (err=%v)", err)
	}
	// And the global / CLI tool-calls file must contain the call.
	data, err := os.ReadFile(filepath.Join(tmpDir, "kiro-tool-calls.jsonl"))
	if err != nil {
		t.Fatalf("read global tool-calls file: %v", err)
	}
	if !strings.Contains(string(data), "cli.txt") {
		t.Fatalf("global tool-calls = %q, want cli.txt", data)
	}
}

func TestCLIStopPreservesInFlightIDETurnCache(t *testing.T) {
	// Regression: ParseHook(stop) used to clear kiro-active-turn
	// unconditionally. With concurrent CLI+IDE activity in the same repo,
	// a CLI stop would delete the in-flight IDE turn binding, causing the
	// IDE's later post-tool-use/stop to fall back to mtime resolution and
	// possibly finalize the wrong chat. CLI stops must leave the IDE turn
	// cache alone.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	tmpDir := filepath.Join(repoRoot, ".entire", "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "kiro-active-turn"), []byte("ide-in-flight"), 0o600); err != nil {
		t.Fatalf("seed active-turn: %v", err)
	}

	if _, err := New().ParseHook(HookNameStop, []byte(`{"conversation_id":"cli-conv"}`)); err != nil {
		t.Fatalf("CLI stop error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "kiro-active-turn"))
	if err != nil {
		t.Fatalf("active-turn was deleted by CLI stop: %v", err)
	}
	if string(data) != "ide-in-flight" {
		t.Fatalf("active-turn = %q, want %q (CLI stop must not touch IDE turn cache)", data, "ide-in-flight")
	}
}

func TestIDEStopFromTurnCacheDoesNotInjectArbitraryConversationID(t *testing.T) {
	// Regression: when resolveStopIdentity served an IDE turn from the
	// active-turn cache, it also queried SQLite for "the latest CLI
	// conversation in this repo" and stuffed that into conversationID.
	// If ensureIDETranscript later degraded for any reason,
	// captureTranscriptForStop's CLI fallback would then capture an
	// unrelated CLI transcript under the IDE session's file. An
	// IDE-bound stop must not pick up a random CLI conversation_id from
	// the database.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	// Plant an unrelated CLI conversation in the kiro DB AND a matching
	// transcript so the CLI fallback would succeed if it were ever
	// reached. We then force ensureIDETranscript to fail (no IDE
	// workspace data) and verify the captured ref is a placeholder, NOT
	// the unrelated CLI transcript content.
	dbPath := filepath.Join(home, "Library", "Application Support", "kiro-cli", "data.sqlite3")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	createSQLiteConversationDB(t, dbPath, repoRoot, `{"conversation_id":"unrelated-cli","history":[{"user":{"content":{"Prompt":{"prompt":"unrelated CLI prompt"}}},"assistant":null}]}`)

	tmpDir := filepath.Join(repoRoot, ".entire", "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "kiro-active-turn"), []byte("ide-only-chat"), 0o600); err != nil {
		t.Fatalf("seed active-turn: %v", err)
	}

	stop, err := New().ParseHook(HookNameStop, nil)
	if err != nil {
		t.Fatalf("stop error = %v", err)
	}
	if stop.SessionID != "ide-only-chat" {
		t.Fatalf("session_id = %q, want %q", stop.SessionID, "ide-only-chat")
	}
	data, err := os.ReadFile(stop.SessionRef)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if strings.Contains(string(data), "unrelated CLI prompt") {
		t.Fatalf("IDE stop captured unrelated CLI transcript via SQLite fallback: %s", data)
	}
}

func TestParseHookCLIConversationIDDoesNotMasqueradeAsIDESession(t *testing.T) {
	// Regression: rawSessionID used to fall through to conversation_id, so
	// a CLI hook with only conversation_id set populated ideSessionID with
	// the CLI value. The stop path then tried ensureIDETranscript first
	// and — when no <conversation_id>.json existed — fell back to "latest
	// IDE session", checkpointing a totally unrelated IDE chat.
	//
	// This test puts unrelated IDE workspace data on disk AND fires a
	// CLI-only stop with conversation_id. The captured transcript must
	// NOT come from the IDE chat; it must come from the CLI SQLite path
	// (or the placeholder), with session_id == conversation_id.
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	// Plant unrelated IDE workspace data in the same repo.
	sessionsDir := createIDEWorkspaceSessionsDir(t, home, repoRoot)
	if err := os.WriteFile(
		filepath.Join(sessionsDir, "sessions.json"),
		[]byte(`[{"sessionId":"ide-chat","title":"unrelated","dateCreated":"2026-02-01T00:00:00Z"}]`),
		0o600,
	); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	writeIDESessionFile(t, sessionsDir, "ide-chat", `{"history":[
		{"message":{"role":"user","content":"unrelated IDE prompt"}},
		{"message":{"role":"assistant","content":"unrelated IDE response"}}
	]}`, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))

	// Fire a CLI-only stop hook: only conversation_id, no IDE session ID.
	stop, err := New().ParseHook(HookNameStop, []byte(`{"conversation_id":"cli-conv"}`))
	if err != nil {
		t.Fatalf("ParseHook(stop) error = %v", err)
	}
	if stop.SessionID != "cli-conv" {
		t.Fatalf("session_id = %q, want %q (CLI conversation_id)", stop.SessionID, "cli-conv")
	}

	// The cached transcript file must be addressed by the CLI conversation
	// id (cli-conv.json), not the unrelated IDE chat (ide-chat.json).
	if !strings.HasSuffix(stop.SessionRef, "cli-conv.json") {
		t.Fatalf("session_ref = %q, want suffix %q", stop.SessionRef, "cli-conv.json")
	}

	// And the captured content must NOT contain the unrelated IDE chat's
	// transcript content (which would prove ensureIDETranscript leaked).
	data, err := os.ReadFile(stop.SessionRef)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}
	if strings.Contains(string(data), "unrelated IDE prompt") || strings.Contains(string(data), "unrelated IDE response") {
		t.Fatalf("CLI stop captured unrelated IDE transcript content: %s", data)
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
