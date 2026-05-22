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
	wantHooks := map[string]bool{"session.created": false, "session.idle": false}
	for _, h := range info.HookNames {
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

func TestParseHookSessionCreated(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	payload := map[string]any{
		"type":       "session.created",
		"cwd":        repo,
		"session_id": "S-1",
		"session": map[string]any{
			"id":    "S-1",
			"title": "test",
			"time":  map[string]any{"created": 1700000000000},
		},
		"messages": []any{},
	}
	body, _ := json.Marshal(payload)

	a := New()
	event, err := a.ParseHook("session.created", body)
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
	// session_ref should exist on disk
	if _, err := os.Stat(event.SessionRef); err != nil {
		t.Fatalf("session_ref not written: %v", err)
	}
}

func TestParseHookFiltersSubSessions(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	payload := map[string]any{
		"type":       "session.idle",
		"cwd":        repo,
		"session_id": "S-child",
		"parent_id":  "S-parent",
	}
	body, _ := json.Marshal(payload)

	a := New()
	event, err := a.ParseHook("session.idle", body)
	if err != nil {
		t.Fatalf("ParseHook error: %v", err)
	}
	if event != nil {
		t.Fatalf("expected nil event for sub-session, got %+v", event)
	}
}

func TestParseHookEmptyInput(t *testing.T) {
	a := New()
	event, err := a.ParseHook("session.created", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Fatalf("expected nil event, got %+v", event)
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
	if count != 2 {
		t.Fatalf("InstallHooks count = %d, want 2", count)
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
	if !strings.Contains(string(data), "entire-agent-kilo") {
		t.Fatal("plugin missing entire-agent-kilo marker")
	}

	// Re-install without force should no-op
	count, err = a.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("re-install error: %v", err)
	}
	if count != 0 {
		t.Fatalf("re-install count = %d, want 0", count)
	}

	if err := a.UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks error: %v", err)
	}
	if a.AreHooksInstalled() {
		t.Fatal("hooks should not be installed after uninstall")
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
