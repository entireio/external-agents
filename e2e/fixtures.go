//go:build e2e

package e2e

import (
	"encoding/json"
	"testing"
)

// HookInput builds stdin payloads for hook-related subcommands.
type HookInput struct {
	HookType   string                 `json:"hook_type,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	SessionRef string                 `json:"session_ref,omitempty"`
	Timestamp  string                 `json:"timestamp,omitempty"`
	UserPrompt string                 `json:"user_prompt,omitempty"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolUseID  string                 `json:"tool_use_id,omitempty"`
	ToolInput  json.RawMessage        `json:"tool_input,omitempty"`
	RawData    map[string]interface{} `json:"raw_data,omitempty"`
}

// JSON returns the JSON-encoded string for use as stdin.
func (h HookInput) JSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal HookInput: %v", err)
	}
	return string(data)
}

// ParseHookInput builds stdin payloads for the parse-hook subcommand.
type ParseHookInput struct {
	HookEventName string          `json:"hook_event_name,omitempty"`
	CWD           string          `json:"cwd,omitempty"`
	Prompt        string          `json:"prompt,omitempty"`
	ToolName      string          `json:"tool_name,omitempty"`
	ToolInput     json.RawMessage `json:"tool_input,omitempty"`
}

// JSON returns the JSON-encoded string.
func (p ParseHookInput) JSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal ParseHookInput: %v", err)
	}
	return string(data)
}
