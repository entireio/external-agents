package gemini

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHookSessionStart(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"test-sess-001","hook_event_name":"SessionStart","timestamp":"2026-08-01T12:00:00Z","model":"gemini-2.5-pro"}`
	event, err := a.ParseHook(HookNameSessionStart, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil for session-start")
	}
	if event.Type != 1 {
		t.Errorf("Type = %d, want 1", event.Type)
	}
	if event.SessionID != "test-sess-001" {
		t.Errorf("SessionID = %q, want test-sess-001", event.SessionID)
	}
	if event.Model != "gemini-2.5-pro" {
		t.Errorf("Model = %q, want gemini-2.5-pro", event.Model)
	}
}

func TestParseHookBeforeAgent(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"test-sess-001","hook_event_name":"BeforeAgent","timestamp":"2026-08-01T12:01:00Z","prompt":"Create hello.txt","model":"gemini-2.5-pro"}`
	event, err := a.ParseHook(HookNameBeforeAgent, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil for before-agent with prompt")
	}
	if event.Type != 2 {
		t.Errorf("Type = %d, want 2", event.Type)
	}
	if event.Prompt != "Create hello.txt" {
		t.Errorf("Prompt = %q, want Create hello.txt", event.Prompt)
	}
}

func TestParseHookBeforeAgentNoPrompt(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"test-sess-001","timestamp":"2026-08-01T12:01:00Z"}`
	event, err := a.ParseHook(HookNameBeforeAgent, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event != nil {
		t.Error("event should be nil when no prompt")
	}
}

func TestParseHookAfterAgent(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"test-sess-001","hook_event_name":"AfterAgent","timestamp":"2026-08-01T12:02:00Z","last_assistant_message":"Done!","model":"gemini-2.5-pro"}`
	event, err := a.ParseHook(HookNameAfterAgent, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil for after-agent")
	}
	if event.Type != 3 {
		t.Errorf("Type = %d, want 3", event.Type)
	}
	if event.ResponseMessage != "Done!" {
		t.Errorf("ResponseMessage = %q, want Done!", event.ResponseMessage)
	}
}

func TestParseHookSessionEnd(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"test-sess-001","hook_event_name":"SessionEnd","timestamp":"2026-08-01T12:03:00Z"}`
	event, err := a.ParseHook(HookNameSessionEnd, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil for session-end")
	}
	if event.Type != 5 {
		t.Errorf("Type = %d, want 5", event.Type)
	}
}

func TestParseHookPreCompress(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"test-sess-001","hook_event_name":"PreCompress","timestamp":"2026-08-01T12:04:00Z"}`
	event, err := a.ParseHook(HookNamePreCompress, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil for pre-compress")
	}
	if event.Type != 4 {
		t.Errorf("Type = %d, want 4", event.Type)
	}
}

func TestParseHookBeforeToolReturnsNil(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"test-sess-001","hook_event_name":"BeforeTool","timestamp":"2026-08-01T12:01:30Z","tool_name":"write_file"}`
	event, err := a.ParseHook(HookNameBeforeTool, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event != nil {
		t.Error("BeforeTool should return nil event (recorded in sidecar only)")
	}
}

func TestParseHookAfterToolReturnsNil(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"test-sess-001","hook_event_name":"AfterTool","timestamp":"2026-08-01T12:01:35Z","tool_name":"write_file"}`
	event, err := a.ParseHook(HookNameAfterTool, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event != nil {
		t.Error("AfterTool should return nil event (recorded in sidecar only)")
	}
}

func TestParseHookEmptyInput(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)
	t.Setenv("GEMINI_SESSION_ID", "env-session-999")

	event, err := a.ParseHook(HookNameSessionStart, nil)
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil {
		t.Fatal("event should not be nil for session-start with empty input")
	}
	if event.SessionID != "env-session-999" {
		t.Errorf("SessionID = %q, want env-session-999", event.SessionID)
	}
}

func TestSidecarWritten(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	input := `{"session_id":"sidecar-test-001","hook_event_name":"SessionStart","timestamp":"2026-08-01T12:00:00Z","model":"gemini-2.5-pro"}`
	_, err := a.ParseHook(HookNameSessionStart, []byte(input))
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}

	// Check sidecar file exists
	dir, _ := a.GetSessionDir(tmp)
	sidecarPath := a.ResolveSessionFile(dir, "sidecar-test-001")
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sidecar file not written: %v", err)
	}

	var rec sidecarRecord
	if err := json.Unmarshal(data[:len(data)-1], &rec); err != nil {
		t.Fatalf("invalid sidecar JSON: %v", err)
	}
	if rec.Agent != AgentName {
		t.Errorf("sidecar agent = %q, want %q", rec.Agent, AgentName)
	}
	if rec.Event != "SessionStart" {
		t.Errorf("sidecar event = %q, want SessionStart", rec.Event)
	}
}

