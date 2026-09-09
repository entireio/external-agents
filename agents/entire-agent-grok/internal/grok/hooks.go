package grok

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

func hooksFilePath(repoRoot string) string {
	return filepath.Join(repoRoot, ".grok", "hooks", hooksFileName)
}

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	raw, err := parseHookInput(input)
	if err != nil {
		return nil, err
	}
	raw.HookEventName = grokEventName(hookName, raw.HookEventName, raw.HookEventNameCamel)
	normalizeRawHook(&raw, hookName)

	repoPath := raw.CWD
	if strings.TrimSpace(repoPath) == "" {
		repoPath = protocol.RepoRoot()
	}
	sessionRef := a.resolveSessionRef(raw.SessionID, repoPath)
	if err := a.writeSessionMarker(raw, sessionRef); err != nil {
		return nil, err
	}
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
		prompt := unwrapUserQuery(raw.Prompt)
		if prompt == "" {
			return nil, nil
		}
		return &protocol.EventJSON{
			Type:       2,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Prompt:     prompt,
			Model:      raw.Model,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	case HookNameStop, HookNameStopCancelled:
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
	case HookNamePostCompact:
		return &protocol.EventJSON{
			Type:            4,
			SessionID:       raw.SessionID,
			SessionRef:      sessionRef,
			Timestamp:       raw.Timestamp,
			ResponseMessage: raw.CompactSummary,
			Metadata:        metadata,
		}, nil
	case HookNamePreToolUse,
		HookNamePostToolUse,
		HookNamePostToolUseFailure,
		HookNameNotification,
		HookNamePermissionRequest,
		HookNameSubagentStart,
		HookNameSubagentStop:
		return nil, nil
	default:
		return nil, nil
	}
}

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	repoRoot := protocol.RepoRoot()
	path := hooksFilePath(repoRoot)

	rawHooks, err := readHookFile(path)
	if err != nil {
		return 0, err
	}

	if force {
		removeEntireHooks(rawHooks)
	}

	count := 0
	for _, spec := range hookSpecs {
		var matchers []grokHookMatcher
		parseHookType(rawHooks, spec.GrokEvent, &matchers)
		command := hookCommand(spec.HookName)
		var changed bool
		matchers, changed = upsertHook(matchers, spec.Matcher, spec.EntryName, command)
		if changed {
			count++
		}
		marshalHookType(rawHooks, spec.GrokEvent, matchers)
	}

	if count == 0 && !force {
		return 0, nil
	}
	return count, writeHookFile(path, rawHooks)
}

func (a *Agent) UninstallHooks() error {
	repoRoot := protocol.RepoRoot()
	path := hooksFilePath(repoRoot)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	rawHooks, err := readHookFile(path)
	if err != nil {
		return err
	}
	removeEntireHooks(rawHooks)
	return writeHookFile(path, rawHooks)
}

func (a *Agent) AreHooksInstalled() bool {
	repoRoot := protocol.RepoRoot()
	rawHooks, err := readHookFile(hooksFilePath(repoRoot))
	if err != nil {
		return false
	}
	for _, spec := range hookSpecs {
		var matchers []grokHookMatcher
		parseHookType(rawHooks, spec.GrokEvent, &matchers)
		if !hookExists(matchers, spec.Matcher, spec.EntryName) {
			return false
		}
	}
	return true
}

func parseHookInput(input []byte) (grokHookInputRaw, error) {
	var raw grokHookInputRaw
	input = bytes.TrimSpace(input)
	if len(input) == 0 {
		return raw, nil
	}
	if err := json.Unmarshal(input, &raw); err != nil {
		return raw, fmt.Errorf("parse grok hook input: %w", err)
	}
	return raw, nil
}

