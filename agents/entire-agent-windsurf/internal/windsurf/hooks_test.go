package windsurf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withRuntimeGOOS(t *testing.T, goos string) {
	t.Helper()
	prev := runtimeGOOS
	runtimeGOOS = goos
	t.Cleanup(func() { runtimeGOOS = prev })
}

func TestParseHookPreUserPromptEmitsTurnStart(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	payload := `{
		"trajectory_id": "traj-abc",
		"timestamp":     "2026-01-01T00:00:00Z",
		"tool_info":     {"user_prompt": "write tests"}
	}`

	event, err := New().ParseHook(HookNamePreUserPrompt, []byte(payload))
	if err != nil {
		t.Fatalf("ParseHook(%s) error = %v", HookNamePreUserPrompt, err)
	}
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != 2 {
		t.Fatalf("event.Type = %d, want 2", event.Type)
	}
	if event.SessionID != "traj-abc" {
		t.Fatalf("session_id = %q, want %q", event.SessionID, "traj-abc")
	}
	if event.Prompt != "write tests" {
		t.Fatalf("prompt = %q, want %q", event.Prompt, "write tests")
	}
}

func TestParseHookPreUserPromptCachesSessionID(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	_, err := New().ParseHook(HookNamePreUserPrompt, []byte(`{"trajectory_id":"traj-xyz"}`))
	if err != nil {
		t.Fatalf("ParseHook error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".entire", "tmp", sessionIDFile))
	if err != nil {
		t.Fatalf("read cached session id: %v", err)
	}
	if string(data) != "traj-xyz" {
		t.Fatalf("cached session id = %q, want %q", string(data), "traj-xyz")
	}
}

func TestParseHookPreUserPromptUsesEnvFallbackForPrompt(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("USER_PROMPT", "env prompt")

	event, err := New().ParseHook(HookNamePreUserPrompt, []byte(`{"trajectory_id":"traj-1"}`))
	if err != nil {
		t.Fatalf("ParseHook error = %v", err)
	}
	if event.Prompt != "env prompt" {
		t.Fatalf("prompt = %q, want %q", event.Prompt, "env prompt")
	}
}

func TestParseHookPreUserPromptFallsBackToCachedSessionID(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	// Cache a session ID first.
	tmpDir := filepath.Join(repoRoot, ".entire", "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, sessionIDFile), []byte("cached-session"), 0o600); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// Hook payload without trajectory_id.
	event, err := New().ParseHook(HookNamePreUserPrompt, []byte(`{"tool_info":{"user_prompt":"hi"}}`))
	if err != nil {
		t.Fatalf("ParseHook error = %v", err)
	}
	if event.SessionID != "cached-session" {
		t.Fatalf("session_id = %q, want cached %q", event.SessionID, "cached-session")
	}
}

func TestParseHookPostWriteCodeReturnsNilAndRecordsFile(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	// Seed a session so the file record targets an existing session.
	New().cacheSessionID("traj-abc")

	payload := `{
		"trajectory_id": "traj-abc",
		"tool_info":     {"file_path": "src/main.go"}
	}`
	event, err := New().ParseHook(HookNamePostWriteCode, []byte(payload))
	if err != nil {
		t.Fatalf("ParseHook(%s) error = %v", HookNamePostWriteCode, err)
	}
	if event != nil {
		t.Fatalf("expected nil event, got %+v", event)
	}

	// Transcript should contain the file record.
	sessionRef := New().resolveSessionRef("traj-abc")
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		t.Fatalf("read session ref: %v", err)
	}
	if !strings.Contains(string(data), `"src/main.go"`) {
		t.Fatalf("transcript missing file path; got %q", string(data))
	}
}

func TestParseHookPostCascadeResponseEmitsTurnEnd(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	payload := `{
		"trajectory_id": "traj-abc",
		"timestamp":     "2026-01-01T00:01:00Z",
		"tool_info":     {"response": "Done! I wrote the file."}
	}`

	event, err := New().ParseHook(HookNamePostCascadeResponse, []byte(payload))
	if err != nil {
		t.Fatalf("ParseHook(%s) error = %v", HookNamePostCascadeResponse, err)
	}
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != 3 {
		t.Fatalf("event.Type = %d, want 3", event.Type)
	}
	if event.SessionID != "traj-abc" {
		t.Fatalf("session_id = %q, want %q", event.SessionID, "traj-abc")
	}
	if event.SessionRef == "" {
		t.Fatal("session_ref should not be empty")
	}
	expectedRef := New().resolveSessionRef("traj-abc")
	if event.SessionRef != expectedRef {
		t.Fatalf("session_ref = %q, want %q", event.SessionRef, expectedRef)
	}
}

func TestParseHookPostCascadeResponseWritesResponseToTranscript(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	payload := `{"trajectory_id":"traj-r","tool_info":{"response":"All done."}}`
	if _, err := New().ParseHook(HookNamePostCascadeResponse, []byte(payload)); err != nil {
		t.Fatalf("ParseHook error = %v", err)
	}

	sessionRef := New().resolveSessionRef("traj-r")
	records, err := readTranscriptRecords(sessionRef)
	if err != nil {
		t.Fatalf("readTranscriptRecords: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one record")
	}
	last := records[len(records)-1]
	if last.Type != transcriptTypeResponse {
		t.Fatalf("last record type = %q, want %q", last.Type, transcriptTypeResponse)
	}
	if last.Content != "All done." {
		t.Fatalf("last record content = %q, want %q", last.Content, "All done.")
	}
}

func TestParseHookUnknownHookReturnsNil(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())

	event, err := New().ParseHook("unknown_hook", []byte(`{}`))
	if err != nil {
		t.Fatalf("ParseHook(unknown) error = %v", err)
	}
	if event != nil {
		t.Fatalf("expected nil event for unknown hook, got %+v", event)
	}
}

func TestInstallHooksWritesHooksConfig(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	count, err := New().InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}
	if count != 3 {
		t.Fatalf("InstallHooks() count = %d, want 3", count)
	}

	hooksPath := filepath.Join(repoRoot, ".windsurf", "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}

	var config windsurfHooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse hooks.json: %v", err)
	}

	wantBase := "entire hooks windsurf "
	for _, hook := range []struct {
		name    string
		entries []windsurfHookEntry
	}{
		{HookNamePreUserPrompt, config.Hooks.PreUserPrompt},
		{HookNamePostWriteCode, config.Hooks.PostWriteCode},
		{HookNamePostCascadeResponse, config.Hooks.PostCascadeResponse},
	} {
		if len(hook.entries) == 0 {
			t.Fatalf("no entries for hook %s", hook.name)
		}
		want := wantBase + hook.name
		if !hookCommandPresent(hook.entries, want) {
			t.Fatalf("hook %s missing command %q", hook.name, want)
		}
	}
}

