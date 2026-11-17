package vibe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

// Hook names passed as `entire hooks mistral-vibe <hook>`.
const (
	HookNameSessionStart = "session-start"
	HookNamePreAgent     = "pre-agent"
	HookNameNotification = "notification"
	HookNameSessionEnd   = "session-end"
)

const (
	notificationTurnCompleted = "turn_completed"
	notificationTurnFailed    = "turn_failed"
)

// hookPayload is the JSON Vibe writes to a hook's stdin. Every event carries
// the session fields; the rest depend on hook_event_name.
type hookPayload struct {
	SessionID        string `json:"session_id"`
	ParentSessionID  string `json:"parent_session_id"`
	Source           string `json:"source"`
	Model            string `json:"model"`
	Prompt           string `json:"prompt"`
	NotificationType string `json:"notification_type"`
	Message          string `json:"message"`
}

// ParseHook maps a Vibe hook payload to an Entire lifecycle event.
//
// Turn end comes from the turn_completed / turn_failed notifications, not from
// post_agent: post_agent fires on a candidate answer that a hook can still
// reject, and the reply is not in the session history until the turn is
// accepted.
func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if len(strings.TrimSpace(string(input))) == 0 {
		return nil, nil
	}
	var raw hookPayload
	if err := json.Unmarshal(input, &raw); err != nil {
		return nil, fmt.Errorf("parse %s payload: %w", hookName, err)
	}
	// Subagents are part of the parent's turn; their own events are not turns.
	if raw.SessionID == "" || raw.ParentSessionID != "" {
		return nil, nil
	}

	sessionRef := transcriptPath(raw.SessionID)
	event := &protocol.EventJSON{
		SessionID:  raw.SessionID,
		SessionRef: sessionRef,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}

	switch hookName {
	case HookNameSessionStart:
		event.Type = 1
		event.Model = raw.Model
		// A resumed or forked session already has history; store it so the
		// first turn's checkpoint starts after it.
		if raw.Source == "resume" || raw.Source == "fork" {
			a.refreshBestEffort(sessionRef, raw.SessionID)
		}
		if err := ensureTranscriptFile(sessionRef); err != nil {
			return nil, err
		}

	case HookNamePreAgent:
		event.Type = 2
		event.Prompt = raw.Prompt
		if err := ensureTranscriptFile(sessionRef); err != nil {
			return nil, err
		}

	case HookNameNotification:
		switch raw.NotificationType {
		case notificationTurnCompleted:
		case notificationTurnFailed:
			if raw.Message != "" {
				event.Metadata = map[string]string{"error": raw.Message}
			}
		default:
			return nil, nil
		}
		event.Type = 3
		markTurnEnd(sessionRef)
		if snapshot := a.refreshBestEffort(sessionRef, raw.SessionID); snapshot != nil {
			event.Model = snapshot.Model
		}
		if err := ensureTranscriptFile(sessionRef); err != nil {
			return nil, err
		}

	case HookNameSessionEnd:
		event.Type = 5
		// The refresh also gives a turn-end hook started at the same moment
		// time to announce itself before the wait below looks for it.
		a.refreshBestEffort(sessionRef, raw.SessionID)
		awaitTurnEnd(sessionRef)

	default:
		return nil, nil
	}
	return event, nil
}

// refreshBestEffort updates the stored transcript. A hook must not fail because
// Vibe's app server could not be reached; prepare-transcript tries again.
func (a *Agent) refreshBestEffort(sessionRef, sessionID string) *Snapshot {
	snapshot, err := a.refresh(sessionRef, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "entire-agent-%s: transcript not refreshed: %v\n", agentName, err)
		return nil
	}
	return snapshot
}

