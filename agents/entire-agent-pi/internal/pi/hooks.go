package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

const (
	extensionDir  = ".pi/extensions/entire"
	extensionFile = ".pi/extensions/entire/index.ts"
)

// piHookPayload is the JSON the TypeScript extension sends to
// `entire agent hook pi <event>`, which arrives on stdin of parse-hook.
type piHookPayload struct {
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

	var payload piHookPayload
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
		return &protocol.EventJSON{
			Type:      1, // SessionStart
			SessionID: sessionID,
			Timestamp: now,
		}, nil

	case "before_agent_start":
		return &protocol.EventJSON{
			Type:      2, // TurnStart
			SessionID: sessionID,
			Prompt:    payload.Prompt,
			Timestamp: now,
		}, nil

	case "agent_end":
		return &protocol.EventJSON{
			Type:      3, // TurnEnd
			SessionID: sessionID,
			Timestamp: now,
		}, nil

	case "session_shutdown":
		// Cleanup event, no protocol lifecycle significance.
		return nil, nil

	default:
		return nil, nil
	}
}

func (a *Agent) InstallHooks(localDev bool, force bool) (int, error) {
	root := protocol.RepoRoot()

	// If already installed and not forcing, return 0 (idempotent no-op).
	if !force && a.AreHooksInstalled() {
		return 0, nil
	}

	dir := filepath.Join(root, extensionDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create extension dir: %w", err)
	}

	binName := "entire-agent-pi"
	if localDev {
		binName = "./entire-agent-pi"
	}

	ext := generateExtension(binName)

	path := filepath.Join(root, extensionFile)
	if err := os.WriteFile(path, []byte(ext), 0o644); err != nil {
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

func generateExtension(binName string) string {
	return fmt.Sprintf(`import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { execFileSync } from "node:child_process";

export default function (pi: ExtensionAPI) {
  function fireHook(hookName: string, data: Record<string, unknown>) {
    try {
      const json = JSON.stringify(data);
      execFileSync("entire", ["agent", "hook", "pi", hookName], {
        input: json,
        timeout: 10000,
        stdio: ["pipe", "pipe", "pipe"],
      });
    } catch {
      // best effort — don't block the agent
    }
  }

  pi.on("session_start", async (_event, ctx) => {
    fireHook("session_start", {
      type: "session_start",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
    });
  });

  pi.on("before_agent_start", async (event, ctx) => {
    fireHook("before_agent_start", {
      type: "before_agent_start",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
      prompt: event.prompt,
    });
  });

  pi.on("agent_end", async (_event, ctx) => {
    fireHook("agent_end", {
      type: "agent_end",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
    });
  });

  pi.on("session_shutdown", async () => {
    fireHook("session_shutdown", {
      type: "session_shutdown",
    });
  });
}
`)
}

// extractSessionIDFromPath extracts the UUID from a Pi session filename.
// Pattern: <timestamp>_<uuid>.jsonl → returns <uuid>
func extractSessionIDFromPath(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	// Remove .jsonl extension
	if len(base) > 6 && base[len(base)-6:] == ".jsonl" {
		base = base[:len(base)-6]
	}
	// Find the UUID after the last underscore
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '_' {
			return base[i+1:]
		}
	}
	return base
}
