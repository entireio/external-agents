package pi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

const (
	extensionDir       = ".pi/extensions/entire"
	extensionFile      = ".pi/extensions/entire/index.ts"
	activeSessionFile  = "pi-active-session"
	settingsLocalFile  = ".entire/settings.local.json"
	commitLinkingKey   = "commit_linking"
	commitLinkingValue = "always"
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
		// Provide the live pi session file as SessionRef so that
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
	if err := ensureCommitLinkingAlways(root); err != nil {
		return 0, err
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
	return `import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { execFileSync } from "node:child_process";

export default function (pi: ExtensionAPI) {
  function fireHook(hookName: string, data: Record<string, unknown>) {
    try {
      const json = JSON.stringify(data);
      execFileSync("entire", ["hooks", "pi", hookName], {
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
`
}

func ensureCommitLinkingAlways(root string) error {
	path := filepath.Join(root, settingsLocalFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create entire settings dir: %w", err)
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}
	settings[commitLinkingKey] = commitLinkingValue

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", settingsLocalFile, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", settingsLocalFile, err)
	}
	return nil
}

func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", settingsLocalFile, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse %s: %w", settingsLocalFile, err)
	}
	if settings == nil {
		return map[string]any{}, nil
	}
	return settings, nil
}

// cacheSessionID writes the session ID to .entire/tmp/pi-active-session.
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

// captureTranscript copies the Pi JSONL session file to .entire/tmp/<id>.json
// so that Entire can read the transcript. Returns the path to the cached file.
func captureTranscript(sessionID, piSessionFile string) string {
	if sessionID == "" || piSessionFile == "" {
		return ""
	}
	dir := protocol.DefaultSessionDir(protocol.RepoRoot())
	_ = os.MkdirAll(dir, 0o750)
	dst := filepath.Join(dir, sessionID+".json")

	data, err := os.ReadFile(piSessionFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "entire-agent-pi: capture transcript: read %s: %v\n", piSessionFile, err)
		return ""
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "entire-agent-pi: capture transcript: write %s: %v\n", dst, err)
		return ""
	}
	return dst
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
