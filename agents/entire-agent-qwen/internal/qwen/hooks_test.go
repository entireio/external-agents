package qwen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The installed hook command lands in .qwen/settings.json, a repo-local file
// that is routinely committed and shared. It must resolve `entire` through
// PATH when the hook runs, never bake in whatever absolute path `entire`
// happened to have on the installing machine — that path does not exist on a
// teammate's machine or in CI, where the `command -v` guard then makes every
// hook a silent no-op and the session is never captured.
func TestInstallHooksResolvesEntireThroughPATH(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	// Put an `entire` executable on PATH at a path unique to this machine.
	binDir := t.TempDir()
	entirePath := filepath.Join(binDir, "entire")
	if err := os.WriteFile(entirePath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(repo, ".qwen", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), binDir) {
		t.Fatalf("hook command baked in the install-time absolute path %q:\n%s", entirePath, data)
	}

	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Name    string `json:"name"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, matchers := range settings.Hooks {
		for _, matcher := range matchers {
			for _, hook := range matcher.Hooks {
				if !strings.HasPrefix(hook.Name, "entire-") {
					continue
				}
				found++
				if !strings.Contains(hook.Command, "command -v entire ") {
					t.Fatalf("hook %s does not probe entire on PATH: %s", hook.Name, hook.Command)
				}
				if !strings.Contains(hook.Command, "entire hooks qwen ") {
					t.Fatalf("hook %s does not invoke entire from PATH: %s", hook.Name, hook.Command)
				}
			}
		}
	}
	if found != len(hookSpecs) {
		t.Fatalf("expected %d entire hooks, found %d", len(hookSpecs), found)
	}
}
