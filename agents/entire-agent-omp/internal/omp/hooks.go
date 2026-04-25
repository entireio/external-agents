package omp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

const (
	extensionDir      = ".omp/extensions/entire"
	extensionFile     = ".omp/extensions/entire/index.ts"
	activeSessionFile = "omp-active-session"
)

// ompHookPayload is the JSON the TypeScript extension sends to
// `entire agent hook omp <event>`, which arrives on stdin of parse-hook.
type ompHookPayload struct {
	Type         string `json:"type"`
	Cwd          string `json:"cwd,omitempty"`
	SessionFile  string `json:"session_file,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	MessageCount int    `json:"message_count,omitempty"`
	TurnIndex    int    `json:"turn_index,omitempty"`
}

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if len(input) == 0 {
		return nil, nil
	}

	var payload ompHookPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("parse hook payload: %w", err)
	}

	sessionID := payload.SessionID
	if sessionID == "" {
		sessionID = extractSessionIDFromPath(payload.SessionFile)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	switch hookName {
	case "session_start":
		cacheSessionID(sessionID)
		return &protocol.EventJSON{
			Type:      1, // SessionStart
			SessionID: sessionID,
			Timestamp: now,
		}, nil

	case "before_agent_start":
		if sessionID == "" {
			sessionID = readCachedSessionID()
		} else {
			cacheSessionID(sessionID)
		}
		// Provide the live omp session file as SessionRef so that
		// state.TranscriptPath is populated before any mid-turn commits.
		// Without this, the post-commit hook cannot condense when no
		// shadow branch exists yet (no prior step checkpoints).
		return &protocol.EventJSON{
			Type:       2, // TurnStart
			SessionID:  sessionID,
			SessionRef: payload.SessionFile,
			Prompt:     payload.Prompt,
			Timestamp:  now,
		}, nil

	case "agent_end":
		if sessionID == "" {
			sessionID = readCachedSessionID()
		}
		sessionRef := captureTranscript(sessionID, payload.SessionFile)
		return &protocol.EventJSON{
			Type:       3, // TurnEnd
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Timestamp:  now,
		}, nil

	case "session_shutdown":
		clearCachedSessionID()
		return nil, nil

	default:
		return nil, nil
	}
}

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	root := protocol.RepoRoot()

	// If already installed and not forcing, return 0 (idempotent no-op).
	if !force && a.AreHooksInstalled() {
		return 0, nil
	}

	dir := filepath.Join(root, extensionDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return 0, fmt.Errorf("create extension dir: %w", err)
	}

	ext := generateExtension()

	path := filepath.Join(root, extensionFile)
	if err := os.WriteFile(path, []byte(ext), 0o600); err != nil {
		return 0, fmt.Errorf("write extension: %w", err)
	}

	return 4, nil // 4 hooks: session_start, before_agent_start, agent_end, session_shutdown
}

func (a *Agent) UninstallHooks() error {
	root := protocol.RepoRoot()
	dir := filepath.Join(root, extensionDir)
	return os.RemoveAll(dir)
}

func (a *Agent) AreHooksInstalled() bool {
	root := protocol.RepoRoot()
	path := filepath.Join(root, extensionFile)
	_, err := os.Stat(path)
	return err == nil
}

func generateExtension() string {
	return `import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { execFile } from "node:child_process";

export default function (pi: ExtensionAPI) {
  function fireHook(hookName: string, data: Record<string, unknown>): Promise<void> {
    return new Promise((resolve) => {
      try {
        const child = execFile(
          "entire",
          ["hooks", "omp", hookName],
          {
            timeout: 10000,
            windowsHide: true,
          },
          () => resolve(),
        );
        child.stdin?.end(JSON.stringify(data));
      } catch {
        // best effort — don't block the agent
        resolve();
      }
    });
  }

  pi.on("tool_call", async (event) => {
    if (event.toolName !== "bash") {
      return;
    }

    const input = event.input as { command?: string };
    if (typeof input.command !== "string" || input.command.includes("GIT_TERMINAL_PROMPT=")) {
      return;
    }

    // omp tool subprocesses may inherit a real TTY even though the agent cannot
    // answer hook prompts. Disable git/Entire terminal prompts for bash calls so
    // Entire treats agent-driven commits as non-interactive.
    input.command = "export GIT_TERMINAL_PROMPT=0\n" + input.command;
  });

  pi.on("session_start", async (_event, ctx) => {
    await fireHook("session_start", {
      type: "session_start",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
    });
  });

  pi.on("before_agent_start", async (event, ctx) => {
    await fireHook("before_agent_start", {
      type: "before_agent_start",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
      prompt: event.prompt,
    });
  });

  pi.on("agent_end", async (_event, ctx) => {
    await fireHook("agent_end", {
      type: "agent_end",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
    });
  });

  pi.on("session_shutdown", async () => {
    await fireHook("session_shutdown", {
      type: "session_shutdown",
    });
  });
}`
}

// cacheSessionID writes the session ID to .entire/tmp/omp-active-session.
func cacheSessionID(id string) {
	if id == "" {
		return
	}
	dir := protocol.DefaultSessionDir(protocol.RepoRoot())
	_ = os.MkdirAll(dir, 0o750)
	_ = os.WriteFile(filepath.Join(dir, activeSessionFile), []byte(id), 0o600)
}

// readCachedSessionID reads the cached session ID.
func readCachedSessionID() string {
	dir := protocol.DefaultSessionDir(protocol.RepoRoot())
	data, err := os.ReadFile(filepath.Join(dir, activeSessionFile))
	if err != nil {
		return ""
	}
	return string(data)
}

// clearCachedSessionID removes the cached session ID file.
func clearCachedSessionID() {
	dir := protocol.DefaultSessionDir(protocol.RepoRoot())
	_ = os.Remove(filepath.Join(dir, activeSessionFile))
}

// captureTranscript copies the omp JSONL session file to .entire/tmp/<id>.json
// so that Entire can read the transcript. Returns the path to the cached file.
func captureTranscript(sessionID, ompSessionFile string) string {
	if sessionID == "" || ompSessionFile == "" {
		return ""
	}
	dir := protocol.DefaultSessionDir(protocol.RepoRoot())
	_ = os.MkdirAll(dir, 0o750)
	dst := filepath.Join(dir, sessionID+".json")

	data, err := os.ReadFile(ompSessionFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "entire-agent-omp: capture transcript: read %s: %v\n", ompSessionFile, err)
		return ""
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "entire-agent-omp: capture transcript: write %s: %v\n", dst, err)
		return ""
	}
	return dst
}

// extractSessionIDFromPath extracts the session ID from an omp session filename.
// Pattern: <timestamp>_<sessionId>.jsonl → returns <sessionId>
// Works for both UUID (pi) and snowflake hex (omp) session IDs.
func extractSessionIDFromPath(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	// Remove .jsonl extension
	if len(base) > 6 && base[len(base)-6:] == ".jsonl" {
		base = base[:len(base)-6]
	}
	// Find the session ID after the last underscore
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '_' {
			return base[i+1:]
		}
	}
	return base
}
