package vibe

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseHookMapsLifecycleEvents(t *testing.T) {
	tests := []struct {
		hook      string
		fixture   string
		wantType  int
		wantFetch bool
	}{
		{HookNameSessionStart, "session_start", 1, false},
		{HookNamePreAgent, "pre_agent", 2, false},
		{HookNameNotification, "notification", 3, true},
		{HookNameNotification, "notification_failed", 3, true},
		{HookNameSessionEnd, "session_end", 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			isolate(t)
			fetcher := &fakeFetcher{snapshot: loadSnapshot(t)}
			agent := &Agent{Fetcher: fetcher}

			event, err := agent.ParseHook(tt.hook, hookPayloadFixture(t, tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if event == nil {
				t.Fatal("got no event")
			}
			if event.Type != tt.wantType {
				t.Errorf("type = %d, want %d", event.Type, tt.wantType)
			}
			if event.SessionID == "" || event.SessionRef != transcriptPath(event.SessionID) {
				t.Errorf("session id %q, ref %q", event.SessionID, event.SessionRef)
			}
			if got := fetcher.calls > 0; got != tt.wantFetch {
				t.Errorf("fetched = %v, want %v", got, tt.wantFetch)
			}
		})
	}
}

func TestParseHookTurnStartCarriesPromptAndCreatesTranscript(t *testing.T) {
	isolate(t)
	agent := &Agent{Fetcher: &fakeFetcher{err: errFetch}}

	event, err := agent.ParseHook(HookNamePreAgent, hookPayloadFixture(t, "pre_agent"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(event.Prompt, "Create a file named probe-entire.txt") {
		t.Errorf("prompt = %q", event.Prompt)
	}
	// Entire reads a position before the first turn has any history.
	info, err := os.Stat(event.SessionRef)
	if err != nil || info.Size() != 0 {
		t.Fatalf("want an empty transcript at %s, got %v, %v", event.SessionRef, info, err)
	}
}

func TestParseHookTurnEndStoresReplyAndModel(t *testing.T) {
	isolate(t)
	agent := &Agent{Fetcher: &fakeFetcher{snapshot: loadSnapshot(t)}}

	event, err := agent.ParseHook(HookNameNotification, hookPayloadFixture(t, "notification"))
	if err != nil {
		t.Fatal(err)
	}
	if event.Model != "mistral-medium-3.5" {
		t.Errorf("model = %q", event.Model)
	}
	summary, ok, err := agent.ExtractSummary(event.SessionRef)
	if err != nil || !ok || summary != "ZEBRA4412" {
		t.Errorf("summary = %q, %v, %v", summary, ok, err)
	}
}

func TestParseHookTurnFailedReportsError(t *testing.T) {
	isolate(t)
	agent := &Agent{Fetcher: &fakeFetcher{err: errFetch}}

	event, err := agent.ParseHook(HookNameNotification, hookPayloadFixture(t, "notification_failed"))
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != 3 || !strings.Contains(event.Metadata["error"], "turn failed") {
		t.Errorf("event = %+v", event)
	}
}

func TestParseHookIgnoresWhatIsNotALifecycleEvent(t *testing.T) {
	tests := map[string]struct {
		hook    string
		payload string
	}{
		"empty input":   {HookNamePreAgent, ""},
		"no session id": {HookNamePreAgent, `{"prompt":"hi"}`},
		// Vibe never runs pre_agent (match = "main") or session_start for a
		// subagent, so only these two can arrive for one.
		"subagent turn end":    {HookNameNotification, `{"session_id":"child","parent_session_id":"parent","notification_type":"turn_completed"}`},
		"subagent session end": {HookNameSessionEnd, `{"session_id":"child","parent_session_id":"parent","reason":"other"}`},
		"approval requested":   {HookNameNotification, `{"session_id":"s","notification_type":"approval_requested"}`},
		"post_agent":           {"post-agent", `{"session_id":"s"}`},
		"unknown hook":         {"something-else", `{"session_id":"s"}`},
		"question asked":       {HookNameNotification, `{"session_id":"s","notification_type":"question_asked"}`},
		"notification no type": {HookNameNotification, `{"session_id":"s"}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			isolate(t)
			fetcher := &fakeFetcher{snapshot: loadSnapshot(t)}
			event, err := (&Agent{Fetcher: fetcher}).ParseHook(tt.hook, []byte(tt.payload))
			if err != nil || event != nil {
				t.Fatalf("got %+v, %v; want nil, nil", event, err)
			}
			if fetcher.calls != 0 {
				t.Errorf("fetched %d times for an ignored hook", fetcher.calls)
			}
		})
	}
}

// A fork or rewind is a main session whose parent_session_id names the session
// it was forked from (mistralai_vibe_local_harness/vibe/_host.py). It must not
// be mistaken for a subagent.
func TestParseHookTracksForkedSession(t *testing.T) {
	isolate(t)
	fetcher := &fakeFetcher{snapshot: loadSnapshot(t)}
	agent := &Agent{Fetcher: fetcher}
	const fork = `"session_id":"fork-1","parent_session_id":"source-1"`

	steps := []struct {
		hook     string
		payload  string
		wantType int
	}{
		{HookNameSessionStart, `{` + fork + `,"source":"fork","model":"m"}`, 1},
		{HookNamePreAgent, `{` + fork + `,"prompt":"continue"}`, 2},
		{HookNameNotification, `{` + fork + `,"notification_type":"turn_completed"}`, 3},
		{HookNameSessionEnd, `{` + fork + `,"reason":"exit"}`, 5},
	}
	for _, step := range steps {
		event, err := agent.ParseHook(step.hook, []byte(step.payload))
		if err != nil {
			t.Fatal(err)
		}
		if event == nil {
			t.Fatalf("%s for a forked session was dropped", step.hook)
		}
		if event.Type != step.wantType || event.SessionID != "fork-1" {
			t.Errorf("%s: event = %+v", step.hook, event)
		}
	}
	// The fork's existing history is stored before its first turn.
	if position, _ := agent.GetTranscriptPosition(transcriptPath("fork-1")); position == 0 {
		t.Error("forked session started with an empty transcript")
	}
}

func TestParseHookRejectsMalformedPayload(t *testing.T) {
	isolate(t)
	if _, err := (&Agent{}).ParseHook(HookNamePreAgent, []byte("{not json")); err == nil {
		t.Fatal("want an error")
	}
}

func TestParseHookSurvivesFetchFailure(t *testing.T) {
	isolate(t)
	agent := &Agent{Fetcher: &fakeFetcher{err: errFetch}}
	event, err := agent.ParseHook(HookNameNotification, hookPayloadFixture(t, "notification"))
	if err != nil || event == nil || event.Type != 3 {
		t.Fatalf("got %+v, %v", event, err)
	}
}

func TestParseHookKeepsHostileSessionIDInsideSessionDir(t *testing.T) {
	isolate(t)
	agent := &Agent{Fetcher: &fakeFetcher{err: errFetch}}
	event, err := agent.ParseHook(HookNamePreAgent, []byte(`{"session_id":"../../escape","prompt":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := agent.GetSessionDir("")
	if filepath.Dir(event.SessionRef) != dir {
		t.Errorf("session ref %s is outside %s", event.SessionRef, dir)
	}
}

func TestSessionDirIsOutsideEntireTmp(t *testing.T) {
	repo := isolate(t)
	dir, err := (&Agent{}).GetSessionDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Other agents claim <repo>/.entire/tmp as their session directory; a
	// transcript stored there is attributed to them.
	if strings.HasPrefix(dir, repo) {
		t.Errorf("session dir %s is inside the repository %s", dir, repo)
	}
	other, _ := (&Agent{}).GetSessionDir(t.TempDir())
	if other == dir {
		t.Error("two repositories share a session dir")
	}
}

// --- hooks.toml ---

func readHooks(t *testing.T, repo string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, hooksFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInstallHooksWritesEveryHook(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}

	count, err := agent.InstallHooks(false, false)
	if err != nil || count != len(installedHooks) {
		t.Fatalf("count = %d, err = %v", count, err)
	}
	content := readHooks(t, repo)
	for _, want := range []string{
		`type = "session_start"`,
		`type = "pre_agent"`,
		`match = "turn_completed"`,
		`match = "turn_failed"`,
		`type = "session_end"`,
		`entire hooks mistral-vibe notification >/dev/null 2>&1 || true`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("hooks.toml lacks %q", want)
		}
	}
	if strings.Count(content, "[[hooks]]") != len(installedHooks) {
		t.Errorf("want %d [[hooks]] tables:\n%s", len(installedHooks), content)
	}
	if !agent.AreHooksInstalled() {
		t.Error("hooks not reported as installed")
	}
}

func TestInstallHooksIsIdempotent(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	first := readHooks(t, repo)

	count, err := agent.InstallHooks(false, false)
	if err != nil || count != 0 {
		t.Fatalf("second install: count = %d, err = %v", count, err)
	}
	if readHooks(t, repo) != first {
		t.Error("second install changed the file")
	}
	if count, err := agent.InstallHooks(false, true); err != nil || count != len(installedHooks) {
		t.Fatalf("forced install: count = %d, err = %v", count, err)
	}
	if readHooks(t, repo) != first {
		t.Error("forced install changed the content")
	}
}

const userHooks = `# my own hooks
[[hooks]]
name = "audit-bash"
type = "pre_tool"
match = "bash"
command = "uv run python audit.py"
`

func TestInstallAndUninstallPreserveUserHooks(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}
	if err := os.MkdirAll(filepath.Join(repo, vibeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, hooksFile), []byte(userHooks), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	content := readHooks(t, repo)
	if !strings.HasPrefix(content, userHooks) {
		t.Errorf("user hooks were altered:\n%s", content)
	}
	if strings.Count(content, "[[hooks]]") != len(installedHooks)+1 {
		t.Errorf("unexpected table count:\n%s", content)
	}

	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if got := readHooks(t, repo); got != userHooks {
		t.Errorf("after uninstall got:\n%q\nwant:\n%q", got, userHooks)
	}
	if agent.AreHooksInstalled() {
		t.Error("hooks still reported as installed")
	}
}

func TestUninstallRemovesFileAndDirWhenNothingElseIsLeft(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, vibeDir)); !os.IsNotExist(err) {
		t.Errorf(".vibe still exists: %v", err)
	}
	// Uninstalling again is not an error.
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallKeepsOtherFilesInVibeDir(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(repo, vibeDir, "config.toml")
	if err := os.WriteFile(config, []byte("active_model = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config); err != nil {
		t.Errorf("user config was removed: %v", err)
	}
}

func TestInstallHooksRefusesInlineHooksArray(t *testing.T) {
	repo := isolate(t)
	if err := os.MkdirAll(filepath.Join(repo, vibeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "hooks = []\n"
	if err := os.WriteFile(filepath.Join(repo, hooksFile), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Agent{}).InstallHooks(false, false); err == nil {
		t.Fatal("want an error: [[hooks]] cannot follow an inline hooks array")
	}
	if readHooks(t, repo) != original {
		t.Error("file was modified")
	}
}

func TestInstallHooksRefusesSymlink(t *testing.T) {
	repo := isolate(t)
	if err := os.MkdirAll(filepath.Join(repo, vibeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere.toml")
	if err := os.WriteFile(target, []byte(userHooks), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(repo, hooksFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	agent := &Agent{}
	if _, err := agent.InstallHooks(false, true); err == nil {
		t.Fatal("want an error for a symlinked hooks file")
	}
	if data, _ := os.ReadFile(target); string(data) != userHooks {
		t.Error("wrote through the symlink")
	}
	if _, err := agent.HooksInstalledState(); err == nil {
		t.Error("an unreadable hooks file must report an unknown state, not 'not installed'")
	}
}

func TestHooksRefuseSymlinkedVibeDir(t *testing.T) {
	repo := isolate(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "hooks.toml")
	if err := os.WriteFile(victim, []byte(userHooks), 0o644); err != nil {
		t.Fatal(err)
	}
	// A checkout can ship .vibe as a symlink to anywhere the user can write.
	if err := os.Symlink(outside, filepath.Join(repo, vibeDir)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	agent := &Agent{}

	if _, err := agent.InstallHooks(false, true); err == nil {
		t.Error("install wrote through a symlinked .vibe directory")
	}
	if err := agent.UninstallHooks(); err == nil {
		t.Error("uninstall must not act through a symlinked .vibe directory")
	}
	if _, err := agent.HooksInstalledState(); err == nil {
		t.Error("state behind a symlinked .vibe directory is unknown, not 'not installed'")
	}
	if data, _ := os.ReadFile(victim); string(data) != userHooks {
		t.Errorf("file outside the repository was changed:\n%s", data)
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 {
		t.Errorf("files were created outside the repository: %v", entries)
	}
}

func TestHooksPreserveExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	repo := isolate(t)
	agent := &Agent{}
	if err := os.MkdirAll(filepath.Join(repo, vibeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, hooksFile)
	if err := os.WriteFile(path, []byte(userHooks), 0o600); err != nil {
		t.Fatal(err)
	}
	mode := func() os.FileMode {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}

	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	if got := mode(); got != 0o600 {
		t.Errorf("install changed a private hooks file to %o", got)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if got := mode(); got != 0o600 {
		t.Errorf("uninstall changed a private hooks file to %o", got)
	}
}

func TestInstallHooksRefusesQuotedInlineHooksKey(t *testing.T) {
	for _, original := range []string{"\"hooks\" = []\n", "'hooks' = []\n", "  hooks=[ ]\n"} {
		repo := isolate(t)
		if err := os.MkdirAll(filepath.Join(repo, vibeDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, hooksFile), []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := (&Agent{}).InstallHooks(false, false); err == nil {
			t.Errorf("%q: appending [[hooks]] would make the file invalid TOML", original)
		}
		if readHooks(t, repo) != original {
			t.Errorf("%q: file was modified", original)
		}
	}
}

// Marker text inside a user's own string is not Entire's block.
func TestMarkersMustBeWholeLines(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}
	mine := "[[hooks]]\nname = \"mine\"\ntype = \"pre_tool\"\ncommand = \"true\"\n" +
		"description = \"see " + blockStart + " and " + blockEnd + " in the docs\"\n"
	if err := os.MkdirAll(filepath.Join(repo, vibeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, hooksFile), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("marker text inside a string counted as an installed block")
	}
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	content := readHooks(t, repo)
	if !strings.HasPrefix(content, mine) {
		t.Errorf("user hook was rewritten:\n%s", content)
	}
	if strings.Count(content, "[[hooks]]") != len(installedHooks)+1 || !agent.AreHooksInstalled() {
		t.Errorf("hooks were not installed:\n%s", content)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if got := readHooks(t, repo); got != mine {
		t.Errorf("after uninstall:\n%q\nwant:\n%q", got, mine)
	}
}

// A start marker whose end marker was deleted must not swallow the user's
// hooks once a complete block is appended after them.
func TestUnterminatedStartMarkerDoesNotSwallowUserHooks(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}
	damaged := blockStart + "\n" + userHooks
	if err := os.MkdirAll(filepath.Join(repo, vibeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, hooksFile), []byte(damaged), 0o644); err != nil {
		t.Fatal(err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("a lone start marker counted as installed")
	}

	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	// A second install must find its own block, not one stretching back to
	// the stray marker.
	if count, err := agent.InstallHooks(false, false); err != nil || count != 0 {
		t.Fatalf("second install: count = %d, err = %v", count, err)
	}
	if count, err := agent.InstallHooks(false, true); err != nil || count != len(installedHooks) {
		t.Fatalf("forced install: count = %d, err = %v", count, err)
	}
	content := readHooks(t, repo)
	if !strings.HasPrefix(content, damaged) || strings.Count(content, "[[hooks]]") != len(installedHooks)+1 {
		t.Fatalf("user hooks were lost on reinstall:\n%s", content)
	}

	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if got := readHooks(t, repo); got != damaged {
		t.Errorf("after uninstall:\n%q\nwant:\n%q", got, damaged)
	}
}

func TestHooksRoundTripCRLFFile(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}
	crlf := strings.ReplaceAll(userHooks, "\n", "\r\n")
	if err := os.MkdirAll(filepath.Join(repo, vibeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, hooksFile), []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	if !agent.AreHooksInstalled() {
		t.Fatal("hooks not detected in a CRLF file")
	}
	if count, err := agent.InstallHooks(false, false); err != nil || count != 0 {
		t.Errorf("second install into a CRLF file: count = %d, err = %v", count, err)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if got := readHooks(t, repo); got != crlf {
		t.Errorf("after uninstall:\n%q\nwant:\n%q", got, crlf)
	}
}

func TestAreHooksInstalledNeedsEveryHook(t *testing.T) {
	repo := isolate(t)
	agent := &Agent{}
	if agent.AreHooksInstalled() {
		t.Fatal("installed before install")
	}
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	partial := strings.Replace(readHooks(t, repo), `name = "entire-session-end"`, `name = "renamed"`, 1)
	if err := os.WriteFile(filepath.Join(repo, hooksFile), []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	if agent.AreHooksInstalled() {
		t.Error("a block missing a hook counts as installed")
	}
	// Installing again repairs it.
	if count, err := agent.InstallHooks(false, false); err != nil || count != len(installedHooks) {
		t.Fatalf("repair: count = %d, err = %v", count, err)
	}
	if !agent.AreHooksInstalled() {
		t.Error("repair did not restore the hooks")
	}
}

func TestHookCommandNeverPrintsOrFails(t *testing.T) {
	// Vibe treats non-JSON stdout or a non-zero exit as a hook failure.
	command := hookCommand(HookNameSessionStart)
	for _, want := range []string{"command -v entire", ">/dev/null 2>&1 || true"} {
		if !strings.Contains(command, want) {
			t.Errorf("command %q lacks %q", command, want)
		}
	}
	if strings.ContainsAny(command, "\"\\\n") {
		t.Errorf("command %q needs TOML escaping", command)
	}
}
