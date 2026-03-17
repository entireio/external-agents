package kiro

import "encoding/json"

const (
	HookNameAgentSpawn       = "agent-spawn"
	HookNameUserPromptSubmit = "user-prompt-submit"
	HookNamePreToolUse       = "pre-tool-use"
	HookNamePostToolUse      = "post-tool-use"
	HookNameStop             = "stop"
)

type hookInputRaw struct {
	HookEventName string `json:"hook_event_name"`
	CWD           string `json:"cwd"`
	Prompt        string `json:"prompt,omitempty"`
	ToolName      string `json:"tool_name,omitempty"`
	ToolInput     string `json:"tool_input,omitempty"`
	ToolResponse  string `json:"tool_response,omitempty"`
}

type transcriptEnvelope struct {
	ConversationID string            `json:"conversation_id,omitempty"`
	History        []json.RawMessage `json:"history,omitempty"`
}
