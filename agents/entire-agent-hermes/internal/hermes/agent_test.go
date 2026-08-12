package hermes

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-hermes/internal/protocol"
	"gopkg.in/yaml.v3"
)

func TestInstallPreservesPluginsAndMultipleRepositories(t *testing.T) {
	home := t.TempDir()
	repoOne := t.TempDir()
	repoTwo := t.TempDir()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "entire"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERMES_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repoOne)

	config := `theme: midnight
plugins:
  enabled:
    - existing-observer
  disabled:
    - entire-observer
    - keep-disabled
  existing-observer:
    option: true
`
	writeFixture(t, filepath.Join(home, "config.yaml"), []byte(config), 0o600)
	writeFixture(t, filepath.Join(home, "plugins", "unrelated", "keep.txt"), []byte("keep\n"), 0o600)

	agent := New()
	count, err := agent.InstallHooks(false, false)
	if err != nil {
		t.Fatalf("InstallHooks(repo one): %v", err)
	}
	if count != len(observerHooks) {
		t.Fatalf("hook count: got %d, want %d", count, len(observerHooks))
	}
	if !agent.AreHooksInstalled() {
		t.Fatal("hooks should be installed for repo one")
	}
	assertConfigState(t, home, []string{"existing-observer", pluginName}, []string{"keep-disabled"})

	t.Setenv("ENTIRE_REPO_ROOT", repoTwo)
	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatalf("InstallHooks(repo two): %v", err)
	}
	registry, err := loadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Repositories) != 2 {
		t.Fatalf("registered repositories: got %d, want 2", len(registry.Repositories))
	}

	t.Setenv("ENTIRE_REPO_ROOT", repoOne)
	if err := agent.UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks(repo one): %v", err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("repo one should no longer be registered")
	}
	t.Setenv("ENTIRE_REPO_ROOT", repoTwo)
	if !agent.AreHooksInstalled() {
		t.Fatal("repo two registration should remain installed")
	}

	writeFixture(t, filepath.Join(pluginDir(home), "user-note.txt"), []byte("preserve\n"), 0o600)
	if err := agent.UninstallHooks(); err != nil {
		t.Fatalf("UninstallHooks(repo two): %v", err)
	}
	if agent.AreHooksInstalled() {
		t.Fatal("repo two should no longer be registered")
	}
	assertConfigState(t, home, []string{"existing-observer"}, []string{"keep-disabled"})
	for _, path := range []string{
		filepath.Join(home, "plugins", "unrelated", "keep.txt"),
		filepath.Join(pluginDir(home), "user-note.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("preserved file %s: %v", path, err)
		}
	}
	for _, name := range []string{"plugin.yaml", "__init__.py", "repositories.json"} {
		if _, err := os.Stat(filepath.Join(pluginDir(home), name)); !os.IsNotExist(err) {
			t.Fatalf("owned file %s should be removed, stat error %v", name, err)
		}
	}
}

func TestInstallRequiresExplicitHermesHome(t *testing.T) {
	t.Setenv("HERMES_HOME", "")
	if _, err := New().InstallHooks(false, false); err == nil || !strings.Contains(err.Error(), "HERMES_HOME") {
		t.Fatalf("InstallHooks error: %v", err)
	}
	if New().AreHooksInstalled() {
		t.Fatal("AreHooksInstalled must be false without HERMES_HOME")
	}
}

func TestResolveSessionFilePreservesSafeSessionID(t *testing.T) {
	dir := t.TempDir()
	path := New().ResolveSessionFile(dir, "test-session-123")
	if filepath.Dir(path) != dir {
		t.Fatalf("session path escaped directory: %s", path)
	}
	if !strings.Contains(filepath.Base(path), "test-session-123") {
		t.Fatalf("session path does not preserve session ID: %s", path)
	}
	if filepath.Ext(path) != ".jsonl" {
		t.Fatalf("unexpected session extension: %s", path)
	}
}