func TestInstallHooksIsIdempotentUnlessForced(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatalf("first InstallHooks() error = %v", err)
	}

	count, err := New().InstallHooks(false, false)
	if err != nil {
		t.Fatalf("second InstallHooks() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("second InstallHooks() count = %d, want 0 (idempotent)", count)
	}

	count, err = New().InstallHooks(false, true)
	if err != nil {
		t.Fatalf("forced InstallHooks() error = %v", err)
	}
	if count != 3 {
		t.Fatalf("forced InstallHooks() count = %d, want 3", count)
	}
}

func TestInstallHooksLocalDevUsesLocalCommands(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	if _, err := New().InstallHooks(true, false); err != nil {
		t.Fatalf("InstallHooks(localDev) error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".windsurf", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	if !strings.Contains(string(data), "WINDSURF_PROJECT_DIR") {
		t.Fatalf("local dev hooks should reference WINDSURF_PROJECT_DIR; got %q", string(data))
	}
}

func TestInstallHooksWindowsUsesPowerShell(t *testing.T) {
	withRuntimeGOOS(t, "windows")
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".windsurf", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var config windsurfHooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse hooks.json: %v", err)
	}
	// Windows entries should use PowerShell, not command.
	for _, entry := range config.Hooks.PreUserPrompt {
		if entry.PowerShell == "" {
			t.Fatal("Windows hooks should use powershell field")
		}
		if entry.Command != "" {
			t.Fatal("Windows hooks should not set command field")
		}
	}
}

func TestInstallHooksPreservesExistingUserHooks(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	// Write a pre-existing hooks.json with a user-defined hook.
	hooksPath := filepath.Join(repoRoot, ".windsurf", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := `{"hooks":{"pre_user_prompt":[{"command":"my-other-tool pre_user_prompt"}]}}`
	if err := os.WriteFile(hooksPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed hooks.json: %v", err)
	}

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var config windsurfHooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse hooks.json: %v", err)
	}
	// User-defined hook must still be present.
	if !hookCommandPresent(config.Hooks.PreUserPrompt, "my-other-tool pre_user_prompt") {
		t.Fatal("existing user hook was lost after install")
	}
	// Entire hook must also be present.
	if !hookCommandPresent(config.Hooks.PreUserPrompt, "entire hooks windsurf "+HookNamePreUserPrompt) {
		t.Fatal("Entire hook was not added")
	}
}

