package kilo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

const (
	pluginDir        = ".kilo/plugin"
	pluginFile       = ".kilo/plugin/entire.ts"
	transcriptSubdir = "kilo"
)

// kiloHookPayload is the JSON body the plugin sends over stdin to
// `entire hooks kilo <event>`.
type kiloHookPayload struct {
	Type      string          `json:"type"`
	Cwd       string          `json:"cwd,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	ParentID  string          `json:"parent_id,omitempty"`
	Session   json.RawMessage `json:"session,omitempty"`
	Messages  json.RawMessage `json:"messages,omitempty"`
}

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if len(input) == 0 {
		return nil, nil
	}

	var payload kiloHookPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("parse hook payload: %w", err)
	}

	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		return nil, nil
	}

	// Sub-sessions emit their own session.idle. Filter to top-level sessions.
	if strings.TrimSpace(payload.ParentID) != "" {
		return nil, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sessionRef := transcriptPath(sessionID)

	if err := writeSessionFromPayload(sessionRef, payload); err != nil {
		return nil, fmt.Errorf("write session ref: %w", err)
	}

	switch hookName {
	case "session.created":
		return &protocol.EventJSON{
			Type:       1,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Model:      latestModelFromSessionRef(sessionRef),
			Timestamp:  now,
		}, nil

	case "session.idle":
		return &protocol.EventJSON{
			Type:       3,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Model:      latestModelFromSessionRef(sessionRef),
			Timestamp:  now,
		}, nil

	default:
		return nil, nil
	}
}

// writeSessionFromPayload writes the merged Session JSON to sessionRef using
// `session` and `messages` from the plugin payload. Missing fields are kept
// as-is so callers can rely on session_ref existing after either hook fires.
func writeSessionFromPayload(sessionRef string, payload kiloHookPayload) error {
	if strings.TrimSpace(sessionRef) == "" {
		return errors.New("session_ref is required")
	}
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o750); err != nil {
		return err
	}

	var session Session
	if len(payload.Session) > 0 {
		if err := json.Unmarshal(payload.Session, &session); err != nil {
			return fmt.Errorf("parse session payload: %w", err)
		}
	}
	if session.ID == "" {
		session.ID = payload.SessionID
	}
	if len(payload.Messages) > 0 {
		var messages []SessionMessage
		if err := json.Unmarshal(payload.Messages, &messages); err != nil {
			return fmt.Errorf("parse messages payload: %w", err)
		}
		session.Messages = messages
	}

	encoded, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return os.WriteFile(sessionRef, encoded, 0o600)
}

// latestModelFromSessionRef returns the most recent model string recorded in
// the prepared session JSON at sessionRef.
func latestModelFromSessionRef(sessionRef string) string {
	if strings.TrimSpace(sessionRef) == "" {
		return ""
	}
	data, err := os.ReadFile(sessionRef)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return ""
	}
	session, err := parseKiloSession(data)
	if err != nil {
		return ""
	}
	return latestSessionModel(session)
}

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	root := protocol.RepoRoot()
	if !force && a.AreHooksInstalled() {
		return 0, nil
	}

	dir := filepath.Join(root, pluginDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return 0, fmt.Errorf("create plugin dir: %w", err)
	}

	path := filepath.Join(root, pluginFile)
	if err := os.WriteFile(path, []byte(generatePlugin()), 0o600); err != nil {
		return 0, fmt.Errorf("write plugin: %w", err)
	}

	return 2, nil
}

func (a *Agent) UninstallHooks() error {
	root := protocol.RepoRoot()
	path := filepath.Join(root, pluginFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Join(root, pluginDir)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
	kiloDir := filepath.Join(root, ".kilo")
	if entries, err := os.ReadDir(kiloDir); err == nil && len(entries) == 0 {
		_ = os.Remove(kiloDir)
	}
	return nil
}

func (a *Agent) AreHooksInstalled() bool {
	root := protocol.RepoRoot()
	data, err := os.ReadFile(filepath.Join(root, pluginFile))
	return err == nil && strings.Contains(string(data), "entire-agent-kilo")
}

// fetchSession uses the CLI fallback when the session_ref needs to be
// refreshed outside of a plugin event (e.g. `entire prepare-transcript`).
func (a *Agent) fetchSession(sessionID, sessionRef string) error {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(sessionRef) == "" {
		return errors.New("session id and session ref are required")
	}
	runner := a.CommandRunner
	if runner == nil {
		runner = &DefaultCommandRunner{}
	}
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o750); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), prepareTranscriptTimeout)
	defer cancel()
	_, err := runner.ExportSession(ctx, sessionID, sessionRef)
	return err
}

func generatePlugin() string {
	return `import type { Plugin } from "@kilocode/plugin"

// entire-agent-kilo: project-local Entire integration for Kilo.
// Subscribes to the event bus and forwards session.created and
// session.idle to the Entire CLI hook handler. Each forwarded payload
// carries the full Session.Info and messages array fetched via the
// local Kilo SDK client, so Entire can write session_ref atomically.

const KiloIntegration: Plugin = async ({ client, directory }) => {
  // Track which sessions have been announced so duplicate session.created
  // events do not produce duplicate SessionStart hooks downstream.
  const announced = new Set<string>()

  async function fetchSnapshot(sessionID: string): Promise<{ session: unknown; messages: unknown } | null> {
    try {
      const [session, messages] = await Promise.all([
        client.session.get({ path: { id: sessionID } }),
        client.session.messages({ path: { id: sessionID } }),
      ])
      return { session: session.data, messages: messages.data }
    } catch (err) {
      console.error("entire-agent-kilo: snapshot fetch failed", sessionID, err)
      return null
    }
  }

  async function fireHook(event: string, sessionID: string, parentID?: string): Promise<void> {
    const snapshot = await fetchSnapshot(sessionID)
    const body = JSON.stringify({
      type: event,
      cwd: directory,
      session_id: sessionID,
      parent_id: parentID ?? "",
      session: snapshot?.session ?? null,
      messages: snapshot?.messages ?? [],
    })

    await new Promise<void>((resolve) => {
      const { execFile } = require("node:child_process") as typeof import("node:child_process")
      const child = execFile(
        "entire",
        ["hooks", "kilo", event],
        { timeout: 10_000, windowsHide: true, maxBuffer: Infinity },
        (err: Error | null, _stdout: string, stderr: string) => {
          if (err) {
            console.error("entire-agent-kilo: hook failed", event, err.message, stderr)
          }
          resolve()
        },
      )
      child.stdin?.end(body)
    })
  }

  return {
    event: async ({ event }) => {
      if (event.type === "session.created") {
        const info = (event.properties as { info?: { id?: string; parentID?: string } } | undefined)?.info
        const id = info?.id
        if (!id) return
        if (announced.has(id)) return
        announced.add(id)
        await fireHook("session.created", id, info.parentID ?? "")
        return
      }
      if (event.type === "session.idle") {
        const props = event.properties as { sessionID?: string; parentID?: string } | undefined
        const id = props?.sessionID
        if (!id) return
        await fireHook("session.idle", id, props?.parentID ?? "")
      }
    },
  }
}

export default { id: "entire-agent-kilo", server: KiloIntegration }
`
}

func transcriptPath(sessionID string) string {
	return filepath.Join(protocol.DefaultSessionDir(protocol.RepoRoot()), transcriptSubdir, safeSessionID(sessionID)+".json")
}

func safeSessionID(sessionID string) string {
	if sessionID == "" {
		return "unknown"
	}
	re := regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
	return re.ReplaceAllString(sessionID, "_")
}
