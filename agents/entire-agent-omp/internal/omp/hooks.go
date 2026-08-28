package omp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

const (
	HookNameSessionStart    = "session_start"
	HookNameAgentStart      = "agent_start"
	HookNameAgentEnd        = "agent_end"
	HookNameSessionShutdown = "session_shutdown"

	extensionRelDir       = ".omp/extensions/entire"
	extensionRelFile      = ".omp/extensions/entire/index.ts"
	extensionMarker       = "// entire-agent-omp: project-local Entire integration for omp."
	legacyExtensionMarker = "// entire-agent-omp: project-local Entire integration for OMP."
	activeSessionFile     = "active-session"
)

type hookPayload struct {
	Type        string `json:"type"`
	CWD         string `json:"cwd,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	SessionFile string `json:"session_file,omitempty"`
	SessionRef  string `json:"session_ref,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	UserPrompt  string `json:"user_prompt,omitempty"`
	Model       string `json:"model,omitempty"`
}

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if hookName == HookNameSessionShutdown {
		_ = clearCachedSessionID()
		return nil, nil
	}
	if len(bytes.TrimSpace(input)) == 0 {
		return nil, nil
	}

	var payload hookPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("parse hook payload: %w", err)
	}

	switch hookName {
	case HookNameSessionStart, HookNameAgentStart, HookNameAgentEnd:
	default:
		return nil, nil
	}

	sessionID := resolveSessionID(payload)
	if sessionID == "" {
		return nil, nil
	}
	if !validSessionID(sessionID) {
		return nil, fmt.Errorf("unsafe session id %q", sessionID)
	}
	_ = cacheSessionID(sessionID)

	event := &protocol.EventJSON{
		SessionID: sessionID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	switch hookName {
	case HookNameSessionStart:
		event.Type = 1
		event.SessionRef = hookSessionRef(payload)
	case HookNameAgentStart:
		event.Type = 2
		event.SessionRef = hookSessionRef(payload)
		event.Prompt = payload.Prompt
		if event.Prompt == "" {
			event.Prompt = payload.UserPrompt
		}
	case HookNameAgentEnd:
		event.Type = 3
		if source := hookSessionRef(payload); source != "" {
			if snapshot, err := snapshotSession(source, payload.CWD, sessionID); err == nil {
				event.SessionRef = snapshot
				event.Model = payload.Model
				if event.Model == "" {
					event.Model = extractModelFromPath(snapshot)
				}
			}
		}
	}
	return event, nil
}

func resolveSessionID(payload hookPayload) string {
	if id := strings.TrimSpace(payload.SessionID); id != "" {
		return id
	}
	if id := sessionIDFromFilename(hookSessionRef(payload)); id != "" {
		return id
	}
	return readCachedSessionID()
}

func hookSessionRef(payload hookPayload) string {
	if ref := strings.TrimSpace(payload.SessionFile); ref != "" {
		return ref
	}
	return strings.TrimSpace(payload.SessionRef)
}

func sessionIDFromFilename(path string) string {
	base := filepath.Base(strings.TrimSpace(path))
	if base == "." || base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if i := strings.LastIndexByte(base, '_'); i >= 0 {
		base = base[i+1:]
	}
	if !validSessionID(base) {
		return ""
	}
	return base
}

func validSessionID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func ompPrivateDir() string {
	return filepath.Join(protocol.DefaultSessionDir(protocol.RepoRoot()), "omp")
}

func cachedSessionPath() string {
	return filepath.Join(ompPrivateDir(), activeSessionFile)
}

func cacheSessionID(id string) error {
	if !validSessionID(id) {
		return fmt.Errorf("unsafe session id %q", id)
	}
	if err := ensurePrivateDir(ompPrivateDir()); err != nil {
		return err
	}
	return atomicWriteFile(cachedSessionPath(), []byte(id+"\n"), 0o600)
}

func readCachedSessionID() string {
	data, err := os.ReadFile(cachedSessionPath())
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(data))
	if !validSessionID(id) {
		return ""
	}
	return id
}

func clearCachedSessionID() error {
	err := os.Remove(cachedSessionPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear active session: %w", err)
	}
	return nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create omp private directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect omp private directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("omp private path is not a directory: %s", path)
	}
	//nolint:gosec // Directories require execute bits; 0700 is the private-directory equivalent of 0600.
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure omp private directory: %w", err)
	}
	return nil
}

