package amp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

const (
	pluginDir         = ".amp/plugins"
	pluginFile        = ".amp/plugins/entire.ts"
	transcriptSubdir  = "amp"
	seenSessionSubdir = "amp/seen"
)

type ampHookPayload struct {
	Type          string            `json:"type"`
	Cwd           string            `json:"cwd,omitempty"`
	ThreadID      string            `json:"thread_id,omitempty"`
	MessageID     string            `json:"message_id,omitempty"`
	Message       string            `json:"message,omitempty"`
	Status        string            `json:"status,omitempty"`
	Messages      []json.RawMessage `json:"messages,omitempty"`
	ModifiedFiles []string          `json:"modified_files,omitempty"`
}

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if len(input) == 0 {
		return nil, nil
	}

	var payload ampHookPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("parse hook payload: %w", err)
	}

	sessionID := payload.ThreadID
	if sessionID == "" {
		return nil, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sessionRef := transcriptPath(sessionID)

	switch hookName {
	case "session.start":
		markSessionSeen(sessionID)
		return &protocol.EventJSON{
			Type:       1,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Timestamp:  now,
		}, nil

	case "agent.start":
		if !sessionSeen(sessionID) {
			markSessionSeen(sessionID)
		}
		if err := appendTranscriptEntry(sessionRef, ampTranscriptEntry{
			Type:     "agent.start",
			ThreadID: sessionID,
			Message:  payload.Message,
		}); err != nil {
			return nil, err
		}
		return &protocol.EventJSON{
			Type:       2,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Prompt:     payload.Message,
			Timestamp:  now,
		}, nil

	case "agent.end":
		if err := appendTranscriptEntry(sessionRef, ampTranscriptEntry{
			Type:     "agent.end",
			ThreadID: sessionID,
			Message:  payload.Message,
		}); err != nil {
			return nil, err
		}
		return &protocol.EventJSON{
			Type:       3,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Timestamp:  now,
		}, nil

	default:
		return nil, nil
	}
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

	return 3, nil
}

func (a *Agent) UninstallHooks() error {
	root := protocol.RepoRoot()
	path := filepath.Join(root, pluginFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	// If the plugin directory is empty after removing the plugin file, remove it as well.
	dir := filepath.Join(root, pluginDir)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
	// If .amp is empty after removing the plugin directory, remove it as well.
	ampDir := filepath.Join(root, ".amp")
	if entries, err := os.ReadDir(ampDir); err == nil && len(entries) == 0 {
		_ = os.Remove(ampDir)
	}

	// Clean up any seen session markers, but leave transcripts in place since they may be useful to the user and are small in size.
	sessionsDir := protocol.DefaultSessionDir(root)
	seenDir := filepath.Join(sessionsDir, seenSessionSubdir)
	if entries, err := os.ReadDir(seenDir); err == nil {
		for _, entry := range entries {
			_ = os.Remove(filepath.Join(seenDir, entry.Name()))
		}
	}

	return nil
}

func (a *Agent) AreHooksInstalled() bool {
	root := protocol.RepoRoot()
	data, err := os.ReadFile(filepath.Join(root, pluginFile))
	return err == nil && strings.Contains(string(data), "entire-agent-amp")
}

func generatePlugin() string {
	return `import type { AgentEndEvent, AgentStartEvent, PluginAPI, ThreadMessage } from "@ampcode/plugin";
import { execFile } from "node:child_process";

// entire-agent-amp: project-local Entire integration for Amp.
export default function (amp: PluginAPI) {
  const seenThreads = new Set<string>();

  function fireHook(hookName: string, data: Record<string, unknown>): Promise<void> {
    return new Promise((resolve) => {
      let body: string;
      try {
        body = JSON.stringify(data);
      } catch (err) {
        amp.logger.log("entire hook stringify failed", hookName, err);
        resolve();
        return;
      }

      // NOTE: we await this from request handlers (agent.start/agent.end) on
      // purpose — Entire's transcript ordering depends on hooks completing in
      // sequence. Do not convert to fire-and-forget.
      const child = execFile(
        "entire",
        ["hooks", "amp", hookName],
        { timeout: 10_000, windowsHide: true, maxBuffer: Infinity },
        (err, _stdout, stderr) => {
          if (err) {
            amp.logger.log("entire hook failed", hookName, err.message, stderr);
          }
          resolve();
        },
      );
      child.stdin?.end(body);
    });
  }

  function modifiedFiles(messages: ThreadMessage[]): string[] {
    const files = new Set<string>();
    for (const item of amp.helpers.toolCallsInMessages(messages)) {
      for (const event of [item.call, item.result]) {
        const modified = amp.helpers.filesModifiedByToolCall(event);
        if (!modified) continue;
        for (const file of modified) files.add(amp.helpers.filePathFromURI(file));
      }
    }
    return Array.from(files).sort();
  }

  amp.on("agent.start", async (event: AgentStartEvent): Promise<void> => {
    if (!seenThreads.has(event.thread.id)) {
      seenThreads.add(event.thread.id);
      await fireHook("session.start", {
        type: "session.start",
        cwd: process.cwd(),
        thread_id: event.thread.id,
      });
    }

    await fireHook("agent.start", {
      type: "agent.start",
      cwd: process.cwd(),
      thread_id: event.thread.id,
      message_id: String(event.id),
      message: event.message,
    });
  });

  amp.on("agent.end", async (event: AgentEndEvent): Promise<void> => {
    await fireHook("agent.end", {
      type: "agent.end",
      cwd: process.cwd(),
      thread_id: event.thread.id,
      message_id: String(event.id),
      message: event.message,
      status: event.status,
      messages: event.messages,
      modified_files: modifiedFiles(event.messages),
    });
  });
}
`
}

func transcriptPath(sessionID string) string {
	return filepath.Join(protocol.DefaultSessionDir(protocol.RepoRoot()), transcriptSubdir, safeSessionID(sessionID)+".jsonl")
}

func seenPath(sessionID string) string {
	return filepath.Join(protocol.DefaultSessionDir(protocol.RepoRoot()), seenSessionSubdir, safeSessionID(sessionID))
}

func sessionSeen(sessionID string) bool {
	_, err := os.Stat(seenPath(sessionID))
	return err == nil
}

func markSessionSeen(sessionID string) {
	path := seenPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err == nil {
		_ = os.WriteFile(path, []byte(sessionID), 0o600)
	}
}

func safeSessionID(sessionID string) string {
	if sessionID == "" {
		return "unknown"
	}
	re := regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
	return re.ReplaceAllString(sessionID, "_")
}
