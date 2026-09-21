package amp

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

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

const (
	pluginDir        = ".amp/plugins"
	pluginFile       = ".amp/plugins/entire.ts"
	transcriptSubdir = "amp"
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
		_ = a.exportThread(sessionID, sessionRef)
		return &protocol.EventJSON{
			Type:       1,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Model:      latestModelFromSessionRef(sessionRef),
			Timestamp:  now,
		}, nil

	case "agent.start":
		model := latestModelFromSessionRef(sessionRef)
		if model == "" {
			// Fallback: ensure we have a prepared transcript so we can populate the model.
			if exportErr := a.exportThread(sessionID, sessionRef); exportErr == nil {
				model = latestModelFromSessionRef(sessionRef)
			}
		}
		return &protocol.EventJSON{
			Type:       2,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Prompt:     payload.Message,
			Model:      model,
			Timestamp:  now,
		}, nil

	case "agent.end":
		if err := a.exportThread(sessionID, sessionRef); err != nil {
			return nil, fmt.Errorf("export thread on agent.end: %w", err)
		}
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

// exportThread runs `amp threads export` for the given thread and stores the
// result at sessionRef as one-message-per-line JSONL, so Entire's line-based
// transcript scoping lands on message boundaries (see session_jsonl.go). The
// native single-JSON export never reaches disk. The parent directory is created
// if needed. Callers generally ignore the returned error and fall back to
// reporting an empty model so events are still emitted.
func (a *Agent) exportThread(threadID, sessionRef string) error {
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(sessionRef) == "" {
		return errors.New("thread id and session ref are required")
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

	raw, err := runner.ExportThread(ctx, threadID)
	if err != nil {
		return err
	}
	thread, err := parseAmpExport(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(thread.ID) == "" {
		thread.ID = strings.TrimSpace(threadID)
	}
	encoded, err := materializeThread(thread)
	if err != nil {
		return err
	}
	return atomicWriteFile(sessionRef, encoded, 0o600)
}

// latestModelFromSessionRef returns the most recent model string recorded in
// the prepared transcript at sessionRef. It returns an empty string if the
// file is missing or not yet a prepared Thread JSON.
func latestModelFromSessionRef(sessionRef string) string {
	if strings.TrimSpace(sessionRef) == "" {
		return ""
	}
	data, err := os.ReadFile(sessionRef)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return ""
	}
	messages, err := decodeTranscript(data)
	if err != nil {
		return ""
	}
	return latestThreadModel(messages)
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

	return nil
}

func (a *Agent) AreHooksInstalled() bool {
	root := protocol.RepoRoot()
	data, err := os.ReadFile(filepath.Join(root, pluginFile))
	return err == nil && strings.Contains(string(data), "entire-agent-amp")
}

func generatePlugin() string {
	return `import type {
  AgentEndEvent,
  AgentStartEvent,
  PluginAPI,
  SessionStartEvent,
  ThreadMessage,
} from "@ampcode/plugin";
import { execFile } from "node:child_process";

// entire-agent-amp: project-local Entire integration for Amp.
export default function (amp: PluginAPI) {
  // In-memory session tracking.
  let currentThreadId: string | null = null;
  const seenThreads = new Set<string>();

  // trackThread records the given thread id and returns true when it is the
  // first time we have seen it for the current session. When a different
  // thread id is discovered we clear all tracking — the user has switched to
  // a new thread, so prior dedupe state no longer applies.
  function trackThread(threadId: string): boolean {
    if (threadId !== currentThreadId) {
      seenThreads.clear();
      currentThreadId = threadId;
    }
    if (seenThreads.has(threadId)) return false;
    seenThreads.add(threadId);
    return true;
  }

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

  amp.on("session.start", async (event: SessionStartEvent): Promise<void> => {
    const threadId = event.thread?.id;
    if (!threadId) return;
    if (!trackThread(threadId)) return;
    await fireHook("session.start", {
      type: "session.start",
      cwd: process.cwd(),
      thread_id: threadId,
    });
  });

  amp.on("agent.start", async (event: AgentStartEvent): Promise<void> => {
    // Amp's session.start does not always include a thread id, so agent.start
    // is the fallback point for firing the synthetic session.start hook.
    if (trackThread(event.thread.id)) {
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

func safeSessionID(sessionID string) string {
	if sessionID == "" {
		return "unknown"
	}
	re := regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
	return re.ReplaceAllString(sessionID, "_")
}
