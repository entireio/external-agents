package amp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

func TestInfo(t *testing.T) {
	agent := New()
	info := agent.Info()
	if info.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("ProtocolVersion = %d, want %d", info.ProtocolVersion, protocol.ProtocolVersion)
	}
	if info.Name != "amp" {
		t.Fatalf("Name = %q, want amp", info.Name)
	}
	if !info.Capabilities.Hooks || !info.Capabilities.TranscriptAnalyzer || !info.Capabilities.CompactTranscript {
		t.Fatalf("unexpected capabilities: %+v", info.Capabilities)
	}
	if !info.Capabilities.TranscriptPreparer {
		t.Fatalf("expected transcript_preparer capability")
	}
	if !info.Capabilities.TokenCalculator {
		t.Fatalf("expected token_calculator capability")
	}
}

func TestParseHookSessionStart(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	runner := &fakeCommandRunner{}
	agent := &Agent{CommandRunner: runner}
	event, err := agent.ParseHook("session.start", []byte(`{"type":"session.start","thread_id":"T-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.Type != 1 || event.SessionID != "T-123" {
		t.Fatalf("event = %+v", event)
	}
	if event.Model != "claude" {
		t.Fatalf("Model = %q, want claude", event.Model)
	}
	assertExportedThread(t, runner, root)
}

func TestParseHookAgentStartFallbackExports(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	runner := &fakeCommandRunner{}
	agent := &Agent{CommandRunner: runner}
	event, err := agent.ParseHook("agent.start", []byte(`{"type":"agent.start","thread_id":"T-123","message":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.Type != 2 || event.Prompt != "hello" {
		t.Fatalf("event = %+v", event)
	}
	if event.Model != "claude" {
		t.Fatalf("Model = %q, want claude", event.Model)
	}
	assertExportedThread(t, runner, root)
}

func TestParseHookAgentStartSkipsExportWhenPrepared(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	path := filepath.Join(root, ".entire", "tmp", "amp", "T-123.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(testPreparedTranscriptJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	agent := &Agent{CommandRunner: runner}
	event, err := agent.ParseHook("agent.start", []byte(`{"type":"agent.start","thread_id":"T-123","message":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.Model != "claude" || event.Prompt != "hello" {
		t.Fatalf("event = %+v", event)
	}
	if runner.threadID != "" {
		t.Fatalf("expected no export, got threadID = %q", runner.threadID)
	}
}

func TestParseHookAgentEndWritesTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	runner := &fakeCommandRunner{}
	agent := &Agent{CommandRunner: runner}
	payload := `{"type":"agent.end","thread_id":"T-123","status":"done","messages":[{"role":"assistant","id":"a1","content":[{"type":"text","text":"done"}]}],"modified_files":["hello.txt"]}`
	event, err := agent.ParseHook("agent.end", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.Type != 3 || event.SessionID != "T-123" || event.SessionRef == "" {
		t.Fatalf("event = %+v", event)
	}
	if event.Model != "claude" {
		t.Fatalf("Model = %q, want claude", event.Model)
	}
	assertExportedThread(t, runner, root)
}

func TestParseHookUsesSessionRefExportedThreadModel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	path := filepath.Join(root, ".entire", "tmp", "amp", "T-123.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(testPreparedTranscriptJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	agent := &Agent{CommandRunner: runner}
	event, err := agent.ParseHook("session.start", []byte(`{"type":"session.start","thread_id":"T-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.Model != "claude" {
		t.Fatalf("event = %+v", event)
	}
	if runner.threadID != "T-123" {
		t.Fatalf("export threadID = %q, want T-123", runner.threadID)
	}
}

func assertExportedThread(t *testing.T, runner *fakeCommandRunner, root string) {
	t.Helper()
	if runner.threadID != "T-123" {
		t.Fatalf("threadID = %q, want T-123", runner.threadID)
	}
	wantPath := filepath.Join(root, ".entire", "tmp", "amp", "T-123.jsonl")
	if runner.outputPath != wantPath {
		t.Fatalf("outputPath = %q, want %q", runner.outputPath, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != testPreparedTranscriptJSON {
		t.Fatalf("exported data = %s", data)
	}
}

func TestGeneratePlugin(t *testing.T) {
	plugin := generatePlugin()
	for _, snippet := range []string{
		`PluginAPI`,
		`from "@ampcode/plugin"`,
		`amp.on("agent.start"`,
		`amp.on("agent.end"`,
		`["hooks", "amp", hookName]`,
		`toolCallsInMessages`,
		`amp.logger.log`,
		`maxBuffer: Infinity`,
	} {
		if !strings.Contains(plugin, snippet) {
			t.Fatalf("plugin missing %q\n%s", snippet, plugin)
		}
	}
}

func TestInstallAndUninstallHooks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", root)
	agent := New()
	if agent.AreHooksInstalled() {
		t.Fatal("hooks should not be installed")
	}
	count, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if !agent.AreHooksInstalled() {
		t.Fatal("hooks should be installed")
	}
	count, err = agent.InstallHooks(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("idempotent count = %d, want 0", count)
	}
	if err := agent.UninstallHooks(); err != nil {
		t.Fatal(err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("hooks should not be installed")
	}
}

func TestSafeSessionID(t *testing.T) {
	if got := safeSessionID("T/foo:bar"); got != "T_foo_bar" {
		t.Fatalf("safeSessionID = %q", got)
	}
}