func TestInstallAndUninstallHooks(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	// Install
	count, err := a.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks error: %v", err)
	}
	if count != 8 {
		t.Errorf("InstallHooks count = %d, want 8", count)
	}

	// Verify installed
	if !a.AreHooksInstalled() {
		t.Error("AreHooksInstalled should return true after install")
	}

	// Read settings.json and verify
	settingsPath := filepath.Join(tmp, ".gemini", settingsFileName)
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}

	hooksRaw, ok := settings["hooks"]
	if !ok {
		t.Fatal("no hooks key in settings.json")
	}

	hooksMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(hooksRaw, &hooksMap); err != nil {
		t.Fatalf("parse hooks: %v", err)
	}

	for _, spec := range hookSpecs {
		if _, ok := hooksMap[spec.GeminiEvent]; !ok {
			t.Errorf("hook event %q missing from settings.json", spec.GeminiEvent)
		}
	}

	// Uninstall
	if err := a.UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks error: %v", err)
	}

	// Verify uninstalled
	if a.AreHooksInstalled() {
		t.Error("AreHooksInstalled should return false after uninstall")
	}
}

func TestInstallHooksIdempotent(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	// First install
	count1, _ := a.InstallHooks(false, false)
	if count1 != 8 {
		t.Fatalf("first install count = %d, want 8", count1)
	}

	// Second install should not add duplicates
	count2, _ := a.InstallHooks(false, false)
	if count2 != 0 {
		t.Errorf("second install count = %d, want 0 (already installed)", count2)
	}
}

func TestInstallHooksPreservesExisting(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	// Write existing user hook
	settingsDir := filepath.Join(tmp, ".gemini")
	os.MkdirAll(settingsDir, 0o755)
	existingSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"SessionStart": []map[string]interface{}{
				{
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": "echo user-hook",
							"name":    "user-hook",
						},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existingSettings, "", "  ")
	os.WriteFile(filepath.Join(settingsDir, settingsFileName), data, 0o644)

	// Install Entire hooks
	count, err := a.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks error: %v", err)
	}
	if count == 0 {
		t.Error("InstallHooks should add hooks even with existing user hooks")
	}

	// Read back and verify user hook is preserved
	settingsData, _ := os.ReadFile(filepath.Join(settingsDir, settingsFileName))
	var settings map[string]json.RawMessage
	json.Unmarshal(settingsData, &settings)

	hooksMap := map[string][]geminiHookMatcher{}
	hooksBytes, _ := json.Marshal(settings["hooks"])
	json.Unmarshal(hooksBytes, &hooksMap)

	sessionStartHooks := hooksMap["SessionStart"]
	foundUser := false
	foundEntire := false
	for _, m := range sessionStartHooks {
		for _, h := range m.Hooks {
			if h.Name == "user-hook" {
				foundUser = true
			}
			if h.Name == "entire-session-start" {
				foundEntire = true
			}
		}
	}
	if !foundUser {
		t.Error("user hook should be preserved after Entire install")
	}
	if !foundEntire {
		t.Error("entire hook should be added after install")
	}
}

func TestHookCommand(t *testing.T) {
	cmd := hookCommand("session-start")
	if cmd == "" {
		t.Error("hookCommand returned empty string")
	}
	// Should contain "entire hooks gemini session-start"
	if !contains(cmd, "entire hooks gemini session-start") {
		t.Errorf("hookCommand = %q, should contain 'entire hooks gemini session-start'", cmd)
	}
	// Should have sh -c wrapper
	if !contains(cmd, "sh -c") {
		t.Errorf("hookCommand = %q, should contain 'sh -c'", cmd)
	}
}

func TestIsEntireHook(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"entire-session-start", "sh -c 'entire hooks gemini session-start'", true},
		{"user-hook", "echo hello", false},
		{"entire-custom", "entire hooks gemini custom-event", true},
		{"my-hook", "my-script.sh", false},
	}
	for _, tc := range tests {
		hook := geminiHookEntry{Name: tc.name, Command: tc.cmd}
		got := isEntireHook(hook)
		if got != tc.want {
			t.Errorf("isEntireHook(name=%q, cmd=%q) = %v, want %v", tc.name, tc.cmd, got, tc.want)
		}
	}
}

func TestGeminiEventName(t *testing.T) {
	// Should return existing event name if present
	got := geminiEventName("session-start", "ExistingEvent")
	if got != "ExistingEvent" {
		t.Errorf("geminiEventName with existing = %q, want ExistingEvent", got)
	}

	// Should map hook name to Gemini event
	got = geminiEventName(HookNameSessionStart, "")
	if got != "SessionStart" {
		t.Errorf("geminiEventName(session-start) = %q, want SessionStart", got)
	}

	got = geminiEventName(HookNameBeforeAgent, "")
	if got != "BeforeAgent" {
		t.Errorf("geminiEventName(before-agent) = %q, want BeforeAgent", got)
	}

	// Unknown hook name should return the hook name itself
	got = geminiEventName("unknown-hook", "")
	if got != "unknown-hook" {
		t.Errorf("geminiEventName(unknown-hook) = %q, want unknown-hook", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
