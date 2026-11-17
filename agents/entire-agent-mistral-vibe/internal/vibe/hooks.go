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
	if raw.SessionID == "" {
		return nil, nil
	}
	sessionRef := transcriptPath(raw.SessionID)

	// parent_session_id does not by itself mean "subagent": a forked or rewound
	// main session carries the id of the session it came from. What separates
	// them is that Vibe never fires session_start for a subagent, and pre_agent
	// is installed with match = "main", which selects root and fork sessions
	// only. So those two hooks always concern a main session, and they are what
	// creates the transcript file. A turn end or session end for a child session
	// with no transcript is a subagent's, and belongs to its parent's turn.
	if raw.ParentSessionID != "" && (hookName == HookNameNotification || hookName == HookNameSessionEnd) {
		if !fileExists(sessionRef) {
			return nil, nil
		}
	}

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
			a.refreshBestEffort(sessionRef, raw.SessionID, hookFetchTimeout)
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
		markTurnOpen(sessionRef)

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
		if snapshot := a.refreshBestEffort(sessionRef, raw.SessionID, hookFetchTimeout); snapshot != nil {
			event.Model = snapshot.Model
		}
		if err := ensureTranscriptFile(sessionRef); err != nil {
			return nil, err
		}

	case HookNameSessionEnd:
		event.Type = 5
		// Vibe stops hooks five seconds after it quits, so both steps are short.
		a.refreshBestEffort(sessionRef, raw.SessionID, sessionEndFetchTimeout)
		awaitTurnEnd(sessionRef)

	default:
		return nil, nil
	}
	return event, nil
}

// refreshBestEffort updates the stored transcript. A hook must not fail because
// Vibe's app server could not be reached; prepare-transcript tries again.
func (a *Agent) refreshBestEffort(sessionRef, sessionID string, timeout time.Duration) *Snapshot {
	snapshot, err := a.refresh(sessionRef, sessionID, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "entire-agent-%s: transcript not refreshed: %v\n", agentName, err)
		return nil
	}
	return snapshot
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// ensureTranscriptFile creates an empty transcript when none exists, so Entire
// can read a position before the first turn has produced any history.
func ensureTranscriptFile(sessionRef string) error {
	if fileExists(sessionRef) {
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

	newHooksFileMode = 0o644
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
//
// The command is POSIX shell. On Windows Vibe runs hooks in Git Bash when it is
// installed and in PowerShell otherwise; under PowerShell this command does not
// parse, so Git Bash is required there.
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
// a complete block was found. A marker counts only when it is a whole line, so
// marker text quoted inside a user's own string is not mistaken for the block.
func splitBlock(content string) (before, block, after string, found bool) {
	start, end := -1, -1
	offset := 0
	for line := range strings.SplitAfterSeq(content, "\n") {
		switch strings.TrimRight(line, "\r\n \t") {
		case blockStart:
			if start < 0 {
				start = offset
			}
		case blockEnd:
			if start >= 0 && end < 0 {
				end = offset + len(line)
			}
		}
		offset += len(line)
	}
	if start < 0 || end < 0 {
		return content, "", "", false
	}
	return content[:start], content[start:end], content[end:], true
}

// inlineHooksKey matches `hooks = [...]` with a bare or quoted key, which
// cannot coexist with the `[[hooks]]` tables Entire appends.
var inlineHooksKey = regexp.MustCompile(`(?m)^[ \t]*(?:hooks|"hooks"|'hooks')[ \t]*=`)

// hooksFileState is the hooks file as found on disk.
type hooksFileState struct {
	content string
	// mode is the existing file's permissions, or newHooksFileMode.
	mode os.FileMode
}

// readHooksFile reads .vibe/hooks.toml without following a symlink at either
// .vibe or the file itself. A repository can ship .vibe as a link to any
// directory the user can write, including their own ~/.vibe; following it
// would create, rewrite or delete a hooks file outside the repository.
func readHooksFile(root string) (hooksFileState, error) {
	state := hooksFileState{mode: newHooksFileMode}
	dir := filepath.Join(root, vibeDir)
	dirInfo, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if !dirInfo.IsDir() {
		return state, fmt.Errorf("refusing to use %s: not a real directory", dir)
	}

	path := filepath.Join(root, hooksFile)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if !info.Mode().IsRegular() {
		return state, fmt.Errorf("refusing to edit %s: not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	state.content = string(data)
	state.mode = info.Mode().Perm()
	return state, nil
}

// InstallHooks adds Entire's hooks to .vibe/hooks.toml, leaving the user's own
// entries and the file's permissions untouched. Vibe only loads project hooks
// in a trusted folder.
func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	root := protocol.RepoRoot()
	path := filepath.Join(root, hooksFile)
	state, err := readHooksFile(root)
	if err != nil {
		return 0, err
	}

	want := renderBlock()
	before, block, after, found := splitBlock(state.content)
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
		updated = state.content
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
	if err := atomicWriteFile(path, []byte(updated), state.mode); err != nil {
		return 0, fmt.Errorf("write %s: %w", path, err)
	}
	return len(installedHooks), nil
}

// UninstallHooks removes only Entire's block. The file and .vibe directory are
// removed when nothing else is left in them.
func (a *Agent) UninstallHooks() error {
	root := protocol.RepoRoot()
	path := filepath.Join(root, hooksFile)
	state, err := readHooksFile(root)
	if err != nil {
		return err
	}
	before, _, after, found := splitBlock(state.content)
	if !found {
		return nil
	}
	// Install put one blank line between the user's content and the block;
	// take it back out so an install/uninstall round trip changes nothing.
	remaining := before + after
	if strings.TrimSpace(after) == "" {
		remaining = before
		if strings.HasSuffix(remaining, "\n\n") {
			remaining = remaining[:len(remaining)-1]
		}
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
	return atomicWriteFile(path, []byte(remaining), state.mode)
}

func hooksInstalled() (bool, error) {
	state, err := readHooksFile(protocol.RepoRoot())
	if err != nil {
		return false, err
	}
	_, block, _, found := splitBlock(state.content)
	if !found {
		return false, nil
	}
	names := map[string]bool{}
	for line := range strings.SplitSeq(block, "\n") {
		names[strings.TrimRight(line, "\r \t")] = true
	}
	for _, hook := range installedHooks {
		if !names[fmt.Sprintf("name = %q", hook.name)] {
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
