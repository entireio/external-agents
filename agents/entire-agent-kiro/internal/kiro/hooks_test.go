package kiro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallHooksWritesCLIAndIDEHooksAndTrustedCommands(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	count, err := New().InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}
	if count != 9 {
		t.Fatalf("InstallHooks() count = %d, want %d", count, 9)
	}

	cliPath := filepath.Join(repoRoot, ".kiro", "agents", "entire.json")
	if _, err := os.Stat(cliPath); err != nil {
		t.Fatalf("expected CLI hook file at %s: %v", cliPath, err)
	}

	idePath := filepath.Join(repoRoot, ".kiro", "hooks", "entire-stop.kiro.hook")
	if _, err := os.Stat(idePath); err != nil {
		t.Fatalf("expected IDE hook file at %s: %v", idePath, err)
	}

	settingsPath := filepath.Join(repoRoot, ".vscode", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var settings map[string][]string
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}
	commands := settings["kiroAgent.trustedCommands"]
	if len(commands) != 1 || commands[0] != "entire hooks *" {
		t.Fatalf("trusted commands = %#v, want [\"entire hooks *\"]", commands)
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
		t.Fatalf("second InstallHooks() count = %d, want %d", count, 0)
	}

	count, err = New().InstallHooks(false, true)
	if err != nil {
		t.Fatalf("forced InstallHooks() error = %v", err)
	}
	if count != 9 {
		t.Fatalf("forced InstallHooks() count = %d, want %d", count, 9)
	}
}

func TestUninstallHooksRemovesEntireArtifactsAndPreservesOtherSettings(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	settingsDir := filepath.Join(repoRoot, ".vscode")
	if err := os.MkdirAll(settingsDir, 0o750); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	initial := []byte("{\n  \"editor.tabSize\": 2,\n  \"kiroAgent.trustedCommands\": [\"custom-tool run\"]\n}\n")
	if err := os.WriteFile(settingsPath, initial, 0o600); err != nil {
		t.Fatalf("write initial settings: %v", err)
	}

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}
	if err := New().UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, ".kiro", "agents", "entire.json")); !os.IsNotExist(err) {
		t.Fatalf("CLI hook file should be removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".kiro", "hooks", "entire-stop.kiro.hook")); !os.IsNotExist(err) {
		t.Fatalf("IDE hook file should be removed, got err=%v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after uninstall: %v", err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings after uninstall: %v", err)
	}
	if _, ok := settings["editor.tabSize"]; !ok {
		t.Fatal("expected unrelated settings to be preserved")
	}
	var commands []string
	if err := json.Unmarshal(settings["kiroAgent.trustedCommands"], &commands); err != nil {
		t.Fatalf("unmarshal trusted commands: %v", err)
	}
	if len(commands) != 1 || commands[0] != "custom-tool run" {
		t.Fatalf("trusted commands after uninstall = %#v, want [\"custom-tool run\"]", commands)
	}
}

func TestAreHooksInstalledTrueForCLIOrIDEArtifacts(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)

	if New().AreHooksInstalled() {
		t.Fatal("AreHooksInstalled() should be false before install")
	}

	cliDir := filepath.Join(repoRoot, ".kiro", "agents")
	if err := os.MkdirAll(cliDir, 0o750); err != nil {
		t.Fatalf("mkdir cli dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "entire.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write CLI hook file: %v", err)
	}
	if !New().AreHooksInstalled() {
		t.Fatal("AreHooksInstalled() should be true when CLI hook exists")
	}

	if err := os.Remove(filepath.Join(cliDir, "entire.json")); err != nil {
		t.Fatalf("remove CLI hook file: %v", err)
	}
	ideDir := filepath.Join(repoRoot, ".kiro", "hooks")
	if err := os.MkdirAll(ideDir, 0o750); err != nil {
		t.Fatalf("mkdir ide dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ideDir, "entire-stop.kiro.hook"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write IDE hook file: %v", err)
	}
	if !New().AreHooksInstalled() {
		t.Fatal("AreHooksInstalled() should be true when IDE hook exists")
	}
}
