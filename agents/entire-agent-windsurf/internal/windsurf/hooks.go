package windsurf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-windsurf/internal/protocol"
)

const (
	HookNamePreUserPrompt       = "pre_user_prompt"
	HookNamePostWriteCode       = "post_write_code"
	HookNamePostCascadeResponse = "post_cascade_response"
	HookNamePostCascadeResponseWithTranscript = "post_cascade_response_with_transcript"
	hooksRelativePath           = ".windsurf/hooks.json"
)

var hookNames = []string{HookNamePreUserPrompt, HookNamePostWriteCode, HookNamePostCascadeResponse, HookNamePostCascadeResponseWithTranscript}

// cascadeHookPayload is the documented common Cascade hook payload. ToolInfo
// stays raw so Member 3 can consume event-specific context without this layer
// interpreting it.
type cascadeHookPayload struct {
	AgentActionName string          `json:"agent_action_name"`
	TrajectoryID    string          `json:"trajectory_id"`
	ExecutionID     string          `json:"execution_id"`
	Timestamp       string          `json:"timestamp"`
	ToolInfo        json.RawMessage `json:"tool_info"`
}

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	inputEvent, err := ParseLifecycleEvent(hookName, input)
	if err != nil || inputEvent == nil { return nil, err }
	if hookName == HookNamePreUserPrompt {
		if strings.TrimSpace(inputEvent.Prompt) == "" { return nil, nil }
		return NormalizeEvent(2, *inputEvent), nil
	}
	if hookName == HookNamePostCascadeResponse { return NormalizeEvent(3, *inputEvent), nil }
	if hookName == HookNamePostCascadeResponseWithTranscript {
		if strings.TrimSpace(inputEvent.SessionRef) == "" { return nil, nil }
		return NormalizeEvent(3, *inputEvent), nil
	}
	// The v1 Entire protocol has no code-write event. The exported lifecycle
	// parser preserves this event for Member 3 without falsely ending a turn.
	return nil, nil
}

// ParseLifecycleEvent is the native hook boundary. It preserves the documented
// tool_info object verbatim in metadata for downstream context extraction.
func ParseLifecycleEvent(hookName string, input []byte) (*LifecycleEvent, error) {
	if !isSupportedHook(hookName) || len(bytes.TrimSpace(input)) == 0 {
		return nil, nil
	}
	var payload cascadeHookPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("parse Windsurf hook payload: %w", err)
	}
	if payload.AgentActionName != "" && payload.AgentActionName != hookName {
		return nil, nil
	}
	if strings.TrimSpace(payload.TrajectoryID) == "" {
		return nil, nil
	}
	inputEvent := &LifecycleEvent{
		Name: hookName, TrajectoryID: payload.TrajectoryID, ExecutionID: payload.ExecutionID,
		Timestamp: payload.Timestamp, Metadata: map[string]string{},
	}
	if len(bytes.TrimSpace(payload.ToolInfo)) != 0 { inputEvent.Metadata["windsurf_tool_info"] = string(payload.ToolInfo) }
	if hookName == HookNamePreUserPrompt {
		inputEvent.Prompt = toolInfoString(payload.ToolInfo, "user_prompt")
	}
	if hookName == HookNamePostCascadeResponseWithTranscript {
		inputEvent.SessionRef = toolInfoString(payload.ToolInfo, "transcript_path")
	}
	return inputEvent, nil
}

func toolInfoString(raw json.RawMessage, key string) string {
	var values map[string]interface{}
	if json.Unmarshal(raw, &values) != nil { return "" }
	value, _ := values[key].(string)
	return value
}

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	path := filepath.Join(protocol.RepoRoot(), hooksRelativePath)
	root, hooks, err := readHooksConfig(path)
	if err != nil { return 0, err }
	changed := 0
	for _, name := range hookNames {
		entries, err := hookEntries(hooks, name)
		if err != nil { return 0, err }
		command := hookCommand(name)
		found := false
		filtered := make([]json.RawMessage, 0, len(entries)+1)
		for _, entry := range entries {
			if isEntireHook(entry, name) {
				if !found && !force { filtered = append(filtered, hookEntry(command)); found = true }
				continue
			}
			filtered = append(filtered, entry)
		}
		if !found { filtered = append(filtered, hookEntry(command)); changed++ }
		hooks[name], _ = json.Marshal(filtered)
	}
	if changed == 0 && !force { return 0, nil }
	root["hooks"], _ = json.Marshal(hooks)
	return changed, writeJSON(path, root)
}

func (a *Agent) UninstallHooks() error {
	path := filepath.Join(protocol.RepoRoot(), hooksRelativePath)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) { return nil } else if err != nil { return err }
	root, hooks, err := readHooksConfig(path)
	if err != nil { return err }
	for _, name := range hookNames {
		entries, err := hookEntries(hooks, name)
		if err != nil { return err }
		filtered := make([]json.RawMessage, 0, len(entries))
		for _, entry := range entries { if !isEntireHook(entry, name) { filtered = append(filtered, entry) } }
		if len(filtered) == 0 { delete(hooks, name) } else { hooks[name], _ = json.Marshal(filtered) }
	}
	if len(hooks) == 0 { delete(root, "hooks") } else { root["hooks"], _ = json.Marshal(hooks) }
	return writeJSON(path, root)
}

func (a *Agent) AreHooksInstalled() bool {
	_, hooks, err := readHooksConfig(filepath.Join(protocol.RepoRoot(), hooksRelativePath))
	if err != nil { return false }
	for _, name := range hookNames {
		entries, err := hookEntries(hooks, name)
		if err != nil { return false }
		found := false
		for _, entry := range entries { if isEntireHook(entry, name) { found = true; break } }
		if !found { return false }
	}
	return true
}

func readHooksConfig(path string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil { if errors.Is(err, os.ErrNotExist) { return map[string]json.RawMessage{}, map[string]json.RawMessage{}, nil }; return nil, nil, err }
	root := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(data)) > 0 { if err := json.Unmarshal(data, &root); err != nil { return nil, nil, fmt.Errorf("parse %s: %w", hooksRelativePath, err) } }
	hooks := map[string]json.RawMessage{}
	if raw, ok := root["hooks"]; ok { if err := json.Unmarshal(raw, &hooks); err != nil { return nil, nil, fmt.Errorf("parse %s hooks: %w", hooksRelativePath, err) } }
	return root, hooks, nil
}

func hookEntries(hooks map[string]json.RawMessage, name string) ([]json.RawMessage, error) {
	if raw, ok := hooks[name]; ok { var entries []json.RawMessage; if err := json.Unmarshal(raw, &entries); err != nil { return nil, fmt.Errorf("parse %s hook entries: %w", name, err) }; return entries, nil }
	return nil, nil
}
func hookCommand(name string) string { return "entire hooks windsurf " + name }
func hookEntry(command string) json.RawMessage { data, _ := json.Marshal(map[string]interface{}{ "command": command, "show_output": false }); return data }
func isEntireHook(raw json.RawMessage, name string) bool { var entry struct { Command string `json:"command"`; PowerShell string `json:"powershell"` }; return json.Unmarshal(raw, &entry) == nil && (entry.Command == hookCommand(name) || entry.PowerShell == hookCommand(name)) }
func isSupportedHook(name string) bool { for _, supported := range hookNames { if name == supported { return true } }; return false }
func writeJSON(path string, value map[string]json.RawMessage) error { data, err := json.MarshalIndent(value, "", "  "); if err != nil { return err }; data = append(data, '\n'); if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { return err }; return os.WriteFile(path, data, 0o600) }