func TestResolveSessionFileSafelyDistinguishesUnsafeSessionIDs(t *testing.T) {
	dir := t.TempDir()
	agent := New()
	first := agent.ResolveSessionFile(dir, "../session/a")
	second := agent.ResolveSessionFile(dir, "../session?a")
	if first == second {
		t.Fatalf("distinct session IDs collided: %s", first)
	}
	for _, path := range []string{first, second} {
		if filepath.Dir(path) != dir || filepath.Ext(path) != ".jsonl" {
			t.Fatalf("unsafe session path escaped directory: %s", path)
		}
		if strings.Contains(filepath.Base(path), "..") || strings.ContainsAny(filepath.Base(path), `/\\`) {
			t.Fatalf("unsafe session path retained traversal characters: %s", path)
		}
	}
}

func TestWriteAndReadSessionPreservesOpaqueNativeData(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	agent := New()
	dir, err := agent.GetSessionDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	ref := agent.ResolveSessionFile(dir, "test-roundtrip-session")
	want := []byte(`{"test": true}`)
	if err := agent.WriteSession(protocol.AgentSessionJSON{SessionRef: ref, NativeData: want}); err != nil {
		t.Fatal(err)
	}
	got, err := agent.ReadSession(&protocol.HookInputJSON{SessionID: "test-roundtrip-session", SessionRef: ref})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.NativeData, want) {
		t.Fatalf("native data changed: got %q, want %q", got.NativeData, want)
	}
	if got.ModifiedFiles == nil || got.NewFiles == nil || got.DeletedFiles == nil {
		t.Fatal("session file lists must be initialized")
	}
}

func TestObserverSessionStateUsesFullSessionID(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	home := t.TempDir()
	repo := t.TempDir()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "entire"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERMES_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	script := `
import importlib.util, os
from pathlib import Path
path = Path(os.environ["HERMES_HOME"]) / "plugins" / "entire-observer" / "__init__.py"
spec = importlib.util.spec_from_file_location("entire_observer_session_key_test", path, submodule_search_locations=[str(path.parent)])
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
assert module._session_key("a" * 256 + "x") != module._session_key("a" * 256 + "y")
seen = {}
def fake_redactor(text, **kwargs):
    seen.update(kwargs)
    return "«redacted:credential-…»"
module._hermes_redact_sensitive_text = fake_redactor
assert module._sanitize_text("https://user:pass@example.test") == "[REDACTED]"
assert seen == {"force": True, "redact_url_credentials": True}
`
	cmd := exec.Command(python, "-c", script)
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("observer session key fixture: %v\n%s", err, output)
	}
}

func TestInstallRefusesNonOwnedPluginCollision(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "entire"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERMES_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	collision := filepath.Join(pluginDir(home), "__init__.py")
	original := []byte("# unrelated user plugin\n")
	writeFixture(t, collision, original, 0o600)
	if _, err := New().InstallHooks(false, true); err == nil || !strings.Contains(err.Error(), "non-owned") {
		t.Fatalf("InstallHooks collision error: %v", err)
	}
	if got := mustReadFile(t, collision); got != string(original) {
		t.Fatalf("collision was overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(pluginDir(home), "plugin.yaml")); !os.IsNotExist(err) {
		t.Fatalf("partial plugin installation should not occur: %v", err)
	}
}

func TestObserverModifiedFilesKeepsRenameDestination(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	home := t.TempDir()
	repo := t.TempDir()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "entire"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERMES_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}

	writeFixture(t, filepath.Join(repo, "old.txt"), []byte("tracked\n"), 0o600)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "Entire Hermes Test"},
		{"config", "user.email", "hermes-test@example.invalid"},
		{"add", "old.txt"},
		{"commit", "-qm", "fixture"},
		{"mv", "old.txt", "new.txt"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}

	script := `
import importlib.util, json, os
from pathlib import Path
home = Path(os.environ["HERMES_HOME"])
path = home / "plugins" / "entire-observer" / "__init__.py"
spec = importlib.util.spec_from_file_location("entire_observer_rename_test", path, submodule_search_locations=[str(path.parent)])
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
print(json.dumps(module._modified_files(Path(os.environ["ENTIRE_REPO_ROOT"]))))
`
	cmd := exec.Command(python, "-c", script)
	cmd.Dir = repo
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("observer rename fixture: %v\n%s", err, output)
	}
	var files []string
	if err := json.Unmarshal(output, &files); err != nil {
		t.Fatalf("decode modified files: %v\n%s", err, output)
	}
	if !slices.Equal(files, []string{"new.txt"}) {
		t.Fatalf("rename modified files: got %v, want [new.txt]", files)
	}
}

