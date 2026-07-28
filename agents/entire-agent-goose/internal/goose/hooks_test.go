package goose

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// stubRunner records export calls and writes a canned export file.
type stubRunner struct {
	calls  []string
	export []byte
	err    error
}

func (r *stubRunner) ExportSession(_ context.Context, sessionID, outPath string) error {
	r.calls = append(r.calls, sessionID)
	if r.err != nil {
		return r.err
	}
	return os.WriteFile(outPath, r.export, 0o600)
}

// Captured from goose 1.37.0 (scripts/verify-goose.sh).
const (
	sessionStartPayload = `{"event":"SessionStart","session_id":"20260611_1","matcher_context":null}`
	promptPayload       = `{"event":"UserPromptSubmit","session_id":"20260611_1","matcher_context":"Create a file","message":"Create a file"}`
	stopPayload         = `{"event":"Stop","session_id":"20260611_1","matcher_context":null}`
	sessionEndPayload   = `{"event":"SessionEnd","session_id":"20260611_1","matcher_context":null}`
)

func testAgent(t *testing.T, runner CommandRunner) *Agent {
	t.Helper()
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	// Keep transcript exports out of the real ~/.local/share/goose.
	t.Setenv("GOOSE_PATH_ROOT", t.TempDir())
	return &Agent{CommandRunner: runner}
}

func TestParseHookSessionStart(t *testing.T) {
	runner := &stubRunner{export: []byte(exportFixture)}
	a := testAgent(t, runner)

	event, err := a.ParseHook(HookNameSessionStart, []byte(sessionStartPayload))
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	if event.Type != 1 {
		t.Errorf("type = %d, want 1", event.Type)
	}
	if event.SessionID != "20260611_1" {
		t.Errorf("session id = %q", event.SessionID)
	}
	if event.SessionRef == "" || filepath.Base(event.SessionRef) != "20260611_1.json" {
		t.Errorf("session ref = %q", event.SessionRef)
	}
	if event.Model != "anthropic/claude-opus-4.6" {
		t.Errorf("model = %q", event.Model)
	}
	if event.Timestamp == "" {
		t.Error("timestamp must be set")
	}
	if len(runner.calls) != 1 || runner.calls[0] != "20260611_1" {
		t.Errorf("export calls = %v", runner.calls)
	}
}

func TestParseHookUserPromptSubmit(t *testing.T) {
	a := testAgent(t, &stubRunner{export: []byte(exportFixture)})

	event, err := a.ParseHook(HookNameUserPromptSubmit, []byte(promptPayload))
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	if event.Type != 2 {
		t.Errorf("type = %d, want 2", event.Type)
	}
	if event.Prompt != "Create a file" {
		t.Errorf("prompt = %q", event.Prompt)
	}
}

func TestParseHookStop(t *testing.T) {
	runner := &stubRunner{export: []byte(exportFixture)}
	a := testAgent(t, runner)

	event, err := a.ParseHook(HookNameStop, []byte(stopPayload))
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	if event.Type != 3 {
		t.Errorf("type = %d, want 3", event.Type)
	}
	if len(runner.calls) != 1 {
		t.Errorf("stop must export the session, calls = %v", runner.calls)
	}
}

func TestParseHookStopExportFailureStillEmitsEvent(t *testing.T) {
	a := testAgent(t, &stubRunner{err: os.ErrPermission})

	event, err := a.ParseHook(HookNameStop, []byte(stopPayload))
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	if event == nil || event.Type != 3 {
		t.Fatalf("event = %+v, want TurnEnd", event)
	}
}

func TestParseHookSessionEnd(t *testing.T) {
	a := testAgent(t, &stubRunner{export: []byte(exportFixture)})

	event, err := a.ParseHook(HookNameSessionEnd, []byte(sessionEndPayload))
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	if event.Type != 5 {
		t.Errorf("type = %d, want 5", event.Type)
	}
}

func TestParseHookIgnoresIrrelevantInput(t *testing.T) {
	a := testAgent(t, &stubRunner{})

	cases := map[string][]byte{
		"empty input":        []byte(""),
		"whitespace":         []byte("  \n"),
		"missing session id": []byte(`{"event":"Stop"}`),
	}
	for name, input := range cases {
		event, err := a.ParseHook(HookNameStop, input)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
		if event != nil {
			t.Errorf("%s: event = %+v, want nil", name, event)
		}
	}

	if event, err := a.ParseHook("unknown-hook", []byte(stopPayload)); err != nil || event != nil {
		t.Errorf("unknown hook: event=%+v err=%v, want nil/nil", event, err)
	}

	if _, err := a.ParseHook(HookNameStop, []byte("not json")); err == nil {
		t.Error("malformed JSON must error")
	}
}

func TestInstallUninstallHooks(t *testing.T) {
	a := testAgent(t, &stubRunner{})
	repoRoot := os.Getenv("ENTIRE_REPO_ROOT")

	if a.AreHooksInstalled() {
		t.Fatal("hooks must not be installed initially")
	}

	count, err := a.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	if count != 4 {
		t.Errorf("installed = %d, want 4", count)
	}
	if !a.AreHooksInstalled() {
		t.Fatal("AreHooksInstalled must be true after install")
	}

	// The hooks file must be a valid goose plugin hooks.json wiring all four
	// lifecycle events to the entire CLI.
	data, err := os.ReadFile(filepath.Join(repoRoot, hooksRelFile))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v", err)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"} {
		rules := file.Hooks[event]
		if len(rules) != 1 || len(rules[0].Hooks) != 1 {
			t.Fatalf("event %s: unexpected rules %+v", event, rules)
		}
		action := rules[0].Hooks[0]
		if action.Type != "command" {
			t.Errorf("event %s: type = %q", event, action.Type)
		}
		if want := "entire hooks goose "; len(action.Command) <= len(want) || action.Command[:len(want)] != want {
			t.Errorf("event %s: command = %q", event, action.Command)
		}
	}

	// Reinstall without force is a no-op.
	count, err = a.InstallHooks(false, false)
	if err != nil || count != 0 {
		t.Errorf("idempotent install = (%d, %v), want (0, nil)", count, err)
	}
	// Force reinstall rewrites.
	count, err = a.InstallHooks(false, true)
	if err != nil || count != 4 {
		t.Errorf("forced install = (%d, %v), want (4, nil)", count, err)
	}

	if err := a.UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks: %v", err)
	}
	if a.AreHooksInstalled() {
		t.Error("hooks must be gone after uninstall")
	}
	if _, err := os.Stat(filepath.Join(repoRoot, pluginRelDir)); !os.IsNotExist(err) {
		t.Error("empty plugin dir must be removed on uninstall")
	}
}

func TestAreHooksInstalledRejectsForeignHooksFile(t *testing.T) {
	a := testAgent(t, &stubRunner{})
	repoRoot := os.Getenv("ENTIRE_REPO_ROOT")

	path := filepath.Join(repoRoot, hooksRelFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"something-else"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if a.AreHooksInstalled() {
		t.Error("a hooks.json without entire commands must not count as installed")
	}
}
