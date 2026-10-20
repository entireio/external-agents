package devin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// liveSessionID is the word-pair session ID captured from a live devin run.
const liveSessionID = "snowy-efraasia"

// Payloads below are verbatim captures from devin 3000.2.17 (see AGENT.md).

func TestParseHook_SessionStart(t *testing.T) {
	transcriptDir := t.TempDir()
	t.Setenv("ENTIRE_TEST_DEVIN_TRANSCRIPT_DIR", transcriptDir)

	a := New()
	payload := []byte(`{"hook_event_name":"SessionStart","source":"startup","session_id":"snowy-efraasia"}`)
	event, err := a.ParseHook(HookNameSessionStart, payload)
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	if event.Type != eventSessionStart {
		t.Errorf("Type = %d, want %d", event.Type, eventSessionStart)
	}
	if event.SessionID != liveSessionID {
		t.Errorf("SessionID = %q, want %q", event.SessionID, liveSessionID)
	}
	want := filepath.Join(transcriptDir, liveSessionID+".json")
	if event.SessionRef != want {
		t.Errorf("SessionRef = %q, want %q", event.SessionRef, want)
	}
}

func TestParseHook_UserPromptSubmit(t *testing.T) {
	t.Setenv("ENTIRE_TEST_DEVIN_TRANSCRIPT_DIR", t.TempDir())

	a := New()
	payload := []byte(`{"hook_event_name":"UserPromptSubmit","prompt":"Create a file named hello.txt","session_id":"snowy-efraasia","prompt_id":"54697fcd-fbea-4b60-b718-f3abcf9375fc"}`)
	event, err := a.ParseHook(HookNameUserPromptSubmit, payload)
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	if event.Type != eventTurnStart {
		t.Errorf("Type = %d, want %d", event.Type, eventTurnStart)
	}
	if event.Prompt != "Create a file named hello.txt" {
		t.Errorf("Prompt = %q", event.Prompt)
	}
	if event.SessionRef == "" {
		t.Error("SessionRef is empty, want derived transcript path")
	}
}

func TestParseHook_StopAndSessionEnd(t *testing.T) {
	t.Setenv("ENTIRE_TEST_DEVIN_TRANSCRIPT_DIR", t.TempDir())
	a := New()

	stop := []byte(`{"hook_event_name":"Stop","stop_hook_active":false,"session_id":"snowy-efraasia","prompt_id":"54697fcd-fbea-4b60-b718-f3abcf9375fc"}`)
	event, err := a.ParseHook(HookNameStop, stop)
	if err != nil {
		t.Fatalf("ParseHook stop: %v", err)
	}
	if event.Type != eventTurnEnd {
		t.Errorf("stop Type = %d, want %d", event.Type, eventTurnEnd)
	}

	end := []byte(`{"hook_event_name":"SessionEnd","reason":"session_complete","session_id":"snowy-efraasia","prompt_id":"54697fcd-fbea-4b60-b718-f3abcf9375fc"}`)
	event, err = a.ParseHook(HookNameSessionEnd, end)
	if err != nil {
		t.Fatalf("ParseHook session-end: %v", err)
	}
	if event.Type != eventSessionEnd {
		t.Errorf("session-end Type = %d, want %d", event.Type, eventSessionEnd)
	}
}

func TestParseHook_Edges(t *testing.T) {
	t.Parallel()
	a := New()

	if event, err := a.ParseHook("unknown-hook", []byte(`{}`)); err != nil || event != nil {
		t.Errorf("unknown hook: event=%v err=%v, want nil,nil", event, err)
	}
	if event, err := a.ParseHook(HookNameStop, []byte(`{"hook_event_name":"Stop"}`)); err != nil || event != nil {
		t.Errorf("missing session_id: event=%v err=%v, want nil,nil", event, err)
	}
	if _, err := a.ParseHook(HookNameStop, []byte(`{not json`)); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func setupHooksTestRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmpDir)
	return tmpDir
}

func readHooksFile(t *testing.T, repoDir string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, ".devin", HooksFileName))
	if err != nil {
		t.Fatalf("read hooks file: %v", err)
	}
	rawHooks := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &rawHooks); err != nil {
		t.Fatalf("parse hooks file: %v", err)
	}
	return rawHooks
}

