package qwen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-qwen/internal/protocol"
)

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	raw, err := parseHookInput(input)
	if err != nil {
		return nil, err
	}
	raw.HookEventName = qwenEventName(hookName, raw.HookEventName)
	normalizeRawHook(&raw, hookName)

	if err := a.appendSidecar(raw); err != nil {
		return nil, err
	}

	sessionRef := a.sidecarPath(raw.SessionID)
	metadata := hookMetadata(raw)

	switch hookName {
	case HookNameSessionStart:
		return &protocol.EventJSON{
			Type:       1,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Model:      raw.Model,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	case HookNameUserPromptSubmit:
		return &protocol.EventJSON{
			Type:       2,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Prompt:     raw.Prompt,
			Model:      raw.Model,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	case HookNameStop:
		return &protocol.EventJSON{
			Type:            3,
			SessionID:       raw.SessionID,
			SessionRef:      sessionRef,
			Model:           raw.Model,
			Timestamp:       raw.Timestamp,
			ResponseMessage: raw.LastAssistantMessage,
			Metadata:        metadata,
		}, nil
	case HookNameStopFailure:
		return &protocol.EventJSON{
			Type:            3,
			SessionID:       raw.SessionID,
			SessionRef:      sessionRef,
			Model:           raw.Model,
			Timestamp:       raw.Timestamp,
			ResponseMessage: raw.ErrorDetails,
			Metadata:        metadata,
		}, nil
	case HookNameSessionEnd:
		return &protocol.EventJSON{
			Type:       5,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	case HookNamePreCompact:
		return &protocol.EventJSON{
			Type:       4,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	case HookNamePostToolUse, HookNamePostToolUseFailure:
		return nil, nil
	default:
		return nil, nil
	}
}

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	repoRoot := protocol.RepoRoot()
	settingsPath := filepath.Join(repoRoot, ".qwen", settingsFileName)

	rawSettings, rawHooks, err := readSettings(settingsPath)
	if err != nil {
		return 0, err
	}

	if force {
		removeEntireHooks(rawHooks)
	}

	count := 0
	for _, spec := range hookSpecs {
		var matchers []qwenHookMatcher
		parseHookType(rawHooks, spec.QwenEvent, &matchers)
		command := hookCommand(spec.HookName)
		if !hookExists(matchers, spec.Matcher, spec.EntryName, command) {
			matchers = addHook(matchers, spec.Matcher, spec.EntryName, command)
			count++
		}
		marshalHookType(rawHooks, spec.QwenEvent, matchers)
	}

	if count == 0 && !force {
		return 0, nil
	}
	return count, writeSettings(settingsPath, rawSettings, rawHooks)
}

func (a *Agent) UninstallHooks() error {
	repoRoot := protocol.RepoRoot()
	settingsPath := filepath.Join(repoRoot, ".qwen", settingsFileName)
	if _, err := os.Stat(settingsPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	rawSettings, rawHooks, err := readSettings(settingsPath)
	if err != nil {
		return err
	}
	removeEntireHooks(rawHooks)
	return writeSettings(settingsPath, rawSettings, rawHooks)
}

func (a *Agent) AreHooksInstalled() bool {
	repoRoot := protocol.RepoRoot()
	settingsPath := filepath.Join(repoRoot, ".qwen", settingsFileName)
	_, rawHooks, err := readSettings(settingsPath)
	if err != nil {
		return false
	}
	for _, spec := range hookSpecs {
		var matchers []qwenHookMatcher
		parseHookType(rawHooks, spec.QwenEvent, &matchers)
		if !hookExists(matchers, spec.Matcher, spec.EntryName, hookCommand(spec.HookName)) {
			return false
		}
	}
	return true
}

func parseHookInput(input []byte) (qwenHookInputRaw, error) {
	var raw qwenHookInputRaw
	input = bytes.TrimSpace(input)
	if len(input) == 0 {
		return raw, nil
	}
	if err := json.Unmarshal(input, &raw); err != nil {
		return raw, fmt.Errorf("parse qwen hook input: %w", err)
	}
	return raw, nil
}

func normalizeRawHook(raw *qwenHookInputRaw, hookName string) {
	if strings.TrimSpace(raw.SessionID) == "" {
		raw.SessionID = stubSessionID
	}
	if strings.TrimSpace(raw.Timestamp) == "" {
		raw.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(raw.Model) == "" && raw.LLMRequest.Model != "" {
		raw.Model = raw.LLMRequest.Model
	}
	if raw.ToolName == "" && (hookName == HookNamePostToolUse || hookName == HookNamePostToolUseFailure) {
		raw.ToolName = "unknown"
	}
}

func qwenEventName(hookName string, existing string) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	switch hookName {
	case HookNameSessionStart:
		return "SessionStart"
	case HookNameUserPromptSubmit:
		return "UserPromptSubmit"
	case HookNameStop:
		return "Stop"
	case HookNameStopFailure:
		return "StopFailure"
	case HookNameSessionEnd:
		return "SessionEnd"
	case HookNamePreCompact:
		return "PreCompact"
	case HookNamePostToolUse:
		return "PostToolUse"
	case HookNamePostToolUseFailure:
		return "PostToolUseFailure"
	default:
		return hookName
	}
}

func hookMetadata(raw qwenHookInputRaw) map[string]string {
	metadata := map[string]string{
		"agent":           AgentName,
		"hook_event_name": raw.HookEventName,
	}
	if raw.TranscriptPath != "" {
		metadata["native_transcript_path"] = raw.TranscriptPath
	}
	if raw.CWD != "" {
		metadata["cwd"] = raw.CWD
	}
	if raw.Source != "" {
		metadata["source"] = raw.Source
	}
	if raw.Reason != "" {
		metadata["reason"] = raw.Reason
	}
	if raw.Error != "" {
		metadata["error"] = raw.Error
	}
	return metadata
}

func readSettings(path string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	rawSettings := make(map[string]json.RawMessage)
	rawHooks := make(map[string]json.RawMessage)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rawSettings, rawHooks, nil
		}
		return nil, nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return rawSettings, rawHooks, nil
	}
	if err := json.Unmarshal(data, &rawSettings); err != nil {
		return nil, nil, fmt.Errorf("parse .qwen/settings.json: %w", err)
	}
	if hooksRaw, ok := rawSettings["hooks"]; ok {
		if err := json.Unmarshal(hooksRaw, &rawHooks); err != nil {
			return nil, nil, fmt.Errorf("parse .qwen/settings.json hooks: %w", err)
		}
	}
	return rawSettings, rawHooks, nil
}

func writeSettings(path string, rawSettings map[string]json.RawMessage, rawHooks map[string]json.RawMessage) error {
	hooksJSON, err := json.Marshal(rawHooks)
	if err != nil {
		return err
	}
	rawSettings["hooks"] = hooksJSON
	data, err := json.MarshalIndent(rawSettings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func parseHookType(rawHooks map[string]json.RawMessage, event string, target *[]qwenHookMatcher) {
	if data, ok := rawHooks[event]; ok {
		_ = json.Unmarshal(data, target)
	}
}

func marshalHookType(rawHooks map[string]json.RawMessage, event string, matchers []qwenHookMatcher) {
	if len(matchers) == 0 {
		delete(rawHooks, event)
		return
	}
	data, err := json.Marshal(matchers)
	if err == nil {
		rawHooks[event] = data
	}
}

func hookCommand(hookName string) string {
	return fmt.Sprintf("sh -c 'if command -v entire >/dev/null 2>&1; then entire hooks qwen %s >/dev/null 2>&1 || true; fi'", hookName)
}

func hookExists(matchers []qwenHookMatcher, matcher string, entryName string, command string) bool {
	for _, m := range matchers {
		if m.Matcher != matcher {
			continue
		}
		for _, h := range m.Hooks {
			if h.Type == "command" && h.Name == entryName && h.Command == command {
				return true
			}
		}
	}
	return false
}

func addHook(matchers []qwenHookMatcher, matcher string, entryName string, command string) []qwenHookMatcher {
	for i := range matchers {
		if matchers[i].Matcher == matcher {
			matchers[i].Hooks = append(matchers[i].Hooks, qwenHookEntry{
				Type:    "command",
				Name:    entryName,
				Command: command,
				Timeout: 10000,
			})
			return matchers
		}
	}
	return append(matchers, qwenHookMatcher{
		Matcher: matcher,
		Hooks: []qwenHookEntry{{
			Type:    "command",
			Name:    entryName,
			Command: command,
			Timeout: 10000,
		}},
	})
}

func removeEntireHooks(rawHooks map[string]json.RawMessage) {
	for _, spec := range hookSpecs {
		var matchers []qwenHookMatcher
		parseHookType(rawHooks, spec.QwenEvent, &matchers)
		matchers = removeEntireHooksFromMatchers(matchers)
		marshalHookType(rawHooks, spec.QwenEvent, matchers)
	}
}

func removeEntireHooksFromMatchers(matchers []qwenHookMatcher) []qwenHookMatcher {
	filteredMatchers := make([]qwenHookMatcher, 0, len(matchers))
	for _, matcher := range matchers {
		filteredHooks := make([]qwenHookEntry, 0, len(matcher.Hooks))
		for _, hook := range matcher.Hooks {
			if isEntireHook(hook) {
				continue
			}
			filteredHooks = append(filteredHooks, hook)
		}
		if len(filteredHooks) == 0 {
			continue
		}
		matcher.Hooks = filteredHooks
		filteredMatchers = append(filteredMatchers, matcher)
	}
	return filteredMatchers
}

func isEntireHook(hook qwenHookEntry) bool {
	if strings.HasPrefix(hook.Name, "entire-") {
		return true
	}
	return strings.Contains(hook.Command, "entire hooks qwen")
}