func TestTranscriptAnalysisSanitizesAndCompacts(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	path := observerSessionPath(t, repo, "analysis")
	data := strings.Join([]string{
		`{"v":1,"type":"session_start","timestamp":"2026-08-06T12:00:00Z","model":"fixture model"}`,
		`{"v":1,"type":"user","timestamp":"2026-08-06T12:00:01Z","content":"Ship it with password=hunter2 and token sk-proj-AbCdEfGhIjKlMnOpQrStUv"}`,
		`{"v":1,"type":"tool","timestamp":"2026-08-06T12:00:02Z","name":"write_file","status":"ok","modified_files":["README.md","../escape",".git/config",".env","config/access-token.txt","cmd/main.go","README.md"],"result":"raw-tool-result"}`,
		`{"v":1,"type":"assistant","timestamp":"2026-08-06T12:00:03Z","content":"Done; authorization=super-secret"}`,
	}, "\n") + "\n"
	writeFixture(t, path, []byte(data), 0o600)

	agent := New()
	files, position, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files, []string{"README.md", "cmd/main.go"}) {
		t.Fatalf("modified files: %v", files)
	}
	if position != 4 {
		t.Fatalf("position: got %d, want 4 transcript lines", position)
	}
	files, position, err = agent.ExtractModifiedFiles(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || position != 4 {
		t.Fatalf("line-offset files=%v position=%d, want no files at position 4", files, position)
	}
	prompts, err := agent.ExtractPrompts(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || strings.Contains(prompts[0], "hunter2") || strings.Contains(prompts[0], "sk-proj-") || !strings.Contains(prompts[0], "[REDACTED]") {
		t.Fatalf("sanitized prompt: %q", prompts)
	}
	summary, ok, err := agent.ExtractSummary(path)
	if err != nil || !ok {
		t.Fatalf("ExtractSummary: summary=%q ok=%v err=%v", summary, ok, err)
	}
	if strings.Contains(summary, "super-secret") || !strings.Contains(summary, "[REDACTED]") {
		t.Fatalf("sanitized summary: %q", summary)
	}

	session, err := agent.ReadSession(&protocol.HookInputJSON{SessionID: "analysis", SessionRef: path})
	if err != nil {
		t.Fatal(err)
	}
	native := string(session.NativeData)
	for _, forbidden := range []string{"hunter2", "super-secret", "raw-tool-result", "../escape", ".git/config", ".env", "access-token.txt", `"result"`} {
		if strings.Contains(native, forbidden) {
			t.Fatalf("read-session native data contains %q: %s", forbidden, native)
		}
	}
	if !strings.Contains(native, `"modified_files":["README.md","cmd/main.go"]`) || !strings.Contains(native, "[REDACTED]") {
		t.Fatalf("read-session native data was not sanitized: %s", native)
	}
	assertEntireParserCompatibleTranscript(t, session.NativeData)
	transcriptData, err := agent.ReadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(transcriptData) != string(session.NativeData) {
		t.Fatalf("read-transcript did not return the sanitized portable transcript:\n%s", transcriptData)
	}

	compact, err := agent.CompactTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(compact.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	compactText := string(decoded)
	for _, forbidden := range []string{"hunter2", "super-secret", "sk-proj-", "raw-tool-result", "../escape", ".git/config", ".env", "access-token.txt"} {
		if strings.Contains(compactText, forbidden) {
			t.Fatalf("compact transcript contains %q: %s", forbidden, compactText)
		}
	}
	if !strings.Contains(compactText, `"modified_files":["README.md","cmd/main.go"]`) {
		t.Fatalf("compact transcript lacks safe file metadata: %s", compactText)
	}
}

func assertEntireParserCompatibleTranscript(t *testing.T, data []byte) {
	t.Helper()
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var raw struct {
			Type       string          `json:"type"`
			HermesType string          `json:"hermes_type"`
			Content    json.RawMessage `json:"content"`
			Message    *struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("portable transcript line is invalid JSON: %v\n%s", err, line)
		}
		if len(raw.Content) != 0 {
			t.Fatalf("portable user/assistant content must use the message envelope: %s", line)
		}
		switch raw.Type {
		case "user":
			if raw.Message == nil {
				t.Fatalf("portable user line lacks message envelope: %s", line)
			}
			var text string
			if err := json.Unmarshal(raw.Message.Content, &text); err != nil || text == "" {
				t.Fatalf("portable user content is not parser-readable text: %s", line)
			}
			seen["user"] = true
		case "assistant":
			if raw.Message == nil {
				t.Fatalf("portable assistant line lacks message envelope: %s", line)
			}
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw.Message.Content, &blocks); err != nil || len(blocks) == 0 {
				t.Fatalf("portable assistant content is not a parser-readable block array: %s", line)
			}
			if raw.HermesType == "tool" {
				if blocks[0].Type != "tool_use" || blocks[0].Name == "" {
					t.Fatalf("portable tool line lacks a tool_use block: %s", line)
				}
				seen["tool"] = true
			} else {
				if blocks[0].Type != "text" || blocks[0].Text == "" {
					t.Fatalf("portable assistant line lacks a text block: %s", line)
				}
				seen["assistant"] = true
			}
		}
	}
	for _, kind := range []string{"user", "tool", "assistant"} {
		if !seen[kind] {
			t.Fatalf("portable transcript lacks %s content: %s", kind, data)
		}
	}
}

