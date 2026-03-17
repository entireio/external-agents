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

type kiroTranscript struct {
	ConversationID string             `json:"conversation_id"`
	History        []kiroHistoryEntry `json:"history"`
}

type kiroHistoryEntry struct {
	User      kiroUserMessage `json:"user"`
	Assistant json.RawMessage `json:"assistant"`
}

type kiroUserMessage struct {
	Content   json.RawMessage `json:"content"`
	Timestamp string          `json:"timestamp,omitempty"`
}

type kiroPromptContent struct {
	Prompt struct {
		Prompt string `json:"prompt"`
	} `json:"Prompt"`
}

type kiroToolUseContent struct {
	ToolUse kiroToolUsePayload `json:"ToolUse"`
}

type kiroToolUsePayload struct {
	MessageID string         `json:"message_id"`
	ToolUses  []kiroToolCall `json:"tool_uses"`
}

type kiroResponseContent struct {
	Response kiroResponsePayload `json:"Response"`
}

type kiroResponsePayload struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
}

type kiroToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type kiroIDETranscript struct {
	History []kiroIDEHistoryEntry `json:"history"`
}

type kiroIDEHistoryEntry struct {
	Message kiroIDEMessage `json:"message"`
}

type kiroIDEMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type kiroIDEContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
