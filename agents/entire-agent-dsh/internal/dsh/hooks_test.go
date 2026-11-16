package dsh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseHookMappings(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("DSH_HOME", t.TempDir())

	wantTypes := map[string]int{
		HookSessionStart:  1,
		HookTurnStart:     2,
		HookTurnEnd:       3,
		HookCompaction:    4,
		HookSessionEnd:    5,
		HookSubagentStart: 6,
		HookSubagentEnd:   7,
	}
	for hook, wantType := range wantTypes {
		payload, err := json.Marshal(map[string]any{
			"hook_type":   hook,
			"session_id":  "session-hook",
			"timestamp":   "2026-01-13T12:00:00Z",
			"user_prompt": "hello",
		})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		event, err := (&Agent{}).ParseHook(hook, payload)
		if err != nil {
			t.Fatalf("ParseHook(%s): %v", hook, err)
		}
		if event == nil {
			t.Fatalf("ParseHook(%s) returned nil", hook)
		}
		if event.Type != wantType {
			t.Errorf("ParseHook(%s) type = %d, want %d", hook, event.Type, wantType)
		}
		if event.SessionID != "session-hook" {
			t.Errorf("ParseHook(%s) session id = %q", hook, event.SessionID)
		}
		if event.SessionRef == "" {
			t.Errorf("ParseHook(%s) session ref is empty", hook)
		}
		if _, err := time.Parse(time.RFC3339, event.Timestamp); err != nil {
			t.Errorf("ParseHook(%s) timestamp %q: %v", hook, event.Timestamp, err)
		}
	}
}

func TestParseHookBridgePayload(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("DSH_HOME", t.TempDir())

	payload := []byte(`{"hook_type":"turn_start","session_id":"session-bridge","timestamp":"2026-11-02T20:34:44Z","raw_data":{"dsh":{"turn":2,"reason":"completed"}},"user_prompt":"fix the bug"}`)
	event, err := (&Agent{}).ParseHook("turn-start", payload)
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	if event == nil || event.Type != 2 || event.Prompt != "fix the bug" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.SessionRef == "" || filepath.Base(event.SessionRef) != "session-bridge.jsonl" {
		t.Errorf("unexpected session ref: %q", event.SessionRef)
	}
}

func TestParseHookNormalizesFractionalTimestamp(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("DSH_HOME", t.TempDir())

	payload := []byte(`{"hook_type":"turn_end","session_id":"session-frac","timestamp":"2026-11-02T20:34:44.373Z"}`)
	event, err := (&Agent{}).ParseHook("turn-end", payload)
	if err != nil || event == nil {
		t.Fatalf("ParseHook: %+v, %v", event, err)
	}
	if event.Timestamp != "2026-11-02T20:34:44Z" {
		t.Errorf("timestamp = %q, want 2026-11-02T20:34:44Z", event.Timestamp)
	}
	if _, err := time.Parse(time.RFC3339, event.Timestamp); err != nil {
		t.Errorf("normalized timestamp is not RFC3339: %v", err)
	}
}

func TestParseHookEmptyAndUnknown(t *testing.T) {
	agent := &Agent{}
	if event, err := agent.ParseHook("turn-end", nil); err != nil || event != nil {
		t.Errorf("empty input = %+v, %v; want nil, nil", event, err)
	}
	if event, err := agent.ParseHook("not-a-hook", []byte(`{"session_id":"s"}`)); err != nil || event != nil {
		t.Errorf("unknown hook = %+v, %v; want nil, nil", event, err)
	}
	if event, err := agent.ParseHook("turn-end", []byte(`{"session_id":""}`)); err != nil || event != nil {
		t.Errorf("empty session id = %+v, %v; want nil, nil", event, err)
	}
}

func TestInstallHooksLifecycle(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := &Agent{}

	if agent.AreHooksInstalled() {
		t.Fatal("hooks reported installed before install")
	}
	count, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	if count != len(hookNames) {
		t.Errorf("first install count = %d, want %d", count, len(hookNames))
	}
	if !agent.AreHooksInstalled() {
		t.Fatal("hooks not reported installed")
	}

	markerPath := filepath.Join(repo, ".entire", "dsh-hooks.json")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var marker hookMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("parse marker: %v", err)
	}
	if marker.Agent != "dsh" || marker.Bridge != bridgePackage || marker.Version != 1 {
		t.Errorf("unexpected marker: %+v", marker)
	}
	if len(marker.HookNames) != len(hookNames) {
		t.Errorf("marker hook count = %d, want %d", len(marker.HookNames), len(hookNames))
	}

	count, err = agent.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("second InstallHooks: %v", err)
	}
	if count != 0 {
		t.Errorf("idempotent install count = %d, want 0", count)
	}

	if _, err := agent.InstallHooks(true, true); err != nil {
		t.Fatalf("force InstallHooks: %v", err)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks: %v", err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("hooks reported installed after uninstall")
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatalf("second UninstallHooks: %v", err)
	}
}
