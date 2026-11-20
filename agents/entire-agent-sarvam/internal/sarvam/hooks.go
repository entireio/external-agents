package sarvam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-sarvam/internal/protocol"
)

// ParseHook converts a sarvam lifecycle hook payload into a protocol event.
//
// Two input shapes reach this code path:
//
//   - the native payload sarvam pipes to the installed command (identified by
//     its "hook_event_name" field), forwarded verbatim by Entire, and
//   - the protocol HookInputJSON the compliance harness (and legacy callers)
//     send, identified by "hook_type".
//
// Events with no protocol representation return a nil event; the handler
// prints "null" and Entire treats them as non-lifecycle.
func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var native sarvamHookInput
	if err := json.Unmarshal(trimmed, &native); err != nil {
		return nil, fmt.Errorf("parse sarvam hook input: %w", err)
	}

	if strings.TrimSpace(native.HookEventName) == "" {
		// Protocol-shaped input: map it onto the native structure so both
		// shapes flow through one path.
		var hookInput protocol.HookInputJSON
		if err := json.Unmarshal(trimmed, &hookInput); err != nil {
			return nil, fmt.Errorf("parse hook input: %w", err)
		}
		native.HookEventName = hookInput.HookType
		if native.HookEventName == "" {
			native.HookEventName = hookName
		}
		native.SessionID = hookInput.SessionID
		native.TranscriptPath = hookInput.SessionRef
		native.Prompt = hookInput.UserPrompt
		native.ToolName = hookInput.ToolName
		native.ToolUseID = hookInput.ToolUseID
		native.ToolInput = hookInput.ToolInput
	}

	event := sarvamEventName(native.HookEventName, hookName)
	metadata := hookMetadata(native)

	switch event {
	case "SessionStart":
		return &protocol.EventJSON{
			Type:       1,
			SessionID:  a.sessionID(native),
			SessionRef: a.sessionRef(native),
			Model:      native.Model,
			Timestamp:  timestampNow(),
			Metadata:   metadata,
		}, nil
	case "UserPromptSubmit":
		if strings.TrimSpace(native.Prompt) == "" {
			return nil, nil
		}
		return &protocol.EventJSON{
			Type:       2,
			SessionID:  a.sessionID(native),
			SessionRef: a.sessionRef(native),
			Prompt:     native.Prompt,
			Model:      native.Model,
			Timestamp:  timestampNow(),
			Metadata:   metadata,
		}, nil
	case "Stop":
		return &protocol.EventJSON{
			Type:            3,
			SessionID:       a.sessionID(native),
			SessionRef:      a.sessionRef(native),
			Model:           native.Model,
			Timestamp:       timestampNow(),
			ResponseMessage: native.LastAssistantMessage,
			Metadata:        metadata,
		}, nil
	case "PostCompact":
		return &protocol.EventJSON{
			Type:       4,
			SessionID:  a.sessionID(native),
			SessionRef: a.sessionRef(native),
			Model:      native.Model,
			Timestamp:  timestampNow(),
			Metadata:   metadata,
		}, nil
	default:
		// PreCompact (a legacy install may still fire it — compaction is
		// reported once, from PostCompact), PreToolUse, PostToolUse,
		// PermissionRequest, and anything unknown: no lifecycle
		// significance for Entire.
		return nil, nil
	}
}

func (a *Agent) sessionID(native sarvamHookInput) string {
	if id := strings.TrimSpace(native.SessionID); id != "" {
		return id
	}
	return stubSessionID
}

func (a *Agent) sessionRef(native sarvamHookInput) string {
	if path := strings.TrimSpace(native.TranscriptPath); path != "" {
		return path
	}
	return a.ResolveSessionFile("", native.SessionID)
}

func timestampNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// sarvamEventName resolves the native event from the payload, falling back to
// the hook name Entire passed. Payloads may carry the PascalCase native event
// ("SessionStart") or the kebab-case protocol name ("session-start"); both map
// onto the installed native event.
func sarvamEventName(fromPayload, fromFlag string) string {
	for _, spec := range hookSpecs {
		if fromPayload == spec.SarvamEvent || fromPayload == spec.HookName {
			return spec.SarvamEvent
		}
	}
	if name := strings.TrimSpace(fromPayload); name != "" {
		return name
	}
	for _, spec := range hookSpecs {
		if fromFlag == spec.HookName || fromFlag == spec.SarvamEvent {
			return spec.SarvamEvent
		}
	}
	return fromFlag
}

func hookMetadata(native sarvamHookInput) map[string]string {
	metadata := map[string]string{
		"agent":           AgentName,
		"hook_event_name": native.HookEventName,
	}
	if native.CWD != "" {
		metadata["cwd"] = native.CWD
	}
	if native.Source != "" {
		metadata["source"] = native.Source
	}
	if native.Trigger != "" {
		metadata["trigger"] = native.Trigger
	}
	if native.PermissionMode != "" {
		metadata["permission_mode"] = native.PermissionMode
	}
	if native.TurnID != "" {
		metadata["turn_id"] = native.TurnID
	}
	return metadata
}

// hookCommand renders the command written into .sarvam/config.toml. It names
// the binary as plain `entire` so it resolves through PATH when the hook
// runs; baking in an absolute path would put a machine-specific path into a
// repo-local config that is routinely committed.
func hookCommand(hookName string) string {
	return fmt.Sprintf("sh -c 'if command -v entire >/dev/null 2>&1; then entire hooks %s %s >/dev/null 2>&1 || true; fi'", AgentName, hookName)
}

// projectConfigPath is the repo-local sarvam config that carries hook
// declarations. Hook *state* (trust) lives in the user config and stays
// untouched: grant it through the sarvam TUI's /hooks review, never here.
func projectConfigPath() string {
	return filepath.Join(protocol.RepoRoot(), ".sarvam", "config.toml")
}

func (a *Agent) InstallHooks(localDev bool, force bool) (int, error) {
	_ = localDev // hook state is user-managed; there is no dev path to point at
	path := projectConfigPath()
	entries := make([]configHookEntry, 0, len(hookSpecs))
	for _, spec := range hookSpecs {
		entries = append(entries, configHookEntry{
			Event:   spec.SarvamEvent,
			Name:    spec.EntryName,
			Command: hookCommand(spec.HookName),
		})
	}
	return upsertHookEntries(path, entries, force)
}

func (a *Agent) UninstallHooks() error {
	return removeHookEntries(projectConfigPath())
}

func (a *Agent) AreHooksInstalled() bool {
	path := projectConfigPath()
	for _, spec := range hookSpecs {
		if !hookEntryInstalled(path, spec.SarvamEvent, hookCommand(spec.HookName)) {
			return false
		}
	}
	return true
}
