package kilo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentInfo(t *testing.T) {
	a := New()
	info := a.Info()
	if info.Name != "kilo" {
		t.Fatalf("Name = %q, want %q", info.Name, "kilo")
	}
	if !info.Capabilities.Hooks {
		t.Fatal("Hooks capability should be true")
	}
	if !info.Capabilities.TranscriptAnalyzer {
		t.Fatal("TranscriptAnalyzer capability should be true")
	}
	if !info.Capabilities.TokenCalculator {
		t.Fatal("TokenCalculator capability should be true")
	}
	if !info.Capabilities.CompactTranscript {
		t.Fatal("CompactTranscript capability should be true")
	}
	wantHooks := map[string]bool{
		HookNameSessionStart: false,
		HookNameTurnStart:    false,
		HookNameTurnEnd:      false,
		HookNameCompaction:   false,
		HookNameSessionEnd:   false,
	}
	for _, h := range info.HookNames {
		if _, ok := wantHooks[h]; !ok {
			t.Fatalf("unexpected hook %q in HookNames", h)
		}
		wantHooks[h] = true
	}
	for hook, found := range wantHooks {
		if !found {
			t.Fatalf("hook %q missing from declared HookNames", hook)
		}
	}
}

func TestFormatResumeCommand(t *testing.T) {
	a := New()
	if got := a.FormatResumeCommand(""); got != "kilo run --continue" {
		t.Fatalf("FormatResumeCommand(empty) = %q", got)
	}
	if got := a.FormatResumeCommand("S-abc123"); got != "kilo run --session S-abc123" {
		t.Fatalf("FormatResumeCommand(S-abc123) = %q", got)
	}
}

func TestParseHookSessionStart(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	body, _ := json.Marshal(sessionInfoRaw{SessionID: "S-1"})

	a := New()
	event, err := a.ParseHook(HookNameSessionStart, body)
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil {
		t.Fatal("event nil; want SessionStart")
	}
	if event.Type != 1 {
		t.Fatalf("event type = %d, want 1", event.Type)
	}
	if event.SessionID != "S-1" {
		t.Fatalf("event session id = %q", event.SessionID)
	}
	if event.SessionRef == "" {
		t.Fatal("session_ref should be populated for session-start")
	}
}

func TestParseHookTurnStart(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	body, _ := json.Marshal(turnStartRaw{
		SessionID: "S-1",
		Prompt:    "fix the bug",
		Model:     "claude-sonnet-4",
	})

	a := New()
	event, err := a.ParseHook(HookNameTurnStart, body)
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil || event.Type != 2 {
		t.Fatalf("event = %+v, want TurnStart (type 2)", event)
	}
	if event.Prompt != "fix the bug" {
		t.Fatalf("prompt = %q", event.Prompt)
	}
	if event.Model != "claude-sonnet-4" {
		t.Fatalf("model = %q", event.Model)
	}
}

func TestParseHookTurnEndWritesSessionRef(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	session := json.RawMessage(`{"id":"S-1","title":"test"}`)
	messages := json.RawMessage(`[{"info":{"id":"m1","role":"user"},"parts":[{"type":"text","text":"hi"}]}]`)
	body, _ := json.Marshal(turnEndRaw{
		SessionID: "S-1",
		Model:     "claude-sonnet-4",
		Session:   session,
		Messages:  messages,
	})

	a := New()
	event, err := a.ParseHook(HookNameTurnEnd, body)
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil || event.Type != 3 {
		t.Fatalf("event = %+v, want TurnEnd (type 3)", event)
	}
	if event.Model != "claude-sonnet-4" {
		t.Fatalf("model = %q", event.Model)
	}
	if _, err := os.Stat(event.SessionRef); err != nil {
		t.Fatalf("session_ref not written: %v", err)
	}
}

func TestParseHookCompaction(t *testing.T) {
	body, _ := json.Marshal(sessionInfoRaw{SessionID: "S-1"})
	a := New()
	event, err := a.ParseHook(HookNameCompaction, body)
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil || event.Type != 4 {
		t.Fatalf("event = %+v, want Compaction (type 4)", event)
	}
}

func TestParseHookSessionEnd(t *testing.T) {
	body, _ := json.Marshal(sessionInfoRaw{SessionID: "S-1"})
	a := New()
	event, err := a.ParseHook(HookNameSessionEnd, body)
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event == nil || event.Type != 5 {
		t.Fatalf("event = %+v, want SessionEnd (type 5)", event)
	}
}

func TestParseHookEmptyInput(t *testing.T) {
	a := New()
	event, err := a.ParseHook(HookNameSessionStart, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Fatalf("expected nil event, got %+v", event)
	}
}

func TestParseHookUnknown(t *testing.T) {
	a := New()
	body, _ := json.Marshal(sessionInfoRaw{SessionID: "S-1"})
	event, err := a.ParseHook("does-not-exist", body)
	if err != nil {
		t.Fatalf("unexpected error for unknown hook: %v", err)
	}
	if event != nil {
		t.Fatalf("expected nil event for unknown hook, got %+v", event)
	}
}

func TestParseHookMissingSessionID(t *testing.T) {
	a := New()
	body, _ := json.Marshal(sessionInfoRaw{})
	event, err := a.ParseHook(HookNameSessionStart, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Fatalf("expected nil event for missing session_id, got %+v", event)
	}
}

