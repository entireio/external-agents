package windsurf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHookFixtures(t *testing.T) {
	tests := []struct { hook, file string; eventType int }{
		{HookNamePreUserPrompt, "pre_user_prompt.json", 2},
		{HookNamePostWriteCode, "post_write_code.json", 0},
		{HookNamePostCascadeResponse, "post_cascade_response.json", 3},
		{HookNamePostCascadeResponseWithTranscript, "post_cascade_response_with_transcript.json", 3},
	}
	for _, test := range tests {
		t.Run(test.hook, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", test.file)); if err != nil { t.Fatal(err) }
			event, err := New().ParseHook(test.hook, data); if err != nil { t.Fatal(err) }
			if test.eventType == 0 { if event != nil { t.Fatalf("code write event = %#v, want nil", event) }; return }
			if event == nil || event.Type != test.eventType || event.SessionID != "trajectory-123" || event.Metadata["execution_id"] != "execution-456" { t.Fatalf("event = %#v", event) }
			if test.hook == HookNamePostCascadeResponseWithTranscript && event.SessionRef != "/home/user/.windsurf/transcripts/trajectory-123.jsonl" { t.Fatalf("session ref = %q", event.SessionRef) }
		})
	}
}

func TestTranscriptHookRequiresTranscriptPath(t *testing.T) {
	input := []byte(`{"agent_action_name":"post_cascade_response_with_transcript","trajectory_id":"trajectory-123","execution_id":"execution-456","tool_info":{"unknown":true}}`)
	event, err := New().ParseHook(HookNamePostCascadeResponseWithTranscript, input)
	if err != nil || event != nil { t.Fatalf("event=%#v err=%v", event, err) }
}

func TestPostWriteLifecyclePreservesRawContext(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "post_write_code.json")); if err != nil { t.Fatal(err) }
	event, err := ParseLifecycleEvent(HookNamePostWriteCode, data); if err != nil { t.Fatal(err) }
	if event == nil || event.TrajectoryID != "trajectory-123" || event.ExecutionID != "execution-456" || event.Metadata["windsurf_tool_info"] == "" { t.Fatalf("lifecycle event = %#v", event) }
}

func TestParseHookDefensiveInput(t *testing.T) {
	for _, hook := range hookNames {
		if event, err := New().ParseHook(hook, []byte(`{"trajectory_id":"trajectory","tool_info":{},"unknown":true}`)); err != nil || event != nil && event.SessionID != "trajectory" { t.Fatalf("unexpected fields: event=%#v err=%v", event, err) }
		if event, err := New().ParseHook(hook, []byte(`{"trajectory_id":`)); err == nil || event != nil { t.Fatalf("malformed: event=%#v err=%v", event, err) }
		if event, err := New().ParseHook(hook, []byte(`{"tool_info":{}}`)); err != nil || event != nil { t.Fatalf("missing identity: event=%#v err=%v", event, err) }
	}
}

func TestInstallAndUninstallHooksPreservesConfiguration(t *testing.T) {
	repo := t.TempDir(); t.Setenv("ENTIRE_REPO_ROOT", repo)
	path := filepath.Join(repo, hooksRelativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(path, []byte(`{"custom":true,"hooks":{"post_write_code":[{"command":"user hook"}],"other":[{"command":"keep"}]}}`), 0o600); err != nil { t.Fatal(err) }
	agent := New()
	count, err := agent.InstallHooks(false, false); if err != nil || count != 4 { t.Fatalf("install count=%d err=%v", count, err) }
	if !agent.AreHooksInstalled() { t.Fatal("hooks not installed") }
	if count, err = agent.InstallHooks(false, false); err != nil || count != 0 { t.Fatalf("second install count=%d err=%v", count, err) }
	if err := agent.UninstallHooks(); err != nil { t.Fatal(err) }
	if agent.AreHooksInstalled() { t.Fatal("hooks remain installed") }
	if err := agent.UninstallHooks(); err != nil { t.Fatal(err) }
	data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
	var config struct { Custom bool `json:"custom"`; Hooks map[string][]struct { Command string `json:"command"` } `json:"hooks"` }
	if err := json.Unmarshal(data, &config); err != nil { t.Fatal(err) }
	if !config.Custom || len(config.Hooks[HookNamePostWriteCode]) != 1 || config.Hooks[HookNamePostWriteCode][0].Command != "user hook" || len(config.Hooks["other"]) != 1 { t.Fatalf("configuration not preserved: %s", data) }
}