// ensureTranscriptFile creates an empty transcript when none exists, so Entire
// can read a position before the first turn has produced any history.
func ensureTranscriptFile(sessionRef string) error {
	if _, err := os.Lstat(sessionRef); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(sessionRef, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return file.Close()
}

// --- hooks.toml ---

const (
	vibeDir   = ".vibe"
	hooksFile = ".vibe/hooks.toml"

	blockStart = "# >>> entire hooks: managed by `entire enable --agent mistral-vibe`, do not edit >>>"
	blockEnd   = "# <<< entire hooks <<<"
)

type hookEntry struct {
	name     string
	hookType string
	match    string
	hookName string
	timeout  int
}

// installedHooks are the Vibe hooks Entire needs. Tool hooks are not installed:
// tool calls are read from the session history instead, which keeps Entire out
// of the path of every tool call.
var installedHooks = []hookEntry{
	{name: "entire-session-start", hookType: "session_start", hookName: HookNameSessionStart, timeout: 60},
	{name: "entire-pre-agent", hookType: "pre_agent", match: "main", hookName: HookNamePreAgent, timeout: 60},
	{name: "entire-turn-completed", hookType: "notification", match: notificationTurnCompleted, hookName: HookNameNotification, timeout: 120},
	{name: "entire-turn-failed", hookType: "notification", match: notificationTurnFailed, hookName: HookNameNotification, timeout: 120},
	{name: "entire-session-end", hookType: "session_end", hookName: HookNameSessionEnd, timeout: 120},
}

// hookCommand always names the `entire` binary on PATH and never fails or
// prints: Vibe treats non-JSON stdout or a non-zero exit as a hook failure and
// shows a warning on every event. An absolute path would break the committed
// file on any other machine.
func hookCommand(hookName string) string {
	return fmt.Sprintf("if command -v entire >/dev/null 2>&1; then entire hooks %s %s >/dev/null 2>&1 || true; fi", agentName, hookName)
}

func renderBlock() string {
	var b strings.Builder
	b.WriteString(blockStart + "\n")
	for _, hook := range installedHooks {
		b.WriteString("[[hooks]]\n")
		fmt.Fprintf(&b, "name = %q\n", hook.name)
		fmt.Fprintf(&b, "type = %q\n", hook.hookType)
		if hook.match != "" {
			fmt.Fprintf(&b, "match = %q\n", hook.match)
		}
		fmt.Fprintf(&b, "command = %q\n", hookCommand(hook.hookName))
		fmt.Fprintf(&b, "timeout = %d.0\n", hook.timeout)
		b.WriteString("description = \"Entire session tracking\"\n\n")
	}
	b.WriteString(blockEnd + "\n")
	return b.String()
}

// splitBlock returns the text before and after the managed block, and whether
// a complete block was found.
func splitBlock(content string) (before, block, after string, found bool) {
	start := strings.Index(content, blockStart)
	if start < 0 {
		return content, "", "", false
	}
	end := strings.Index(content[start:], blockEnd)
	if end < 0 {
		return content, "", "", false
	}
	end += start + len(blockEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[:start], content[start:end], content[end:], true
}

// inlineHooksKey matches `hooks = [...]`, which cannot coexist with the
// `[[hooks]]` tables Entire appends.
var inlineHooksKey = regexp.MustCompile(`(?m)^\s*hooks\s*=`)

func readHooksFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to edit %s: not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// InstallHooks adds Entire's hooks to .vibe/hooks.toml, leaving the user's own
// entries untouched. Vibe only loads project hooks in a trusted folder.
func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	root := protocol.RepoRoot()
	path := filepath.Join(root, hooksFile)
	content, err := readHooksFile(path)
	if err != nil {
		return 0, err
	}

	want := renderBlock()
	before, block, after, found := splitBlock(content)
	if found && block == want && !force {
		return 0, nil
	}
	if inlineHooksKey.MatchString(before + after) {
		return 0, fmt.Errorf("%s defines hooks as an inline array; convert it to [[hooks]] tables before installing", path)
	}

	var updated string
	if found {
		updated = before + want + after
	} else {
		updated = content
		if updated != "" && !strings.HasSuffix(updated, "\n") {
			updated += "\n"
		}
		if updated != "" {
			updated += "\n"
		}
		updated += want
	}

	if err := os.MkdirAll(filepath.Join(root, vibeDir), 0o750); err != nil {
		return 0, fmt.Errorf("create %s: %w", vibeDir, err)
	}
	if err := atomicWriteFile(path, []byte(updated), 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", path, err)
	}
	return len(installedHooks), nil
}

// UninstallHooks removes only Entire's block. The file and .vibe directory are
// removed when nothing else is left in them.
func (a *Agent) UninstallHooks() error {
	root := protocol.RepoRoot()
	path := filepath.Join(root, hooksFile)
	content, err := readHooksFile(path)
	if err != nil {
		return err
	}
	before, _, after, found := splitBlock(content)
	if !found {
		return nil
	}
	remaining := strings.TrimRight(before, "\n")
	if remaining != "" {
		remaining += "\n"
	}
	if strings.TrimSpace(after) != "" {
		if remaining != "" {
			remaining += "\n"
		}
		remaining += strings.TrimLeft(after, "\n")
	}

	if strings.TrimSpace(remaining) == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		dir := filepath.Join(root, vibeDir)
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
		return nil
	}
	return atomicWriteFile(path, []byte(remaining), 0o644)
}

func hooksInstalled() (bool, error) {
	content, err := readHooksFile(filepath.Join(protocol.RepoRoot(), hooksFile))
	if err != nil {
		return false, err
	}
	_, block, _, found := splitBlock(content)
	if !found {
		return false, nil
	}
	for _, hook := range installedHooks {
		if !strings.Contains(block, fmt.Sprintf("name = %q", hook.name)) {
			return false, nil
		}
	}
	return true, nil
}

// AreHooksInstalled reports false when the state cannot be read; use
// HooksInstalledState when "unknown" must not be mistaken for "none".
func (a *Agent) AreHooksInstalled() bool {
	installed, _ := hooksInstalled()
	return installed
}

// HooksInstalledState returns an error when the hooks file exists but cannot
// be read, so the caller can report the state as unknown.
func (a *Agent) HooksInstalledState() (bool, error) {
	installed, err := hooksInstalled()
	if err != nil {
		return false, errors.Join(errors.New("cannot read hooks state"), err)
	}
	return installed, nil
}