func TestInstallHooksRefusesForeignPluginWithoutForce(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	a := New()

	// Install the Entire-generated plugin once to establish ownership.
	if _, err := a.InstallHooks(false, false); err != nil {
		t.Fatalf("first install error: %v", err)
	}

	pluginPath := filepath.Join(repo, pluginFile)
	if err := os.WriteFile(pluginPath, []byte("// user's custom kilo plugin\nexport default {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Default install must refuse to overwrite the foreign plugin.
	count, err := a.InstallHooks(false, false)
	if err == nil || count != 0 {
		t.Fatalf("foreign install count=%d err=%v, want error", count, err)
	}
	foreign, readErr := os.ReadFile(pluginPath)
	if readErr != nil || !strings.Contains(string(foreign), "user's custom kilo plugin") {
		t.Fatalf("foreign plugin was changed: %q err=%v", foreign, readErr)
	}

	// Explicit force install may replace the foreign plugin.
	count, err = a.InstallHooks(false, true)
	if err != nil || count != 1 {
		t.Fatalf("forced install count=%d err=%v, want success", count, err)
	}
	if !a.AreHooksInstalled() {
		t.Fatal("generated plugin should be installed after forced replacement")
	}
}

func TestUninstallHooksLeavesForeignPluginUntouched(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	a := New()

	pluginPath := filepath.Join(repo, pluginFile)
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte("// user's custom kilo plugin\nexport default {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.UninstallHooks(); err != nil {
		t.Fatalf("uninstall error: %v", err)
	}
	data, readErr := os.ReadFile(pluginPath)
	if readErr != nil || string(data) != "// user's custom kilo plugin\nexport default {}\n" {
		t.Fatalf("uninstall changed foreign plugin: %q err=%v", data, readErr)
	}
}

func TestAreHooksInstalledRequiresOwnershipMarker(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	a := New()

	pluginPath := filepath.Join(repo, pluginFile)
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte("// user's custom kilo plugin\nexport default {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if a.AreHooksInstalled() {
		t.Fatal("foreign plugin reported as installed")
	}
	if err := a.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	data, readErr := os.ReadFile(pluginPath)
	if readErr != nil || !strings.Contains(string(data), "user's custom kilo plugin") {
		t.Fatalf("uninstall changed foreign plugin: %q err=%v", data, readErr)
	}
}

func TestInstallAndUninstallHooks(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	a := New()
	if a.AreHooksInstalled() {
		t.Fatal("hooks should not be installed before install")
	}

	count, err := a.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks error: %v", err)
	}
	if count != 1 {
		t.Fatalf("InstallHooks count = %d, want 1", count)
	}
	if !a.AreHooksInstalled() {
		t.Fatal("hooks should be installed after install")
	}

	pluginPath := filepath.Join(repo, pluginFile)
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}
	if !strings.Contains(string(data), "@kilocode/plugin") {
		t.Fatal("plugin missing @kilocode/plugin import")
	}
	if !strings.Contains(string(data), pluginMarker) {
		t.Fatal("plugin missing marker")
	}
	// Default cmd substitution applied
	if !strings.Contains(string(data), `ENTIRE_CMD = 'entire'`) {
		t.Fatal("plugin missing default entire command substitution")
	}

	// Re-install idempotent (same content)
	count, err = a.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("re-install error: %v", err)
	}
	if count != 0 {
		t.Fatalf("re-install count = %d, want 0", count)
	}

	// localDev rewrites the cmd prefix
	count, err = a.InstallHooks(true, false)
	if err != nil {
		t.Fatalf("localDev install error: %v", err)
	}
	if count != 1 {
		t.Fatalf("localDev install count = %d, want 1", count)
	}
	data, _ = os.ReadFile(pluginPath)
	if !strings.Contains(string(data), `go run`) {
		t.Fatal("localDev plugin missing go-run command substitution")
	}

	if err := a.UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks error: %v", err)
	}
	if a.AreHooksInstalled() {
		t.Fatal("hooks should not be installed after uninstall")
	}
}

func TestGeneratedPluginStartsTurnFromTextPart(t *testing.T) {
	plugin := generatePlugin()
	// Turn-start must not fire with an empty prompt from message.updated —
	// that suppresses the real prompt carried by message.part.updated.
	if strings.Contains(plugin, `prompt: ""`) {
		t.Fatal("plugin still fires turn-start with an empty prompt")
	}
	if !strings.Contains(plugin, "prompt: part.text") {
		t.Fatal("plugin no longer starts a turn from the text part")
	}
}

func TestGeneratedPluginTurnEndIsSynchronous(t *testing.T) {
	plugin := generatePlugin()
	// Turn-end must not await an async fetch — kilo run exits on the idle
	// event and would cancel it. The transcript comes from prepare-transcript.
	if strings.Contains(plugin, "snapshotSession") {
		t.Fatal("plugin still performs an async snapshot at turn-end")
	}
}

func TestSafeSessionID(t *testing.T) {
	cases := map[string]string{
		"":                  "unknown",
		"S-abc_123":         "S-abc_123",
		"path/with/slashes": "path_with_slashes",
		"weird chars!@#$%":  "weird_chars_",
		"dotted.id.is.fine": "dotted.id.is.fine",
	}
	for in, want := range cases {
		if got := safeSessionID(in); got != want {
			t.Errorf("safeSessionID(%q) = %q, want %q", in, got, want)
		}
	}
}
