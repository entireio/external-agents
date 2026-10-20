package devin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-devin/internal/protocol"
)

// HooksFileName is the standalone hooks file used by Devin CLI. Unlike
// .claude/settings.json, the hooks object is the entire file — event names
// are top-level keys with no "hooks" wrapper.
const HooksFileName = "hooks.v1.json"

// Devin hook names - these become subcommands under `entire hooks devin`.
const (
	HookNameSessionStart     = "session-start"
	HookNameSessionEnd       = "session-end"
	HookNameStop             = "stop"
	HookNameUserPromptSubmit = "user-prompt-submit"
)

// Event type values from the external agent protocol.
const (
	eventSessionStart = 1
	eventTurnStart    = 2
	eventTurnEnd      = 3
	eventSessionEnd   = 5
)

// ParseHook translates a Devin hook payload into a normalized lifecycle
// event. Devin payloads carry no transcript_path, so SessionRef is derived
// from the session ID via the canonical transcript location.
func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if len(input) == 0 {
		return nil, nil // No payload — nothing to parse (protocol contract)
	}
	switch hookName {
	case HookNameSessionStart:
		return parseSessionInfoEvent(input, eventSessionStart)
	case HookNameUserPromptSubmit:
		return parseTurnStart(input)
	case HookNameStop:
		return parseSessionInfoEvent(input, eventTurnEnd)
	case HookNameSessionEnd:
		return parseSessionInfoEvent(input, eventSessionEnd)
	default:
		return nil, nil // Unknown hooks have no lifecycle action
	}
}

func parseSessionInfoEvent(input []byte, eventType int) (*protocol.EventJSON, error) {
	var raw sessionInfoRaw
	if err := json.Unmarshal(input, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse hook input: %w", err)
	}
	if raw.SessionID == "" {
		return nil, nil // No session — nothing to track
	}
	return &protocol.EventJSON{
		Type:       eventType,
		SessionID:  raw.SessionID,
		SessionRef: sessionRefForID(raw.SessionID),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func parseTurnStart(input []byte) (*protocol.EventJSON, error) {
	var raw userPromptSubmitRaw
	if err := json.Unmarshal(input, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse hook input: %w", err)
	}
	if raw.SessionID == "" {
		return nil, nil
	}
	return &protocol.EventJSON{
		Type:       eventTurnStart,
		SessionID:  raw.SessionID,
		SessionRef: sessionRefForID(raw.SessionID),
		Prompt:     raw.Prompt,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// --- Hook installation ---

// hooksFilePath returns the absolute path of .devin/hooks.v1.json for the repo.
func hooksFilePath() string {
	return filepath.Join(protocol.RepoRoot(), ".devin", HooksFileName)
}

// hookCommand builds the command installed for a hook verb. The entire binary
// is resolved at install time so hooks work even when the agent's PATH
// differs; failures are swallowed (`|| true`) so a hook error never blocks
// Devin's turn.
func hookCommand(hookName string) string {
	entire := "entire"
	if path, err := exec.LookPath("entire"); err == nil && strings.TrimSpace(path) != "" {
		entire = path
	}
	return fmt.Sprintf("sh -c 'if command -v %s >/dev/null 2>&1; then %s hooks devin %s >/dev/null 2>&1 || true; fi'", shellQuote(entire), shellQuote(entire), hookName)
}

// shellQuote makes a string safe for single-quoted sh interpolation.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`&|;<>()*?[]#~%") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// InstallHooks installs Devin hooks in .devin/hooks.v1.json.
// If force is true, removes existing Entire hooks before installing.
// Returns the number of hooks installed.
func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	hooksPath := hooksFilePath()

	// The whole file is the hooks object; use a raw map to preserve unknown
	// event types on round-trip.
	rawHooks := make(map[string]json.RawMessage)
	if existingData, readErr := os.ReadFile(hooksPath); readErr == nil {
		if err := json.Unmarshal(existingData, &rawHooks); err != nil {
			return 0, fmt.Errorf("failed to parse existing %s: %w", HooksFileName, err)
		}
	}

	// Parse only the hook types we manage
	var sessionStart, sessionEnd, stop, userPromptSubmit []HookMatcher
	parseHookType(rawHooks, "SessionStart", &sessionStart)
	parseHookType(rawHooks, "SessionEnd", &sessionEnd)
	parseHookType(rawHooks, "Stop", &stop)
	parseHookType(rawHooks, "UserPromptSubmit", &userPromptSubmit)

	// If force is true, remove all existing Entire hooks first
	if force {
		sessionStart = removeEntireHooks(sessionStart)
		sessionEnd = removeEntireHooks(sessionEnd)
		stop = removeEntireHooks(stop)
		userPromptSubmit = removeEntireHooks(userPromptSubmit)
	}

	// Define hook commands
	sessionStartCmd := hookCommand(HookNameSessionStart)
	sessionEndCmd := hookCommand(HookNameSessionEnd)
	stopCmd := hookCommand(HookNameStop)
	userPromptSubmitCmd := hookCommand(HookNameUserPromptSubmit)

	count := 0

	// Add hooks if they don't exist
	if !hookCommandExists(sessionStart, sessionStartCmd) {
		sessionStart = addHookToMatcher(sessionStart, sessionStartCmd)
		count++
	}
	if !hookCommandExists(sessionEnd, sessionEndCmd) {
		sessionEnd = addHookToMatcher(sessionEnd, sessionEndCmd)
		count++
	}
	if !hookCommandExists(stop, stopCmd) {
		stop = addHookToMatcher(stop, stopCmd)
		count++
	}
	if !hookCommandExists(userPromptSubmit, userPromptSubmitCmd) {
		userPromptSubmit = addHookToMatcher(userPromptSubmit, userPromptSubmitCmd)
		count++
	}

	if count == 0 {
		return 0, nil // All hooks already installed
	}

	// Marshal modified hook types back to rawHooks
	marshalHookType(rawHooks, "SessionStart", sessionStart)
	marshalHookType(rawHooks, "SessionEnd", sessionEnd)
	marshalHookType(rawHooks, "Stop", stop)
	marshalHookType(rawHooks, "UserPromptSubmit", userPromptSubmit)

	// Write back to file
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o750); err != nil {
		return 0, fmt.Errorf("failed to create .devin directory: %w", err)
	}
	output, err := json.MarshalIndent(rawHooks, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal hooks: %w", err)
	}
	output = append(output, '\n')
	if err := os.WriteFile(hooksPath, output, 0o600); err != nil {
		return 0, fmt.Errorf("failed to write %s: %w", HooksFileName, err)
	}
	return count, nil
}

// UninstallHooks removes Entire hooks from .devin/hooks.v1.json.
func (a *Agent) UninstallHooks() error {
	hooksPath := hooksFilePath()
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		return nil //nolint:nilerr // No hooks file means nothing to uninstall
	}

	rawHooks := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &rawHooks); err != nil {
		return fmt.Errorf("failed to parse %s: %w", HooksFileName, err)
	}

	// Parse only the hook types we need to modify
	var sessionStart, sessionEnd, stop, userPromptSubmit []HookMatcher
	parseHookType(rawHooks, "SessionStart", &sessionStart)
	parseHookType(rawHooks, "SessionEnd", &sessionEnd)
	parseHookType(rawHooks, "Stop", &stop)
	parseHookType(rawHooks, "UserPromptSubmit", &userPromptSubmit)

	// Remove Entire hooks from all hook types
	marshalHookType(rawHooks, "SessionStart", removeEntireHooks(sessionStart))
	marshalHookType(rawHooks, "SessionEnd", removeEntireHooks(sessionEnd))
	marshalHookType(rawHooks, "Stop", removeEntireHooks(stop))
	marshalHookType(rawHooks, "UserPromptSubmit", removeEntireHooks(userPromptSubmit))

	output, err := json.MarshalIndent(rawHooks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hooks: %w", err)
	}
	output = append(output, '\n')
	if err := os.WriteFile(hooksPath, output, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", HooksFileName, err)
	}
	return nil
}