func TestParseHookUsesAllowlistedFields(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	input := []byte(`{
  "session_id":"session-1",
  "user_prompt":"Use authorization=top-secret",
  "model":"model with spaces",
  "conversation_history":[{"role":"system","content":"do not capture"}],
  "platform":"platform-id"
}`)
	event, err := New().ParseHook("pre_llm_call", input)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.Type != 2 || event.SessionID != "session-1" {
		t.Fatalf("event: %#v", event)
	}
	if strings.Contains(event.Prompt, "top-secret") || !strings.Contains(event.Prompt, "[REDACTED]") {
		t.Fatalf("prompt: %q", event.Prompt)
	}
	if event.Model != "model_with_spaces" || event.Metadata != nil {
		t.Fatalf("allowlisted event fields: %#v", event)
	}
	if !strings.HasPrefix(event.SessionRef, filepath.Join(home, "entire", "transcripts")) {
		t.Fatalf("session ref outside observer root: %s", event.SessionRef)
	}
	unknown, err := New().ParseHook("post_tool_call", input)
	if err != nil || unknown != nil {
		t.Fatalf("observer-only hook should parse as null: event=%#v err=%v", unknown, err)
	}
}

func TestObserverResolvesGatewayAbsoluteWriteAndForwardsLifecycleBeforeMutation(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	home := t.TempDir()
	repo := t.TempDir()
	gatewayDir := t.TempDir()
	binDir := t.TempDir()
	mutationPath := filepath.Join(repo, "created.txt")
	eventLog := filepath.Join(t.TempDir(), "events.log")
	fakeEntire := `#!/bin/sh
if [ -e "$FAKE_MUTATION_PATH" ]; then
  phase=late
else
  phase=before
fi
printf '%s\t%s\t%s' "$PWD" "$phase" "$*" >> "$FAKE_EVENT_LOG"
if [ "$1" = "hooks" ]; then
  printf '\t' >> "$FAKE_EVENT_LOG"
  cat >> "$FAKE_EVENT_LOG"
fi
printf '\n' >> "$FAKE_EVENT_LOG"
exit 0
`
	writeExecutable(t, filepath.Join(binDir, "entire"), fakeEntire)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERMES_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("FAKE_MUTATION_PATH", mutationPath)
	t.Setenv("FAKE_EVENT_LOG", eventLog)

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	script := `
import importlib.util
import os
from pathlib import Path

home = Path(os.environ["HERMES_HOME"])
repo = Path(os.environ["ENTIRE_REPO_ROOT"])
gateway = Path(os.environ["FAKE_GATEWAY_DIR"])
path = home / "plugins" / "entire-observer" / "__init__.py"
spec = importlib.util.spec_from_file_location("entire_observer_test", path, submodule_search_locations=[str(path.parent)])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
hooks = {}
class Context:
    def register_hook(self, name, callback): hooks[name] = callback
module.register(Context())
os.chdir(repo)
hooks["on_session_start"](session_id="session", model="model one", platform="platform-id")
hooks["pre_llm_call"](session_id="session", user_message="password=hunter2", model="model one", conversation_history=[{"role":"system","content":"history-secret"}], platform="platform-id")
assert not list((home / "entire" / "transcripts").glob("**/*.jsonl"))
# A new empty turn must clear the prior buffered prompt before repository projection.
hooks["pre_llm_call"](session_id="session", user_message=None, model="model one")
# A pathless tool must not use the registered process CWD as repository evidence.
hooks["pre_tool_call"](session_id="session", tool_name="web_search", args={"query": "unrelated"})
hooks["post_tool_call"](session_id="session", tool_name="web_search", args={"query": "unrelated"}, result="ignored")
assert not list((home / "entire" / "transcripts").glob("**/*.jsonl"))
os.chdir(gateway)
hooks["pre_tool_call"](session_id="session", tool_name="write_file", args={"path": str(repo / "created.txt"), "content":"tool-arg-secret"})
(repo / "created.txt").write_text("created\n", encoding="utf-8")
hooks["post_tool_call"](session_id="session", tool_name="write_file", args={"path": str(repo / "created.txt"), "content":"tool-arg-secret"}, result="raw-result-secret", status="ok")
`
	t.Setenv("FAKE_GATEWAY_DIR", gatewayDir)
	cmd := exec.Command(python, "-c", script)
	cmd.Dir = gatewayDir
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("observer fixture: %v\n%s", err, output)
	}
	log, err := os.ReadFile(eventLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "	before	hooks hermes on_session_start	") ||
		!strings.Contains(lines[1], "	before	hooks hermes pre_llm_call	") {
		t.Fatalf("session/turn lifecycle ordering before mutation: %q", log)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, repo+"\t") {
			t.Fatalf("forwarded outside resolved repository: %q", line)
		}
	}
	transcript, err := os.ReadFile(observerSessionPath(t, repo, "session"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(transcript)
	for _, forbidden := range []string{"hunter2", "tool-arg-secret", "raw-result-secret", "history-secret", "platform-id"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("observer captured forbidden value %q: %s", forbidden, text)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		for key := range entry {
			if !slices.Contains([]string{"v", "type", "timestamp", "content", "model", "name", "status", "modified_files"}, key) {
				t.Fatalf("forbidden observer field %q in %s", key, line)
			}
		}
	}
}

func TestObserverProjectsOneTurnToTwoRepositories(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	home := t.TempDir()
	repoOne := t.TempDir()
	repoTwo := t.TempDir()
	gatewayDir := t.TempDir()
	binDir := t.TempDir()
	eventLog := filepath.Join(t.TempDir(), "events.log")
	writeExecutable(t, filepath.Join(binDir, "entire"), `#!/bin/sh
printf '%s\t%s' "$PWD" "$*" >> "$FAKE_EVENT_LOG"
if [ "$1" = "hooks" ]; then printf '\t' >> "$FAKE_EVENT_LOG"; cat >> "$FAKE_EVENT_LOG"; fi
printf '\n' >> "$FAKE_EVENT_LOG"
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERMES_HOME", home)
	t.Setenv("FAKE_EVENT_LOG", eventLog)
	for _, repo := range []string{repoOne, repoTwo} {
		t.Setenv("ENTIRE_REPO_ROOT", repo)
		if _, err := New().InstallHooks(false, false); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "init", "-q")
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, output)
		}
	}
	t.Setenv("REPO_ONE", repoOne)
	t.Setenv("REPO_TWO", repoTwo)
	t.Setenv("FAKE_GATEWAY_DIR", gatewayDir)
	script := `
import importlib.util
import os
from pathlib import Path
home = Path(os.environ["HERMES_HOME"])
r1, r2 = Path(os.environ["REPO_ONE"]), Path(os.environ["REPO_TWO"])
path = home / "plugins" / "entire-observer" / "__init__.py"
spec = importlib.util.spec_from_file_location("entire_observer_test", path, submodule_search_locations=[str(path.parent)])
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
hooks = {}
class Context:
    def register_hook(self, name, callback): hooks[name] = callback
module.register(Context())
os.chdir(os.environ["FAKE_GATEWAY_DIR"])
hooks["on_session_start"](session_id="shared", model="safe-model")
hooks["pre_llm_call"](session_id="shared", user_message="touch both", model="safe-model")
hooks["pre_tool_call"](session_id="shared", tool_name="write_file", args={"path": str(r1 / "one.txt"), "content": "repo-one-secret"})
(r1 / "one.txt").write_text("one\n", encoding="utf-8")
hooks["post_tool_call"](session_id="shared", tool_name="write_file", args={"path": str(r1 / "one.txt"), "content": "repo-one-secret"}, result="one-result")
hooks["pre_tool_call"](session_id="shared", tool_name="patch", args={"path": str(r2 / "two.txt"), "old_string": "repo-two-secret"})
(r2 / "two.txt").write_text("two\n", encoding="utf-8")
hooks["post_tool_call"](session_id="shared", tool_name="patch", args={"path": str(r2 / "two.txt"), "old_string": "repo-two-secret"}, result="two-result")
hooks["post_llm_call"](session_id="shared", assistant_response="finished both", model="safe-model", conversation_history=[{"content":"history-secret"}])
hooks["on_session_end"](session_id="shared")
hooks["pre_llm_call"](session_id="shared", user_message="tool-free follow-up", model="safe-model")
hooks["post_llm_call"](session_id="shared", assistant_response="tool-free answer", model="safe-model")
hooks["on_session_end"](session_id="shared")
hooks["pre_llm_call"](session_id="shared", user_message="unrelated pathless prompt", model="safe-model")
hooks["pre_tool_call"](session_id="shared", tool_name="web_search", args={"query": "unrelated"})
hooks["post_tool_call"](session_id="shared", tool_name="web_search", args={"query": "unrelated"}, result="unrelated pathless result")
hooks["post_llm_call"](session_id="shared", assistant_response="unrelated pathless answer", model="safe-model")
hooks["on_session_end"](session_id="shared")
hooks["pre_llm_call"](session_id="shared", user_message="post-pathless tool-free prompt", model="safe-model")
hooks["post_llm_call"](session_id="shared", assistant_response="post-pathless tool-free answer", model="safe-model")
hooks["on_session_end"](session_id="shared")
hooks["on_session_finalize"](session_id="shared", platform="platform-id")
`
	cmd := exec.Command(python, "-c", script)
	cmd.Dir = gatewayDir
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("observer fixture: %v\n%s", err, output)
	}

	one := mustReadFile(t, observerSessionPath(t, repoOne, "shared"))
	two := mustReadFile(t, observerSessionPath(t, repoTwo, "shared"))
	for label, text := range map[string]string{"repo one": one, "repo two": two} {
		for _, required := range []string{"touch both", "finished both", "tool-free follow-up", "tool-free answer", `"type":"turn_end"`, `"type":"session_end"`} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s transcript lacks %q: %s", label, required, text)
			}
		}
		for _, forbidden := range []string{"repo-one-secret", "repo-two-secret", "one-result", "two-result", "history-secret", "platform-id", "unrelated pathless", "post-pathless tool-free", repoOne, repoTwo} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s transcript contains forbidden data %q: %s", label, forbidden, text)
			}
		}
	}
	if !strings.Contains(one, `"modified_files":["one.txt"]`) || strings.Contains(one, "two.txt") || strings.Contains(one, `"name":"patch"`) {
		t.Fatalf("repo one contains cross-repo evidence: %s", one)
	}
	if !strings.Contains(two, `"modified_files":["two.txt"]`) || strings.Contains(two, "one.txt") || strings.Contains(two, `"name":"write_file"`) {
		t.Fatalf("repo two contains cross-repo evidence: %s", two)
	}

	log := mustReadFile(t, eventLog)
	for _, repo := range []string{repoOne, repoTwo} {
		for _, hook := range []string{"on_session_start", "pre_llm_call", "on_session_end", "on_session_finalize"} {
			if !strings.Contains(log, repo+"\thooks hermes "+hook+"\t") {
				t.Fatalf("missing %s projection for %s: %s", hook, repo, log)
			}
		}
	}
}

func TestObserverRejectsTraversalSensitiveAndUsesLongestRegisteredRepo(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	home := t.TempDir()
	outer := t.TempDir()
	nested := filepath.Join(outer, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	gatewayDir := t.TempDir()
	outside := t.TempDir()
	binDir := t.TempDir()
	eventLog := filepath.Join(t.TempDir(), "events.log")
	writeExecutable(t, filepath.Join(binDir, "entire"), "#!/bin/sh\nprintf '%s\\t%s\\n' \"$PWD\" \"$*\" >> \"$FAKE_EVENT_LOG\"\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERMES_HOME", home)
	t.Setenv("FAKE_EVENT_LOG", eventLog)
	for _, repo := range []string{outer, nested} {
		t.Setenv("ENTIRE_REPO_ROOT", repo)
		if _, err := New().InstallHooks(false, false); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("OUTER_REPO", outer)
	t.Setenv("NESTED_REPO", nested)
	t.Setenv("OUTSIDE_DIR", outside)
	t.Setenv("FAKE_GATEWAY_DIR", gatewayDir)
	script := `
import importlib.util
import os
from pathlib import Path
home = Path(os.environ["HERMES_HOME"]); outer = Path(os.environ["OUTER_REPO"]); nested = Path(os.environ["NESTED_REPO"]); outside = Path(os.environ["OUTSIDE_DIR"])
path = home / "plugins" / "entire-observer" / "__init__.py"
spec = importlib.util.spec_from_file_location("entire_observer_test", path, submodule_search_locations=[str(path.parent)])
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
hooks = {}
class Context:
    def register_hook(self, name, callback): hooks[name] = callback
module.register(Context()); os.chdir(os.environ["FAKE_GATEWAY_DIR"])
resolved = module._repositories({"path": str(nested / "inside.txt"), "content": {"path": str(outer / "wrong.txt")}})
assert len(resolved) == 1 and resolved[0][1] == nested
hooks["pre_llm_call"](session_id="reject", user_message="buffered")
hooks["pre_tool_call"](session_id="reject", tool_name="write_file", args={"cwd": str(outer), "path": "../" + outside.name + "/escape.txt"})
hooks["pre_tool_call"](session_id="reject", tool_name="write_file", args={"workdir": str(outer), "path": ".env"})
link = outer / "link"; link.symlink_to(outside, target_is_directory=True)
hooks["pre_tool_call"](session_id="reject", tool_name="write_file", args={"cwd": str(outer), "path": "link/escape.txt"})
nested_alias = Path(os.environ["FAKE_GATEWAY_DIR"]) / "nested-link"; nested_alias.symlink_to(nested, target_is_directory=True)
hooks["pre_tool_call"](session_id="reject", tool_name="write_file", args={"workdir": str(nested_alias), "path": "inside.txt"})
`
	cmd := exec.Command(python, "-c", script)
	cmd.Dir = gatewayDir
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("observer fixture: %v\n%s", err, output)
	}
	log := mustReadFile(t, eventLog)
	if strings.Contains(log, outer+"	") || strings.Count(log, nested+"	") != 2 {
		t.Fatalf("expected only nested session/turn lifecycle, got: %s", log)
	}
	if _, err := os.Stat(observerSessionPath(t, outer, "reject")); !os.IsNotExist(err) {
		t.Fatalf("outer transcript should not exist: %v", err)
	}
	if transcript := mustReadFile(t, observerSessionPath(t, nested, "reject")); !strings.Contains(transcript, "buffered") {
		t.Fatalf("nested transcript lacks buffered prompt: %s", transcript)
	}
}

func TestTranscriptAPIsRejectArbitraryPaths(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	agent := New()
	outside := filepath.Join(t.TempDir(), "private.jsonl")
	writeFixture(t, outside, []byte(`{"v":1,"type":"user","content":"outside-secret"}`+"\n"), 0o600)
	if _, err := agent.ReadTranscript(outside); err == nil {
		t.Fatal("ReadTranscript accepted an arbitrary path")
	}
	if err := agent.WriteSession(protocol.AgentSessionJSON{SessionRef: outside, NativeData: []byte("overwritten")}); err == nil {
		t.Fatal("WriteSession accepted an arbitrary path")
	}
	if got := mustReadFile(t, outside); !strings.Contains(got, "outside-secret") {
		t.Fatalf("arbitrary file was modified: %s", got)
	}
	position, err := agent.GetTranscriptPosition(outside)
	if err != nil || position != 0 {
		t.Fatalf("safe arbitrary position: got %d, err %v", position, err)
	}
	files, current, err := agent.ExtractModifiedFiles(outside, 0)
	if err != nil || len(files) != 0 || current != 0 {
		t.Fatalf("safe arbitrary extraction: files=%v current=%d err=%v", files, current, err)
	}

	owned := observerSessionPath(t, repo, "owned")
	if err := agent.WriteSession(protocol.AgentSessionJSON{SessionRef: owned, NativeData: []byte(`{"v":1,"type":"user","content":"owned"}` + "\n")}); err != nil {
		t.Fatalf("WriteSession owned path: %v", err)
	}
	if data, err := agent.ReadTranscript(owned); err != nil || !strings.Contains(string(data), "owned") {
		t.Fatalf("ReadTranscript owned path: data=%q err=%v", data, err)
	}
}

func observerSessionPath(t *testing.T, repo, sessionID string) string {
	t.Helper()
	dir, err := New().GetSessionDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	return New().ResolveSessionFile(dir, sessionID)
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertConfigState(t *testing.T, home string, enabled, disabled []string) {
	t.Helper()
	root, err := loadConfig(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plugins := mappingValue(root.Content[0], "plugins")
	enabledNode, err := sequenceValue(plugins, "enabled", false)
	if err != nil {
		t.Fatal(err)
	}
	disabledNode, err := sequenceValue(plugins, "disabled", false)
	if err != nil {
		t.Fatal(err)
	}
	values := func(nodeValues []string, nodeName string, nodeContent func() []string) {
		got := nodeContent()
		if !slices.Equal(got, nodeValues) {
			t.Fatalf("plugins.%s: got %v, want %v", nodeName, got, nodeValues)
		}
	}
	values(enabled, "enabled", func() []string { return yamlSequenceStrings(enabledNode) })
	values(disabled, "disabled", func() []string { return yamlSequenceStrings(disabledNode) })
	if mappingValue(root.Content[0], "theme") == nil || mappingValue(plugins, "existing-observer") == nil {
		t.Fatal("unrelated Hermes config was not preserved")
	}
}

func yamlSequenceStrings(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		values = append(values, item.Value)
	}
	return values
}

func writeFixture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	writeFixture(t, path, []byte(content), 0o700)
}