func TestUninstallHooksPreservesUnrelatedHooks(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	hooksPath := filepath.Join(repoRoot, ".windsurf", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Seed a file with both a user hook and an Entire hook.
	seed := `{"hooks":{"pre_user_prompt":[{"command":"my-other-tool pre_user_prompt"},{"command":"entire hooks windsurf pre_user_prompt"}]}}`
	if err := os.WriteFile(hooksPath, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed hooks.json: %v", err)
	}

	if err := New().UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks() error = %v", err)
	}

	// File must still exist because a non-Entire hook remains.
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("hooks.json should still exist after partial uninstall: %v", err)
	}
	var config windsurfHooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse remaining hooks.json: %v", err)
	}
	if hookCommandPresent(config.Hooks.PreUserPrompt, "entire hooks windsurf "+HookNamePreUserPrompt) {
		t.Fatal("Entire hook should have been removed")
	}
	if !hookCommandPresent(config.Hooks.PreUserPrompt, "my-other-tool pre_user_prompt") {
		t.Fatal("user hook was incorrectly removed")
	}
}

func TestUninstallHooksRemovesFileWhenEntireOnly(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	// Install only Entire hooks, then uninstall — file should disappear.
	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}
	if err := New().UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks() error = %v", err)
	}

	hooksPath := filepath.Join(repoRoot, ".windsurf", "hooks.json")
	if _, err := os.Stat(hooksPath); !os.IsNotExist(err) {
		t.Fatalf("hooks.json should not exist after uninstall of Entire-only config: %v", err)
	}
}

func TestUninstallHooksRemovesHooksConfig(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	if err := New().UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks() error = %v", err)
	}

	hooksPath := filepath.Join(repoRoot, ".windsurf", "hooks.json")
	if _, err := os.Stat(hooksPath); !os.IsNotExist(err) {
		t.Fatalf("hooks.json should not exist after uninstall: %v", err)
	}
}

func TestUninstallHooksIsIdempotent(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	if err := New().UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks() on missing file error = %v", err)
	}
}

func TestAreHooksInstalled(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	if New().AreHooksInstalled() {
		t.Fatal("should not be installed before InstallHooks")
	}

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	if !New().AreHooksInstalled() {
		t.Fatal("should be installed after InstallHooks")
	}

	if err := New().UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks() error = %v", err)
	}

	if New().AreHooksInstalled() {
		t.Fatal("should not be installed after UninstallHooks")
	}
}

func TestFullTurnLifecycleWritesTranscript(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	agent := New()

	// Turn start.
	promptPayload := `{"trajectory_id":"traj-1","tool_info":{"user_prompt":"create main.go"}}`
	ev, err := agent.ParseHook(HookNamePreUserPrompt, []byte(promptPayload))
	if err != nil || ev == nil || ev.Type != 2 {
		t.Fatalf("pre_user_prompt event = %v, err = %v", ev, err)
	}

	// File modification.
	filePayload := `{"trajectory_id":"traj-1","tool_info":{"file_path":"main.go"}}`
	ev, err = agent.ParseHook(HookNamePostWriteCode, []byte(filePayload))
	if err != nil || ev != nil {
		t.Fatalf("post_write_code should return nil, got %v, err = %v", ev, err)
	}

	// Turn end.
	responsePayload := `{"trajectory_id":"traj-1","tool_info":{"response":"Created main.go."}}`
	ev, err = agent.ParseHook(HookNamePostCascadeResponse, []byte(responsePayload))
	if err != nil || ev == nil || ev.Type != 3 {
		t.Fatalf("post_cascade_response event = %v, err = %v", ev, err)
	}

	records, err := readTranscriptRecords(ev.SessionRef)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records (prompt, file, response), got %d", len(records))
	}
	if records[0].Type != transcriptTypePrompt || records[0].Content != "create main.go" {
		t.Fatalf("records[0] = %+v, want prompt 'create main.go'", records[0])
	}
	if records[1].Type != transcriptTypeFile || records[1].Path != "main.go" {
		t.Fatalf("records[1] = %+v, want file 'main.go'", records[1])
	}
	if records[2].Type != transcriptTypeResponse || records[2].Content != "Created main.go." {
		t.Fatalf("records[2] = %+v, want response 'Created main.go.'", records[2])
	}
}