func normalizeRawHook(raw *grokHookInputRaw, hookName string) {
	if strings.TrimSpace(raw.SessionID) == "" {
		raw.SessionID = strings.TrimSpace(raw.SessionIDCamel)
	}
	if strings.TrimSpace(raw.SessionID) == "" {
		raw.SessionID = stubSessionID
	}
	if strings.TrimSpace(raw.Timestamp) == "" {
		raw.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(raw.CWD) == "" {
		raw.CWD = raw.WorkspaceRoot
	}
	if strings.TrimSpace(raw.Prompt) == "" {
		raw.Prompt = raw.UserPrompt
	}
	if strings.TrimSpace(raw.Model) == "" && raw.LLMRequest.Model != "" {
		raw.Model = raw.LLMRequest.Model
	}
	if len(bytes.TrimSpace(raw.ToolInput)) == 0 {
		raw.ToolInput = firstRawMessage(raw.Inputs, raw.Input)
	}
	if len(bytes.TrimSpace(raw.ToolResponse)) == 0 {
		raw.ToolResponse = firstRawMessage(raw.ToolResponse, raw.ToolResult, raw.Response)
	}
	if raw.ToolName == "" && (hookName == HookNamePostToolUse || hookName == HookNamePostToolUseFailure) {
		raw.ToolName = "unknown"
	}
}

func grokEventName(hookName string, existing string, existingCamel string) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	if strings.TrimSpace(existingCamel) != "" {
		return existingCamel
	}
	switch hookName {
	case HookNameSessionStart:
		return "SessionStart"
	case HookNameUserPromptSubmit:
		return "UserPromptSubmit"
	case HookNamePreToolUse:
		return "PreToolUse"
	case HookNameStop:
		return "Stop"
	case HookNameStopCancelled:
		return "StopCancelled"
	case HookNameStopFailure:
		return "StopFailure"
	case HookNameSessionEnd:
		return "SessionEnd"
	case HookNamePreCompact:
		return "PreCompact"
	case HookNamePostCompact:
		return "PostCompact"
	case HookNamePostToolUse:
		return "PostToolUse"
	case HookNamePostToolUseFailure:
		return "PostToolUseFailure"
	case HookNameNotification:
		return "Notification"
	case HookNamePermissionRequest:
		return "PermissionDenied"
	case HookNameSubagentStart:
		return "SubagentStart"
	case HookNameSubagentStop:
		return "SubagentStop"
	default:
		return hookName
	}
}

func hookMetadata(raw grokHookInputRaw) map[string]string {
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
	if raw.PermissionMode != "" {
		metadata["permission_mode"] = raw.PermissionMode
	}
	if raw.Reason != "" {
		metadata["reason"] = raw.Reason
	}
	if raw.Error != "" {
		metadata["error"] = raw.Error
	}
	if raw.ErrorType != "" {
		metadata["error_type"] = raw.ErrorType
	}
	if raw.Trigger != "" {
		metadata["trigger"] = raw.Trigger
	}
	if raw.NotificationType != "" {
		metadata["notification_type"] = raw.NotificationType
	}
	if raw.AgentID != "" {
		metadata["agent_id"] = raw.AgentID
	}
	if raw.AgentType != "" {
		metadata["agent_type"] = raw.AgentType
	}
	if raw.AgentTranscriptPath != "" {
		metadata["agent_transcript_path"] = raw.AgentTranscriptPath
	}
	if raw.CustomInstructions != "" {
		metadata["custom_instructions"] = raw.CustomInstructions
	}
	if raw.IsInterrupt {
		metadata["is_interrupt"] = "true"
	}
	if raw.IsTimeout {
		metadata["is_timeout"] = "true"
	}
	return metadata
}

func readHookFile(path string) (map[string]json.RawMessage, error) {
	rawHooks := make(map[string]json.RawMessage)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rawHooks, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return rawHooks, nil
	}
	var root struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if root.Hooks != nil {
		rawHooks = root.Hooks
	}
	return rawHooks, nil
}

