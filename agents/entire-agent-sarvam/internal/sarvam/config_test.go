package sarvam

import (
	"fmt"
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

func TestRepairParentlessHooks_SingleGroupForAllEntries(t *testing.T) {
	// Several parentless hook entries for one event must be repaired with a
	// single synthetic group header, so they stay one matcher group — a
	// header per entry would silently split the user's entries.
	content := `[hooks]
[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/first-start-hook"
[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/second-start-hook"
[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = "/usr/local/bin/prompt-hook"
`
	blocks, trailingNewline := parseConfigBlocks(content)
	repaired := serializeConfigBlocks(repairParentlessHooks(blocks), trailingNewline)

	if strings.Count(repaired, "[[hooks.SessionStart]]\n") != 1 {
		t.Fatalf("exactly one synthetic SessionStart group header may be inserted, got:\n%s", repaired)
	}
	if strings.Count(repaired, "[[hooks.UserPromptSubmit]]\n") != 1 {
		t.Fatalf("exactly one synthetic UserPromptSubmit group header may be inserted, got:\n%s", repaired)
	}
	for _, want := range []string{
		`command = "/usr/local/bin/first-start-hook"`,
		`command = "/usr/local/bin/second-start-hook"`,
		`command = "/usr/local/bin/prompt-hook"`,
	} {
		if !strings.Contains(repaired, want) {
			t.Errorf("entries lost during repair, missing %q:\n%s", want, repaired)
		}
	}

	// The end-to-end install must not add a second synthetic header for the
	// user's entries on top of the repair: our own entry opens its own new
	// group, so exactly two headers exist in total.
	path := writeConfig(t, content)
	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	installed := readConfig(t, path)
	if got := strings.Count(installed, "[[hooks.SessionStart]]\n"); got != 2 {
		t.Fatalf("installed config should carry the repaired group plus ours, got %d headers:\n%s", got, installed)
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
	blocks, _, err := loadBlocks(path)
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
	if first >= second || second >= ours {
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

func TestInstallHooks_RejectsMapFormEntry(t *testing.T) {
	// The single-bracket ENTRY form is the same class of corruption as the
	// single-bracket group: inserting a [[hooks.Stop.hooks]] array next to a
	// table definition is a TOML type conflict, and the CLI would silently
	// drop every hook.
	path := writeConfig(t, `[hooks.Stop.hooks]
type = "command"
command = "/usr/local/bin/my-own-stop-hook"
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err == nil {
		t.Fatal("expected an error for the table-form entry header")
	}
	if strings.Contains(readConfig(t, path), "entire hooks sarvam") {
		t.Errorf("install must not touch a config it rejects:\n%s", readConfig(t, path))
	}
}

func TestInstallHooks_PersistsParentlessRepair(t *testing.T) {
	// The sarvam docs' example shape: our entries are already present with
	// exact commands but no [[hooks.<Event>]] group headers, so the CLI
	// rejects the whole table. install-hooks must persist the repair even
	// though no entry changed, and the entries must not count as installed
	// while they are parentless.
	var content strings.Builder
	for _, spec := range hookSpecs {
		content.WriteString("[[hooks." + spec.SarvamEvent + ".hooks]]\n")
		content.WriteString("type = \"command\"\n")
		content.WriteString("command = " + encodeTOMLString(hookCommand(spec.HookName)) + "\n")
	}
	path := writeConfig(t, content.String())

	spec := hookSpecs[0]
	command := hookCommand(spec.HookName)
	if hookEntryInstalled(path, spec.SarvamEvent, command) {
		t.Fatal("parentless entries must not count as installed: the CLI cannot parse the config")
	}

	count, err := upsertHookEntries(path, testEntries(), false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("hooks_installed = %d, want 0 (no entry changed, only the repair)", count)
	}
	repaired := readConfig(t, path)
	for _, spec := range hookSpecs {
		if !strings.Contains(repaired, "[[hooks."+spec.SarvamEvent+"]]\n[[hooks."+spec.SarvamEvent+".hooks]]") {
			t.Fatalf("repair not persisted for %s:\n%s", spec.SarvamEvent, repaired)
		}
	}
	if !hookEntryInstalled(path, spec.SarvamEvent, command) {
		t.Fatal("the repaired config must count as installed")
	}
}

func TestUninstallHooks_NoOpLeavesFileUntouched(t *testing.T) {
	// A config holding none of our entries — indented header, CRLF
	// endings, no trailing newline — must survive uninstall byte-for-byte.
	// When an install does change the file, the untouched header keeps its
	// raw indentation and line ending.
	content := "\t[model.lite]\r\ncontext_window = 128000"
	path := writeConfig(t, content)

	if err := removeHookEntries(path); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t, path); got != content {
		t.Fatalf("uninstall rewrote a config with no entire entries:\n%q", got)
	}

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t, path); !strings.Contains(got, "\t[model.lite]\r\ncontext_window = 128000") {
		t.Fatalf("the indented CRLF header was rewritten:\n%q", got)
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
	if err := removeHookEntries(path); err != nil {
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
	if err := removeHookEntries(path); err != nil {
		t.Fatal(err)
	}

	content := readConfig(t, path)
	if strings.Contains(content, "hooks.") {
		t.Errorf("uninstall left managed hook scaffolding behind:\n%s", content)
	}
}

func TestUninstallHooks_RemovesLegacyPreCompactScaffolding(t *testing.T) {
	// An older release installed a PreCompact hook; that release's config
	// still carries its group and entry. Uninstall must remove the owned
	// entry and the empty group it leaves behind, while never touching a
	// PreCompact group the user authored without our entry.
	legacy := "[[hooks.PreCompact]]\n[[hooks.PreCompact.hooks]]\ntype = \"command\"\ncommand = " + encodeTOMLString(hookCommand("pre-compact")) + "\n"
	path := writeConfig(t, legacy+`[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/my-own-start-hook"
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	if err := removeHookEntries(path); err != nil {
		t.Fatal(err)
	}

	content := readConfig(t, path)
	if strings.Contains(content, "PreCompact") {
		t.Fatalf("uninstall left the legacy pre-compact scaffolding behind:\n%s", content)
	}
	if strings.Contains(content, "entire hooks sarvam") {
		t.Errorf("uninstall left our entries behind:\n%s", content)
	}
	if !strings.Contains(content, `command = "/usr/local/bin/my-own-start-hook"`) {
		t.Errorf("uninstall removed the user's hook entry:\n%s", content)
	}
}

func TestUninstallHooks_KeepsUserAuthoredPreCompactGroup(t *testing.T) {
	// An empty PreCompact group the user wrote — never parented by one of
	// our entries — is theirs, not legacy residue: uninstall leaves it.
	path := writeConfig(t, `[[hooks.PreCompact]]
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	if err := removeHookEntries(path); err != nil {
		t.Fatal(err)
	}

	content := readConfig(t, path)
	if !strings.Contains(content, "[[hooks.PreCompact]]") {
		t.Fatalf("uninstall removed the user's own pre-compact group:\n%s", content)
	}
	if strings.Contains(content, "entire hooks sarvam") {
		t.Errorf("uninstall left our entries behind:\n%s", content)
	}
}

func TestUninstallHooks_ForeignEntryKeepsOnlyItsGroup(t *testing.T) {
	// The user's own SessionStart entry shares our event. After uninstall
	// their group and entry stay and our group is gone — an empty managed
	// group must not survive just because the event still has entries.
	path := writeConfig(t, `[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]]
type = "command"
command = "/usr/local/bin/my-own-start-hook"
`)

	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	if err := removeHookEntries(path); err != nil {
		t.Fatal(err)
	}

	content := readConfig(t, path)
	if got := strings.Count(content, "[[hooks.SessionStart]]"); got != 1 {
		t.Fatalf("%d SessionStart group headers after uninstall, want the user's one:\n%s", got, content)
	}
	if !strings.Contains(content, `command = "/usr/local/bin/my-own-start-hook"`) {
		t.Errorf("uninstall removed the user's hook entry:\n%s", content)
	}
	if strings.Contains(content, "entire hooks sarvam") {
		t.Errorf("uninstall left our entry behind:\n%s", content)
	}
}

func TestInstallHooks_PreservesTrailingComments(t *testing.T) {
	// Owned entry lines carrying trailing comments must still be recognized
	// as ours: without comment stripping, the next install duplicates the
	// entry (type no longer reads "command") or rewrites the command line
	// and silently drops the comment.
	spec := hookSpecs[0]
	content := fmt.Sprintf("[[hooks.%s]]\n[[hooks.%s.hooks]]\ntype = \"command\" # entire\ncommand = %s # managed by entire install-hooks\n",
		spec.SarvamEvent, spec.SarvamEvent, encodeTOMLString(hookCommand(spec.HookName)))
	path := writeConfig(t, content)

	count, err := upsertHookEntries(path, testEntries(), false)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(hookSpecs)-1 {
		t.Fatalf("install: hooks_installed = %d, want %d (the commented entry counts as installed)", count, len(hookSpecs)-1)
	}
	if got := strings.Count(readConfig(t, path), "[[hooks."+spec.SarvamEvent+".hooks]]"); got != 1 {
		t.Fatalf("%d %s entry headers after install, want 1 (no duplicate):\n%s", got, spec.SarvamEvent, readConfig(t, path))
	}
	for _, want := range []string{`type = "command" # entire`, "# managed by entire install-hooks"} {
		if !strings.Contains(readConfig(t, path), want) {
			t.Errorf("install rewrote the commented entry, lost %q:\n%s", want, readConfig(t, path))
		}
	}
}

func TestInstallHooks_ForceLeavesNoEmptyGroups(t *testing.T) {
	// Every force install drops the owned entries and re-inserts them; the
	// stranded group headers must be cleaned up too, or each force run
	// strands another empty [[hooks.<Event>]] group in the config.
	path := writeConfig(t, "")
	if _, err := upsertHookEntries(path, testEntries(), true); err != nil {
		t.Fatal(err)
	}

	for run := 1; run <= 2; run++ {
		count, err := upsertHookEntries(path, testEntries(), true)
		if err != nil {
			t.Fatal(err)
		}
		if count != len(hookSpecs) {
			t.Fatalf("force run %d: hooks_installed = %d, want %d", run, count, len(hookSpecs))
		}
		content := readConfig(t, path)
		for _, spec := range hookSpecs {
			if got := strings.Count(content, "[[hooks."+spec.SarvamEvent+"]]"); got != 1 {
				t.Fatalf("force run %d: %d [[hooks.%s]] headers, want 1:\n%s", run, got, spec.SarvamEvent, content)
			}
			if got := strings.Count(content, "[[hooks."+spec.SarvamEvent+".hooks]]"); got != 1 {
				t.Fatalf("force run %d: %d [[hooks.%s.hooks]] entries, want 1:\n%s", run, got, spec.SarvamEvent, content)
			}
		}
	}
}

func TestHookEntryInstalled(t *testing.T) {
	path := writeConfig(t, "")
	spec := hookSpecs[0]
	command := hookCommand(spec.HookName)

	if hookEntryInstalled(path, spec.SarvamEvent, command) {
		t.Fatal("hooks must not be installed before install-hooks")
	}
	if _, err := upsertHookEntries(path, testEntries(), false); err != nil {
		t.Fatal(err)
	}
	if !hookEntryInstalled(path, spec.SarvamEvent, command) {
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
		{`type = "command" # entire`, "type", "command"},
		{`timeout = 30 # seconds`, "timeout", "30"},
		{`command = "echo # not a comment"`, "command", `echo # not a comment`},
		{`# comment`, "", ""},
	}
	for _, tc := range cases {
		key, value, ok := parseKeyValue(tc.line)
		if key != tc.key || value != tc.value {
			t.Errorf("parseKeyValue(%q) = (%q, %q, %v), want (%q, %q)", tc.line, key, value, ok, tc.key, tc.value)
		}
	}
}
