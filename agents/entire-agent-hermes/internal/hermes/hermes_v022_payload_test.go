package hermes

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestObserverAcceptsHermesV022TaskIDPayload covers the Hermes 0.22.x hook
// payload, where the session identifier arrives in "task_id" and the
// session_id, turn_id, tool_call_id and api_request_id fields are empty
// strings. Before the fallback the observer returned at its first statement
// for every callback, so it silently captured nothing while still failing open.
func TestObserverAcceptsHermesV022TaskIDPayload(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	home := t.TempDir()
	repo := canonicalTempDir(t)
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "entire"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERMES_HOME", home)
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	if _, err := New().InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}

	const sessionID = "20261027_111234_44b40482"
	script := `
import importlib.util
import os
from pathlib import Path

home = Path(os.environ["HERMES_HOME"])
repo = Path(os.environ["ENTIRE_REPO_ROOT"])
path = home / "plugins" / "entire-observer" / "__init__.py"
spec = importlib.util.spec_from_file_location("entire_observer_task_id", path, submodule_search_locations=[str(path.parent)])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
hooks = {}
class Context:
    def register_hook(self, name, callback): hooks[name] = callback
module.register(Context())
os.chdir(repo)
# Hermes 0.22.x shape: every identifier field is empty and the session
# identifier is carried by task_id.
base = {"session_id": "", "turn_id": "", "tool_call_id": "", "api_request_id": "", "task_id": "SESSION_ID"}
hooks["on_session_start"](**base, model="model one", platform="platform-id")
hooks["pre_llm_call"](**base, user_message="create created.txt", model="model one")
target = {"path": str(repo / "created.txt"), "content": "tool-arg-secret"}
hooks["pre_tool_call"](**base, tool_name="write_file", args=target)
(repo / "created.txt").write_text("created\n", encoding="utf-8")
hooks["post_tool_call"](**base, tool_name="write_file", args=target, result="raw-result-secret", status="ok")
hooks["post_llm_call"](**base, assistant_response="wrote created.txt", model="model one")
hooks["on_session_end"](**base)
`
	script = strings.ReplaceAll(script, "SESSION_ID", sessionID)
	cmd := exec.Command(python, "-c", script)
	cmd.Dir = repo
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("observer fixture: %v\n%s", err, output)
	}

	transcript, err := os.ReadFile(observerSessionPath(t, repo, sessionID))
	if err != nil {
		t.Fatalf("transcript for task_id session was not written: %v", err)
	}
	text := string(transcript)
	if !strings.Contains(text, "create created.txt") || !strings.Contains(text, "wrote created.txt") {
		t.Fatalf("transcript missing prompt or response: %s", text)
	}
	for _, forbidden := range []string{"tool-arg-secret", "raw-result-secret", "platform-id"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("observer captured forbidden value %q: %s", forbidden, text)
		}
	}
}