func writeHookFile(path string, rawHooks map[string]json.RawMessage) error {
	root := map[string]any{"hooks": rawHooks}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(rawHooks) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func parseHookType(rawHooks map[string]json.RawMessage, event string, target *[]grokHookMatcher) {
	if data, ok := rawHooks[event]; ok {
		_ = json.Unmarshal(data, target)
	}
}

func marshalHookType(rawHooks map[string]json.RawMessage, event string, matchers []grokHookMatcher) {
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
	entire := "entire"
	if path, err := exec.LookPath("entire"); err == nil && strings.TrimSpace(path) != "" {
		entire = path
	}
	return fmt.Sprintf("sh -c 'if command -v %s >/dev/null 2>&1; then %s hooks grok %s >/dev/null 2>&1 || true; fi'", shellQuote(entire), shellQuote(entire), hookName)
}

func hookExists(matchers []grokHookMatcher, matcher string, entryName string) bool {
	for _, m := range matchers {
		if m.Matcher != matcher {
			continue
		}
		for _, h := range m.Hooks {
			if h.Type == "command" && h.Name == entryName && isEntireHook(h) {
				return true
			}
		}
	}
	return false
}

func upsertHook(matchers []grokHookMatcher, matcher string, entryName string, command string) ([]grokHookMatcher, bool) {
	for i := range matchers {
		if matchers[i].Matcher == matcher {
			for j := range matchers[i].Hooks {
				if isEntireHook(matchers[i].Hooks[j]) && matchers[i].Hooks[j].Name == entryName {
					matchers[i].Hooks[j] = entireHookEntry(entryName, command)
					return matchers, false
				}
			}
			matchers[i].Hooks = append(matchers[i].Hooks, entireHookEntry(entryName, command))
			return matchers, true
		}
	}
	return append(matchers, grokHookMatcher{
		Matcher: matcher,
		Hooks:   []grokHookEntry{entireHookEntry(entryName, command)},
	}), true
}

func entireHookEntry(entryName string, command string) grokHookEntry {
	return grokHookEntry{
		Type:    "command",
		Name:    entryName,
		Command: command,
		Timeout: 10000,
	}
}

func removeEntireHooks(rawHooks map[string]json.RawMessage) {
	for _, spec := range hookSpecs {
		var matchers []grokHookMatcher
		parseHookType(rawHooks, spec.GrokEvent, &matchers)
		matchers = removeEntireHooksFromMatchers(matchers)
		marshalHookType(rawHooks, spec.GrokEvent, matchers)
	}
}

func removeEntireHooksFromMatchers(matchers []grokHookMatcher) []grokHookMatcher {
	filteredMatchers := make([]grokHookMatcher, 0, len(matchers))
	for _, matcher := range matchers {
		filteredHooks := make([]grokHookEntry, 0, len(matcher.Hooks))
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

func isEntireHook(hook grokHookEntry) bool {
	if strings.HasPrefix(hook.Name, "entire-") {
		return true
	}
	return strings.Contains(hook.Command, "entire hooks grok")
}

func firstRawMessage(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(bytes.TrimSpace(value)) != 0 {
			return value
		}
	}
	return nil
}

func (m *grokHookMatcher) UnmarshalJSON(data []byte) error {
	type matcherFields struct {
		Matcher    string          `json:"matcher,omitempty"`
		Sequential *bool           `json:"sequential,omitempty"`
		Hooks      []grokHookEntry `json:"hooks"`
	}
	var fields matcherFields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	extra, err := rawObjectWithout(data, "matcher", "sequential", "hooks")
	if err != nil {
		return err
	}
	m.Matcher = fields.Matcher
	m.Sequential = fields.Sequential
	m.Hooks = fields.Hooks
	m.Extra = extra
	return nil
}

func (m grokHookMatcher) MarshalJSON() ([]byte, error) {
	raw := copyRawMap(m.Extra)
	if m.Matcher != "" {
		putJSON(raw, "matcher", m.Matcher)
	}
	if m.Sequential != nil {
		putJSON(raw, "sequential", m.Sequential)
	}
	putJSON(raw, "hooks", m.Hooks)
	return json.Marshal(raw)
}

func (h *grokHookEntry) UnmarshalJSON(data []byte) error {
	type hookFields struct {
		Type        string `json:"type"`
		Command     string `json:"command,omitempty"`
		Name        string `json:"name,omitempty"`
		Description string `json:"description,omitempty"`
		Timeout     int    `json:"timeout,omitempty"`
		Async       bool   `json:"async,omitempty"`
		Shell       string `json:"shell,omitempty"`
	}
	var fields hookFields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	extra, err := rawObjectWithout(data, "type", "command", "name", "description", "timeout", "async", "shell")
	if err != nil {
		return err
	}
	h.Type = fields.Type
	h.Command = fields.Command
	h.Name = fields.Name
	h.Description = fields.Description
	h.Timeout = fields.Timeout
	h.Async = fields.Async
	h.Shell = fields.Shell
	h.Extra = extra
	return nil
}

func (h grokHookEntry) MarshalJSON() ([]byte, error) {
	raw := copyRawMap(h.Extra)
	if h.Type != "" {
		putJSON(raw, "type", h.Type)
	}
	if h.Command != "" {
		putJSON(raw, "command", h.Command)
	}
	if h.Name != "" {
		putJSON(raw, "name", h.Name)
	}
	if h.Description != "" {
		putJSON(raw, "description", h.Description)
	}
	if h.Timeout != 0 {
		putJSON(raw, "timeout", h.Timeout)
	}
	if h.Async {
		putJSON(raw, "async", h.Async)
	}
	if h.Shell != "" {
		putJSON(raw, "shell", h.Shell)
	}
	return json.Marshal(raw)
}

func rawObjectWithout(data []byte, keys ...string) (map[string]json.RawMessage, error) {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	for _, key := range keys {
		delete(raw, key)
	}
	return raw, nil
}

func copyRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in)+4)
	for key, value := range in {
		out[key] = copyRawMessage(value)
	}
	return out
}

func putJSON(raw map[string]json.RawMessage, key string, value any) {
	data, err := json.Marshal(value)
	if err == nil {
		raw[key] = data
	}
}