// AreHooksInstalled checks if Entire hooks are installed.
func (a *Agent) AreHooksInstalled() bool {
	data, err := os.ReadFile(hooksFilePath())
	if err != nil {
		return false
	}

	rawHooks := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &rawHooks); err != nil {
		return false
	}

	// Check for at least one of our hooks
	var stop []HookMatcher
	parseHookType(rawHooks, "Stop", &stop)
	for _, matcher := range stop {
		for _, hook := range matcher.Hooks {
			if isEntireHook(hook.Command) {
				return true
			}
		}
	}
	return false
}

// Helper functions for hook management

// parseHookType parses a specific hook type from rawHooks into the target slice.
// Silently ignores parse errors (leaves target unchanged).
func parseHookType(rawHooks map[string]json.RawMessage, hookType string, target *[]HookMatcher) {
	if data, ok := rawHooks[hookType]; ok {
		_ = json.Unmarshal(data, target)
	}
}

// marshalHookType marshals a hook type back to rawHooks.
// If the slice is empty, removes the key from rawHooks.
func marshalHookType(rawHooks map[string]json.RawMessage, hookType string, matchers []HookMatcher) {
	if len(matchers) == 0 {
		delete(rawHooks, hookType)
		return
	}
	data, err := json.Marshal(matchers)
	if err == nil {
		rawHooks[hookType] = data
	}
}

func hookCommandExists(matchers []HookMatcher, command string) bool {
	for _, matcher := range matchers {
		for _, hook := range matcher.Hooks {
			if hook.Command == command {
				return true
			}
		}
	}
	return false
}

// addHookToMatcher appends the command to the empty-string matcher group,
// creating it if needed. All Devin lifecycle hooks use the empty matcher.
func addHookToMatcher(matchers []HookMatcher, command string) []HookMatcher {
	entry := HookEntry{Type: "command", Command: command}
	for i, matcher := range matchers {
		if matcher.Matcher == "" {
			matchers[i].Hooks = append(matchers[i].Hooks, entry)
			return matchers
		}
	}
	return append(matchers, HookMatcher{Matcher: "", Hooks: []HookEntry{entry}})
}

// isEntireHook checks if a command is an Entire hook installed by this agent.
func isEntireHook(command string) bool {
	return strings.Contains(command, " hooks devin ") || strings.HasPrefix(command, "entire ")
}

// removeEntireHooks removes all Entire hooks from a list of matchers.
func removeEntireHooks(matchers []HookMatcher) []HookMatcher {
	result := make([]HookMatcher, 0, len(matchers))
	for _, matcher := range matchers {
		filteredHooks := make([]HookEntry, 0, len(matcher.Hooks))
		for _, hook := range matcher.Hooks {
			if !isEntireHook(hook.Command) {
				filteredHooks = append(filteredHooks, hook)
			}
		}
		// Only keep the matcher if it has hooks remaining
		if len(filteredHooks) > 0 {
			matcher.Hooks = filteredHooks
			result = append(result, matcher)
		}
	}
	return result
}
