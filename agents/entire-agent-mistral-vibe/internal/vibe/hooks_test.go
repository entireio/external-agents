package vibe

import (
	"os"
	"path/filepath"
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
		"empty input":          {HookNamePreAgent, ""},
		"no session id":        {HookNamePreAgent, `{"prompt":"hi"}`},
		"subagent turn":        {HookNamePreAgent, `{"session_id":"child","parent_session_id":"parent","prompt":"hi"}`},
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
