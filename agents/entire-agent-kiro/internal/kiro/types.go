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

type kiroAgentFile struct {
	Name  string    `json:"name"`
	Tools []string  `json:"tools"`
	Hooks kiroHooks `json:"hooks"`
}

type kiroHooks struct {
	AgentSpawn       []kiroHookEntry `json:"agentSpawn,omitempty"`
	UserPromptSubmit []kiroHookEntry `json:"userPromptSubmit,omitempty"`
	PreToolUse       []kiroHookEntry `json:"preToolUse,omitempty"`
	PostToolUse      []kiroHookEntry `json:"postToolUse,omitempty"`
	Stop             []kiroHookEntry `json:"stop,omitempty"`
}

type kiroHookEntry struct {
	Command string `json:"command"`
}

type kiroIDEHookFile struct {
	Enabled     bool            `json:"enabled"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     string          `json:"version"`
	When        kiroIDEHookWhen `json:"when"`
	Then        kiroIDEHookThen `json:"then"`
}

type kiroIDEHookWhen struct {
	Type string `json:"type"`
}

type kiroIDEHookThen struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type transcriptEnvelope struct {
	ConversationID string            `json:"conversation_id,omitempty"`
	History        []json.RawMessage `json:"history,omitempty"`
}
