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

func TestInstallHooks_MultiLineArray(t *testing.T) {
	// The last element of a multi-line array has no trailing comma, so its
	// line — ["b"] — would match the header pattern on its own. Array-depth
	// tracking must keep it inside the entry's body.
	path := writeConfig(t, `[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/user-start-hook"
args = [
  ["a"],
  ["b"]
]
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	content := readConfig(t, path)

	for _, want := range []string{
		"args = [",
		`  ["a"],`,
		`  ["b"]`,
		"]",
		`command = "/usr/local/bin/user-start-hook"`,
		"entire hooks sarvam session-start",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("multi-line array corrupted, missing %q:\n%s", want, content)
		}
	}

	// The foreign entry must still be recognized as one block: its command
	// stays queryable after a reparse.
	blocks, err := loadBlocks(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, block := range blocks {
		if entryCommand(block) == "/usr/local/bin/user-start-hook" {
			found = true
		}
	}
	if !found {
		t.Errorf("the foreign entry's command is no longer queryable after install:\n%s", content)
	}
}

func TestNetBracketsOutsideQuotes(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{"args = [", 1},
		{`  ["a"],`, 0},
		{`  ["b"]`, 0},
		{"]", -1},
		{`command = "array[0]"`, 0},
		{"depth = 0 # [comment]", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := netBracketsOutsideQuotes(tc.line); got != tc.want {
			t.Errorf("netBracketsOutsideQuotes(%q) = %d, want %d", tc.line, got, tc.want)
		}
	}
}

func TestInstallHooks_MatcherScopedForeignGroup(t *testing.T) {
	// A user's matcher-scoped group for one of our events must not capture
	// our entry: the installer always opens its own matcher-less group, so
	// the hook fires for every event, not only where the foreign matcher hits.
	path := writeConfig(t, `[[hooks.SessionStart]]
matcher = "SomeTool"

[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/user-start-hook"
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	content := readConfig(t, path)

	first := strings.Index(content, "[[hooks.SessionStart]]")
	second := strings.Index(content[first+1:], "[[hooks.SessionStart]]") + first + 1
	ours := strings.Index(content, "entire hooks sarvam session-start")
	if !(first < second && second < ours) {
		t.Fatalf("our entry must open its own group after the user's, got:\n%s", content)
	}
	if strings.Count(content, "matcher") != 1 {
		t.Errorf("the user's matcher must stay in their group only:\n%s", content)
	}
	if !strings.Contains(content, `command = "/usr/local/bin/user-start-hook"`) {
		t.Errorf("the user's entry must be preserved:\n%s", content)
	}
}

func TestInstallHooks_HeaderTrailingComments(t *testing.T) {
	path := writeConfig(t, `[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]] # my own startup hook
type = "command"
command = "/usr/local/bin/user-start-hook"
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	content := readConfig(t, path)

	if !strings.Contains(content, `[[hooks.SessionStart.hooks]] # my own startup hook`) {
		t.Errorf("the commented header must be preserved verbatim:\n%s", content)
	}
	if !strings.Contains(content, "entire hooks sarvam session-start") {
		t.Errorf("our entry must still be installed:\n%s", content)
	}
	if strings.Count(content, "[[hooks.SessionStart]]\n") != 2 {
		t.Errorf("our entry must open its own group:\n%s", content)
	}

	// A map-form event header with a trailing comment must be rejected
	// instead of silently corrupting the file with an array alongside a table.
	mapForm := writeConfig(t, "[hooks.Stop] # legacy\n")
	if _, err := upsertHookEntries(mapForm, testEntries(), false); err == nil {
		t.Fatal("map-form [hooks.Stop] with a trailing comment must be rejected")
	}
}

func TestInstallHooks_MultiLineStringValue(t *testing.T) {
	// Lines inside an open multi-line string are body lines however much
	// they look like table headers.
	path := writeConfig(t, `[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]]
type = "command"
command = """
[[hooks.SessionStart]]
not a header, just prose
"""
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	content := readConfig(t, path)

	for _, want := range []string{
		"command = \"\"\"",
		"[[hooks.SessionStart]]\nnot a header, just prose",
		"\"\"\"",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("multi-line string corrupted, missing %q:\n%s", want, content)
		}
	}
	if !strings.Contains(content, "entire hooks sarvam session-start") {
		t.Errorf("our entry must still be installed:\n%s", content)
	}
}

func TestParseConfigBlocks_HeaderRecognition(t *testing.T) {
	cases := []struct {
		line   string
		header bool
	}{
		{"[[hooks.SessionStart]]", true},
		{"[[hooks.SessionStart.hooks]] # note", true},
		{"[hooks.Stop] # legacy", true},
		{`[hooks.state."a#b"]`, true},
		{`["a", "b"],`, false},
		{"command = \"x\"", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isHeaderLine(tc.line); got != tc.header {
			t.Errorf("isHeaderLine(%q) = %v, want %v", tc.line, got, tc.header)
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
