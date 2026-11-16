package sarvam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEntries() []configHookEntry {
	entries := make([]configHookEntry, 0, len(hookSpecs))
	for _, spec := range hookSpecs {
		entries = append(entries, configHookEntry{
			Event:   spec.SarvamEvent,
			Command: hookCommand(spec.HookName),
		})
	}
	return entries
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".sarvam", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInstallHooks_FreshConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sarvam", "config.toml")

	count, err := upsertHookEntries(path, testEntries(), false)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(hookSpecs) {
		t.Fatalf("first install: hooks_installed = %d, want %d", count, len(hookSpecs))
	}

	content := readConfig(t, path)
	for _, spec := range hookSpecs {
		if !strings.Contains(content, "[[hooks."+spec.SarvamEvent+"]]") {
			t.Errorf("config missing [[hooks.%s]] group header", spec.SarvamEvent)
		}
		if !strings.Contains(content, "entire hooks sarvam "+spec.HookName) {
			t.Errorf("config missing command for %s", spec.HookName)
		}
	}
}

func TestInstallHooks_Idempotent(t *testing.T) {
	path := writeConfig(t, "")

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	count, err := upsertHookEntries(path, testEntries(), false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("second install: hooks_installed = %d, want 0", count)
	}
}

func TestInstallHooks_ForceReinstalls(t *testing.T) {
	path := writeConfig(t, "")

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	count, err := upsertHookEntries(path, testEntries(), true)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(hookSpecs) {
		t.Fatalf("force install: hooks_installed = %d, want %d", count, len(hookSpecs))
	}
}

func TestInstallHooks_PreservesUnrelatedContent(t *testing.T) {
	path := writeConfig(t, `# my config
model = "glm-5.3"

[model.lite]
context_window = 128000

[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/my-own-start-hook"
timeout = 30
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	content := readConfig(t, path)

	for _, want := range []string{
		"# my config",
		`model = "glm-5.3"`,
		"[model.lite]",
		"context_window = 128000",
		`command = "/usr/local/bin/my-own-start-hook"`,
		"entire hooks sarvam session-start",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("config lost unrelated content: %q missing from:\n%s", want, content)
		}
	}
}

func TestInstallHooks_RepairsParentlessHooksShape(t *testing.T) {
	// The shape the sarvam docs' SessionStart example produces: hook entries
	// with no matcher-group header, which the CLI rejects wholesale
	// ("invalid type: map, expected a sequence").
	path := writeConfig(t, `[hooks]
[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/my-own-start-hook"
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	content := readConfig(t, path)

	if !strings.Contains(content, "[[hooks.SessionStart]]\n[[hooks.SessionStart.hooks]]") {
		t.Errorf("parentless repair did not insert group header:\n%s", content)
	}
	if !strings.Contains(content, `command = "/usr/local/bin/my-own-start-hook"`) {
		t.Errorf("repair dropped the author's hook entry:\n%s", content)
	}
}

func TestInstallHooks_RejectsMapFormEvent(t *testing.T) {
	path := writeConfig(t, `[hooks.SessionStart]
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err == nil {
		t.Fatal("expected an error for the table-form event header")
	}
}

func TestUninstallHooks_RemovesOnlyEntireEntries(t *testing.T) {
	path := writeConfig(t, `model = "glm-5.3"

[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/my-own-start-hook"
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	if err := removeHookEntries(path, hookSpecs); err != nil {
		t.Fatal(err)
	}

	content := readConfig(t, path)
	if strings.Contains(content, "entire hooks sarvam") {
		t.Errorf("uninstall left entire entries behind:\n%s", content)
	}
	for _, want := range []string{
		`model = "glm-5.3"`,
		`command = "/usr/local/bin/my-own-start-hook"`,
		"[[hooks.SessionStart]]",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("uninstall removed foreign content %q:\n%s", want, content)
		}
	}
}

func TestUninstallHooks_RemovesEmptyManagedGroups(t *testing.T) {
	path := writeConfig(t, "")

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	if err := removeHookEntries(path, hookSpecs); err != nil {
		t.Fatal(err)
	}

	content := readConfig(t, path)
	if strings.Contains(content, "hooks.") {
		t.Errorf("uninstall left managed hook scaffolding behind:\n%s", content)
	}
}

func TestHookEntryInstalled(t *testing.T) {
	path := writeConfig(t, "")
	spec := hookSpecs[0]
	command := hookCommand(spec.HookName)

	if hookEntryInstalled(path, spec.SarvamEvent, spec.EntryName, command) {
		t.Fatal("hooks must not be installed before install-hooks")
	}
	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	if !hookEntryInstalled(path, spec.SarvamEvent, spec.EntryName, command) {
		t.Fatal("hooks must be installed after install-hooks")
	}
}

func TestParseKeyValue_QuotedStrings(t *testing.T) {
	cases := []struct {
		line  string
		key   string
		value string
	}{
		{`command = "sh -c 'echo hi'"`, "command", `sh -c 'echo hi'`},
		{`command = 'sh -c "echo hi"'`, "command", `sh -c "echo hi"`},
		{"command = \"a\\\"b\"", "command", `a"b`},
		{`timeout = 30`, "timeout", "30"},
		{`# comment`, "", ""},
	}
	for _, tc := range cases {
		key, value, ok := parseKeyValue(tc.line)
		if key != tc.key || value != tc.value {
			t.Errorf("parseKeyValue(%q) = (%q, %q, %v), want (%q, %q)", tc.line, key, value, ok, tc.key, tc.value)
		}
	}
}