func snapshotSession(source, cwd, sessionID string) (string, error) {
	if !validSessionID(sessionID) {
		return "", fmt.Errorf("unsafe session id %q", sessionID)
	}
	if strings.TrimSpace(source) == "" {
		return "", errors.New("session file is required for agent_end")
	}
	if strings.TrimSpace(cwd) == "" {
		return "", errors.New("cwd is required for agent_end snapshot")
	}
	sessionDir, err := New().GetSessionDir(cwd)
	if err != nil {
		return "", err
	}
	canonicalSessionDir, err := canonicalPath(sessionDir)
	if err != nil {
		return "", fmt.Errorf("resolve omp session directory: %w", err)
	}
	canonicalSource, err := canonicalPath(source)
	if err != nil {
		return "", fmt.Errorf("resolve omp session file: %w", err)
	}
	if filepath.Dir(canonicalSource) != canonicalSessionDir || filepath.Ext(canonicalSource) != ".jsonl" {
		return "", errors.New("omp session file is outside the managed session directory")
	}
	info, err := os.Stat(canonicalSource)
	if err != nil {
		return "", fmt.Errorf("inspect omp session: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("omp session file is not a regular file")
	}
	src, err := os.Open(canonicalSource)
	if err != nil {
		return "", fmt.Errorf("open omp session: %w", err)
	}
	defer src.Close()

	snapshotDir := filepath.Join(canonicalSessionDir, ".entire")
	if err := ensurePrivateDir(snapshotDir); err != nil {
		return "", err
	}
	destination := filepath.Join(snapshotDir, sessionID+".jsonl")
	tmp, err := os.CreateTemp(snapshotDir, ".snapshot-*")
	if err != nil {
		return "", fmt.Errorf("create transcript snapshot: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		return "", errors.Join(err, tmp.Close())
	}
	if _, err := io.Copy(tmp, src); err != nil {
		return "", errors.Join(fmt.Errorf("copy transcript snapshot: %w", err), tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return "", errors.Join(fmt.Errorf("sync transcript snapshot: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close transcript snapshot: %w", err)
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return "", fmt.Errorf("publish transcript snapshot: %w", err)
	}
	return destination, nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Write(data); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func isOwnedExtension(data []byte) bool {
	return bytes.Contains(data, []byte(extensionMarker)) ||
		bytes.Contains(data, []byte(legacyExtensionMarker))
}

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	root := protocol.RepoRoot()
	path := filepath.Join(root, extensionRelFile)
	if data, err := os.ReadFile(path); err == nil {
		if bytes.Equal(data, []byte(generatedExtension)) && !force {
			return 0, nil
		}
		if !isOwnedExtension(data) && !force {
			return 0, fmt.Errorf("refusing to overwrite foreign omp extension %s", path)
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, fmt.Errorf("create omp extension directory: %w", err)
	}
	if err := atomicWriteFile(path, []byte(generatedExtension), 0o600); err != nil {
		return 0, fmt.Errorf("write omp extension: %w", err)
	}
	return 4, nil
}

func (a *Agent) UninstallHooks() error {
	root := protocol.RepoRoot()
	path := filepath.Join(root, extensionRelFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read omp extension: %w", err)
	}
	if !isOwnedExtension(data) {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove omp extension: %w", err)
	}
	_ = os.Remove(filepath.Join(root, extensionRelDir))
	return nil
}

func (a *Agent) AreHooksInstalled() bool {
	data, err := os.ReadFile(filepath.Join(protocol.RepoRoot(), extensionRelFile))
	return err == nil && isOwnedExtension(data)
}

const generatedExtension = `import type { ExtensionAPI, ExtensionContext } from "@oh-my-pi/pi-coding-agent";
import { execFile, execFileSync } from "node:child_process";

// entire-agent-omp: project-local Entire integration for omp.
export default function entireAgent(pi: ExtensionAPI): void {
  let turnOpen = false;
  let pendingPrompt: string | undefined;
  let runEnd: Promise<boolean> | undefined;
  let resolveRunEnd: ((willContinue: boolean) => void) | undefined;
  let deferredEnd: Record<string, unknown> | undefined;
  let turnIdentity: Record<string, unknown> | undefined;
  let sessionGeneration = 0;

  function armRunEnd(): Promise<boolean> {
    if (!runEnd) {
      runEnd = new Promise((resolve) => {
        resolveRunEnd = resolve;
      });
    }
    return runEnd;
  }

  function settleRunEnd(willContinue: boolean): void {
    resolveRunEnd?.(willContinue);
    resolveRunEnd = undefined;
  }

  async function crossRunEnd(): Promise<boolean | undefined> {
    const generation = sessionGeneration;
    const barrier = armRunEnd();
    const willContinue = await barrier;
    if (generation !== sessionGeneration) return undefined;
    if (runEnd === barrier) runEnd = undefined;
    return willContinue;
  }

  function runHook(name: string, payload: Record<string, unknown>, timeout = 10_000): Promise<void> {
    return new Promise((resolve) => {
      let body: string;
      try {
        body = JSON.stringify(payload);
      } catch {
        resolve();
        return;
      }
      try {
        const child = execFile(
          "entire",
          ["hooks", "omp", name],
          { timeout, windowsHide: true, maxBuffer: 4 * 1024 * 1024 },
          () => resolve(),
        );
        child.stdin?.on("error", () => resolve());
        child.stdin?.end(body);
      } catch {
        resolve();
      }
    });
  }

  function runHookSync(name: string, payload: Record<string, unknown>, timeout = 10_000): void {
    let body: string;
    try {
      body = JSON.stringify(payload);
    } catch {
      return;
    }
    try {
      execFileSync(
        "entire",
        ["hooks", "omp", name],
        { input: body, timeout, windowsHide: true, maxBuffer: 4 * 1024 * 1024 },
      );
    } catch {
      // Hook failures must not terminate omp.
    }
  }

  async function startSession(_event: unknown, ctx: ExtensionContext): Promise<void> {
    const previousEnd = turnOpen ? armRunEnd() : undefined;
    sessionGeneration++;
    pendingPrompt = undefined;
    if (previousEnd) {
      const willContinue = await previousEnd;
      if (willContinue && deferredEnd) runHookSync("agent_end", deferredEnd);
    }
    turnOpen = false;
    runEnd = undefined;
    resolveRunEnd = undefined;
    deferredEnd = undefined;
    turnIdentity = undefined;
    await runHook("session_start", {
      type: "session_start",
      cwd: ctx.cwd,
      session_id: ctx.sessionManager.getSessionId(),
      session_file: ctx.sessionManager.getSessionFile(),
    });
  }

  pi.on("session_start", startSession);
  pi.on("session_switch", startSession);
  pi.on("session_branch", startSession);

  pi.on("before_agent_start", (event) => {
    pendingPrompt = event.prompt;
  });

  pi.on("agent_start", async (_event, ctx) => {
    if (pendingPrompt === undefined) {
      if (!turnOpen) return;
      const willContinue = await crossRunEnd();
      if (willContinue) {
        deferredEnd = undefined;
        armRunEnd();
      }
      return;
    }
    const prompt = pendingPrompt;
    pendingPrompt = undefined;
    if (turnOpen) {
      const willContinue = await crossRunEnd();
      if (willContinue === undefined) return;
      if (willContinue) {
        deferredEnd = undefined;
        armRunEnd();
        return;
      }
    }
    runEnd = undefined;
    deferredEnd = undefined;
    turnIdentity = {
      cwd: ctx.cwd,
      session_id: ctx.sessionManager.getSessionId(),
      session_file: ctx.sessionManager.getSessionFile(),
    };
    runHookSync("agent_start", {
      type: "agent_start",
      ...turnIdentity,
      prompt,
    });
    turnOpen = true;
    armRunEnd();
  });

  pi.on("agent_end", (event) => {
    if (!turnOpen || !turnIdentity) return;
    const willContinue = event.willContinue === true;
    const endPayload = {
      type: "agent_end",
      ...turnIdentity,
      model: (event.messages.findLast((message) => message.role === "assistant") as
        { model?: string } | undefined)?.model,
    };
    if (!willContinue) {
      runHookSync("agent_end", endPayload);
      turnOpen = false;
      deferredEnd = undefined;
      turnIdentity = undefined;
    } else {
      deferredEnd = endPayload;
    }
    settleRunEnd(willContinue);
  });

  pi.on("session_shutdown", async (_event, ctx) => {
    await runHook("session_shutdown", {
      type: "session_shutdown",
      cwd: ctx.cwd,
      session_id: ctx.sessionManager.getSessionId(),
      session_file: ctx.sessionManager.getSessionFile(),
    }, 1_500);
  });

  pi.on("tool_call", (event) => {
    if (event.toolName !== "bash" || typeof event.input?.command !== "string") return;
    if (/(^|\s)GIT_TERMINAL_PROMPT\s*=/.test(event.input.command)) return;
    event.input.command = ` + "`export GIT_TERMINAL_PROMPT=0\n${event.input.command}`" + `;
  });
}
`
