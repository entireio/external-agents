package zcode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-zcode/internal/protocol"
)

func withTempZCodeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ZCODE_HOME", home)
	return home
}

func TestParseHookEventMappings(t *testing.T) {
	agent := &Agent{}
	tests := []struct {
		name     string
		hook     string
		payload  string
		wantType int
		wantNil  bool
	}{
		{name: "session start", hook: HookNameSessionStart,
			payload: `{"session_id":"sess_1","source":"startup"}`, wantType: 1},
		{name: "resume is still session start", hook: HookNameSessionStart,
			payload: `{"session_id":"sess_1","source":"resume"}`, wantType: 1},
		{name: "compact source becomes compaction", hook: HookNameSessionStart,
			payload: `{"session_id":"sess_1","source":"compact"}`, wantType: 4},
		{name: "dedicated compaction hook", hook: HookNameCompaction,
			payload: `{"session_id":"sess_1","source":"compact"}`, wantType: 4},
		{name: "turn start carries prompt", hook: HookNameTurnStart,
			payload: `{"session_id":"sess_1","prompt":"fix the bug"}`, wantType: 2},
		{name: "turn end carries response", hook: HookNameTurnEnd,
			payload: `{"session_id":"sess_1","last_assistant_message":"done"}`, wantType: 3},
		{name: "empty session id is ignored", hook: HookNameTurnStart,
			payload: `{"prompt":"no session"}`, wantNil: true},
		{name: "unknown hook is ignored", hook: "pre-tool-use",
			payload: `{"session_id":"sess_1"}`, wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := agent.ParseHook(tt.hook, []byte(tt.payload))
			if err != nil {
				t.Fatalf("ParseHook: %v", err)
			}
			if tt.wantNil {
				if event != nil {
					t.Fatalf("want nil event, got %+v", event)
				}
				return
			}
			if event == nil {
				t.Fatal("want event, got nil")
			}
			if event.Type != tt.wantType {
				t.Fatalf("type = %d, want %d", event.Type, tt.wantType)
			}
			if event.SessionID != "sess_1" {
				t.Fatalf("session id = %q", event.SessionID)
			}
			if event.SessionRef == "" {
				t.Fatal("session ref must point at the export path")
			}
		})
	}
}

func TestParseHookTurnFields(t *testing.T) {
	agent := &Agent{}
	start, err := agent.ParseHook(HookNameTurnStart, []byte(`{"session_id":"s","prompt":"hello"}`))
	if err != nil || start == nil {
		t.Fatalf("turn-start: %v %v", start, err)
	}
	if start.Prompt != "hello" {
		t.Fatalf("prompt = %q", start.Prompt)
	}
	end, err := agent.ParseHook(HookNameTurnEnd, []byte(`{"session_id":"s","last_assistant_message":"all done"}`))
	if err != nil || end == nil {
		t.Fatalf("turn-end: %v %v", end, err)
	}
	if end.ResponseMessage != "all done" {
		t.Fatalf("response = %q", end.ResponseMessage)
	}
}

func TestParseHookEmptyInput(t *testing.T) {
	agent := &Agent{}
	event, err := agent.ParseHook(HookNameSessionStart, nil)
	if err != nil || event != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", event, err)
	}
}

func TestInstallAreUninstallHooks(t *testing.T) {
	withTempZCodeHome(t)
	agent := &Agent{}

	if agent.AreHooksInstalled() {
		t.Fatal("nothing installed yet")
	}
	n, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if n != len(hookSpecs) {
		t.Fatalf("installed %d hooks, want %d", n, len(hookSpecs))
	}
	if !agent.AreHooksInstalled() {
		t.Fatal("hooks should be installed")
	}

	// Idempotent reinstall.
	n, err = agent.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if n != 0 {
		t.Fatalf("reinstall added %d hooks, want 0", n)
	}

	if err := agent.UninstallHooks(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("hooks should be uninstalled")
	}
}

func TestInstallHooksPreservesForeignConfig(t *testing.T) {
	home := withTempZCodeHome(t)
	path := filepath.Join(home, "cli", "config.json")
	foreign := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"events": map[string]any{
				"Stop": []any{map[string]any{
					"hooks": []any{map[string]any{"type": "command", "command": "echo user-hook"}},
				}},
			},
		},
	}
	raw, _ := json.Marshal(foreign)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	agent := &Agent{}
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatalf("install: %v", err)
	}

	var got map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("config not valid JSON after install: %v", err)
	}
	if got["theme"] != "dark" {
		t.Fatalf("foreign key lost: %v", got)
	}

	if err := agent.UninstallHooks(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	got = map[string]any{}
	data, _ = os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["theme"] != "dark" {
		t.Fatalf("foreign key lost on uninstall: %v", got)
	}
	hooks2, ok := got["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks lost: %v", got)
	}
	events, ok := hooks2["events"].(map[string]any)
	if !ok {
		t.Fatalf("events lost: %v", got)
	}
	stopList, ok := events["Stop"].([]any)
	if !ok || len(stopList) != 1 {
		t.Fatalf("user Stop hook removed: %v", events["Stop"])
	}
	hook, ok := stopList[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if !ok || hook["command"] != "echo user-hook" {
		t.Fatalf("wrong survivor: %v", stopList)
	}
}

func TestInstallHooksForceReplacesStaleEntries(t *testing.T) {
	withTempZCodeHome(t)
	agent := &Agent{}
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	n, err := agent.InstallHooks(false, true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if n != len(hookSpecs) {
		t.Fatalf("force install replaced %d, want %d", n, len(hookSpecs))
	}
	if !agent.AreHooksInstalled() {
		t.Fatal("hooks missing after force install")
	}
}

func TestInfoDeclaresImplementedCapabilities(t *testing.T) {
	info := (&Agent{}).Info()
	if info.Name != "zcode" || info.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("info: %+v", info)
	}
	caps := info.Capabilities
	if !caps.Hooks || !caps.TranscriptAnalyzer || !caps.TranscriptPreparer || !caps.TokenCalculator {
		t.Fatalf("missing declared capabilities: %+v", caps)
	}
	for _, forbidden := range []bool{caps.TextGenerator, caps.SubagentAwareExtractor, caps.CompactTranscript} {
		if forbidden {
			t.Fatalf("undeclared capability must be false: %+v", caps)
		}
	}
}