func TestInstallHooks_FreshInstall(t *testing.T) {
	repoDir := setupHooksTestRepo(t)
	a := New()

	count, err := a.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}

	rawHooks := readHooksFile(t, repoDir)
	for _, event := range []string{"SessionStart", "SessionEnd", "Stop", "UserPromptSubmit"} {
		if _, ok := rawHooks[event]; !ok {
			t.Errorf("hooks file missing event %q", event)
		}
	}

	var stop []HookMatcher
	if err := json.Unmarshal(rawHooks["Stop"], &stop); err != nil {
		t.Fatalf("parse Stop: %v", err)
	}
	if len(stop) != 1 || len(stop[0].Hooks) != 1 {
		t.Fatalf("Stop matchers = %+v", stop)
	}
	if !strings.Contains(stop[0].Hooks[0].Command, "hooks devin stop") {
		t.Errorf("Stop command = %q, want it to invoke 'hooks devin stop'", stop[0].Hooks[0].Command)
	}

	if !a.AreHooksInstalled() {
		t.Error("AreHooksInstalled = false after install")
	}
}

func TestInstallHooks_Idempotent(t *testing.T) {
	setupHooksTestRepo(t)
	a := New()

	if _, err := a.InstallHooks(false, false); err != nil {
		t.Fatalf("first InstallHooks: %v", err)
	}
	count, err := a.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("second InstallHooks: %v", err)
	}
	if count != 0 {
		t.Errorf("second install count = %d, want 0", count)
	}
}

func TestInstallHooks_PreservesForeignHooks(t *testing.T) {
	repoDir := setupHooksTestRepo(t)
	a := New()

	existing := `{
  "Stop": [{"matcher": "", "hooks": [{"type": "command", "command": "./my-hook.sh"}]}],
  "PreToolUse": [{"matcher": "exec", "hooks": [{"type": "command", "command": "./validate.sh"}]}]
}`
	if err := os.MkdirAll(filepath.Join(repoDir, ".devin"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".devin", HooksFileName), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := a.InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	rawHooks := readHooksFile(t, repoDir)

	var preToolUse []HookMatcher
	if err := json.Unmarshal(rawHooks["PreToolUse"], &preToolUse); err != nil {
		t.Fatalf("parse PreToolUse: %v", err)
	}
	if len(preToolUse) != 1 || preToolUse[0].Hooks[0].Command != "./validate.sh" {
		t.Errorf("foreign PreToolUse hook not preserved: %+v", preToolUse)
	}

	var stop []HookMatcher
	if err := json.Unmarshal(rawHooks["Stop"], &stop); err != nil {
		t.Fatalf("parse Stop: %v", err)
	}
	foundForeign, foundEntire := false, false
	for _, m := range stop {
		for _, h := range m.Hooks {
			if h.Command == "./my-hook.sh" {
				foundForeign = true
			}
			if isEntireHook(h.Command) {
				foundEntire = true
			}
		}
	}
	if !foundForeign || !foundEntire {
		t.Errorf("Stop hooks foreign=%v entire=%v, want both true: %+v", foundForeign, foundEntire, stop)
	}
}

func TestUninstallHooks_RemovesOnlyEntireHooks(t *testing.T) {
	repoDir := setupHooksTestRepo(t)
	a := New()

	existing := `{"Stop": [{"matcher": "", "hooks": [{"type": "command", "command": "./my-hook.sh"}]}]}`
	if err := os.MkdirAll(filepath.Join(repoDir, ".devin"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".devin", HooksFileName), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := a.InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	if err := a.UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks: %v", err)
	}

	if a.AreHooksInstalled() {
		t.Error("AreHooksInstalled = true after uninstall")
	}

	rawHooks := readHooksFile(t, repoDir)
	var stop []HookMatcher
	if err := json.Unmarshal(rawHooks["Stop"], &stop); err != nil {
		t.Fatalf("parse Stop: %v", err)
	}
	if len(stop) != 1 || stop[0].Hooks[0].Command != "./my-hook.sh" {
		t.Errorf("foreign Stop hook not preserved after uninstall: %+v", stop)
	}
	if _, ok := rawHooks["SessionStart"]; ok {
		t.Error("SessionStart still present after uninstall (should be removed when empty)")
	}
}

func TestInstallHooks_Force(t *testing.T) {
	repoDir := setupHooksTestRepo(t)
	a := New()

	if _, err := a.InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	count, err := a.InstallHooks(false, true)
	if err != nil {
		t.Fatalf("force InstallHooks: %v", err)
	}
	if count != 4 {
		t.Errorf("force install count = %d, want 4", count)
	}

	rawHooks := readHooksFile(t, repoDir)
	var stop []HookMatcher
	if err := json.Unmarshal(rawHooks["Stop"], &stop); err != nil {
		t.Fatalf("parse Stop: %v", err)
	}
	total := 0
	for _, m := range stop {
		total += len(m.Hooks)
	}
	if total != 1 {
		t.Errorf("Stop hook count after force = %d, want 1 (no duplicates)", total)
	}
}

func TestAreHooksInstalled_NoFile(t *testing.T) {
	setupHooksTestRepo(t)
	if New().AreHooksInstalled() {
		t.Error("AreHooksInstalled = true with no hooks file")
	}
}
