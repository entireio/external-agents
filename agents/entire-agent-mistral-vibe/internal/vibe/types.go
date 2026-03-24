package vibe

import "encoding/json"

// Protocol hook names (kebab-case, used by the Entire CLI protocol).
const (
	HookNameSessionStart      = "session-start"
	HookNameUserPromptSubmit  = "user-prompt-submit"
	HookNamePreToolUse        = "pre-tool-use"
	HookNamePostToolUse       = "post-tool-use"
	HookNameTurnEnd           = "turn-end"
)

// Vibe native hook names (underscore, used by Vibe internally).
const (
	VibeNativeSessionStart     = "session_start"
	VibeNativeUserPromptSubmit = "user_prompt_submit"
	VibeNativePreToolUse       = "pre_tool_use"
	VibeNativePostToolUse      = "post_tool_use"
	VibeNativeTurnEnd          = "turn_end"
)

// NativeToProtocolHook maps Vibe's native underscore hook names to the
// kebab-case protocol hook names used by the Entire CLI.
var NativeToProtocolHook = map[string]string{
	VibeNativeSessionStart:     HookNameSessionStart,
	VibeNativeUserPromptSubmit: HookNameUserPromptSubmit,
	VibeNativePreToolUse:       HookNamePreToolUse,
	VibeNativePostToolUse:      HookNamePostToolUse,
	VibeNativeTurnEnd:          HookNameTurnEnd,
}

// VibeHookPayload is the JSON payload Vibe sends on stdin for lifecycle hooks.
type VibeHookPayload struct {
	HookEventName string          `json:"hook_event_name"`
	CWD           string          `json:"cwd"`
	SessionID     string          `json:"session_id"`
	Prompt        string          `json:"prompt,omitempty"`
	ToolName      string          `json:"tool_name,omitempty"`
	ToolInput     json.RawMessage `json:"tool_input,omitempty"`
	ToolOutcome   string          `json:"tool_outcome,omitempty"`
	ToolResponse  json.RawMessage `json:"tool_response,omitempty"`
	ToolError     string          `json:"tool_error,omitempty"`
}

// VibeMessage represents a single line in the Vibe JSONL transcript.
type VibeMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []VibeToolCall `json:"tool_calls,omitempty"`
	MessageID  string         `json:"message_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// VibeToolCall represents a tool invocation within a Vibe transcript message.
type VibeToolCall struct {
	ID       string           `json:"id"`
	Index    int              `json:"index"`
	Function VibeToolFunction `json:"function"`
	Type     string           `json:"type"`
}

// VibeToolFunction holds the function name and arguments for a tool call.
type VibeToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// VibeSessionMeta represents the meta.json file in a Vibe session directory.
type VibeSessionMeta struct {
	SessionID     string           `json:"session_id"`
	StartTime     string           `json:"start_time"`
	EndTime       string           `json:"end_time,omitempty"`
	Title         string           `json:"title,omitempty"`
	TotalMessages int              `json:"total_messages"`
	GitCommit     string           `json:"git_commit,omitempty"`
	GitBranch     string           `json:"git_branch,omitempty"`
	Environment   string           `json:"environment,omitempty"`
	Stats         VibeSessionStats `json:"stats,omitempty"`
}

// VibeSessionStats holds aggregate statistics for a Vibe session.
type VibeSessionStats struct {
	TotalTokens  int `json:"total_tokens,omitempty"`
	PromptTokens int `json:"prompt_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}
