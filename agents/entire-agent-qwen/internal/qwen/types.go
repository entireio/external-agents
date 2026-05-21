package qwen

import "encoding/json"

const (
	AgentName        = "qwen"
	AgentType        = "Qwen Code"
	qwenBinary       = "qwen"
	stubSessionID    = "qwen-session-000"
	settingsFileName = "settings.json"
)

const (
	HookNameSessionStart       = "session-start"
	HookNameUserPromptSubmit   = "user-prompt-submit"
	HookNameStop               = "stop"
	HookNameStopFailure        = "stop-failure"
	HookNameSessionEnd         = "session-end"
	HookNamePreCompact         = "pre-compact"
	HookNamePostToolUse        = "post-tool-use"
	HookNamePostToolUseFailure = "post-tool-use-failure"
)

var hookSpecs = []hookSpec{
	{QwenEvent: "SessionStart", HookName: HookNameSessionStart, EntryName: "entire-session-start"},
	{QwenEvent: "UserPromptSubmit", HookName: HookNameUserPromptSubmit, EntryName: "entire-user-prompt-submit"},
	{QwenEvent: "Stop", HookName: HookNameStop, EntryName: "entire-stop"},
	{QwenEvent: "StopFailure", HookName: HookNameStopFailure, EntryName: "entire-stop-failure"},
	{QwenEvent: "SessionEnd", HookName: HookNameSessionEnd, EntryName: "entire-session-end"},
	{QwenEvent: "PreCompact", HookName: HookNamePreCompact, EntryName: "entire-pre-compact"},
	{QwenEvent: "PostToolUse", HookName: HookNamePostToolUse, EntryName: "entire-post-tool-use", Matcher: "*"},
	{QwenEvent: "PostToolUseFailure", HookName: HookNamePostToolUseFailure, EntryName: "entire-post-tool-use-failure", Matcher: "*"},
}

type hookSpec struct {
	QwenEvent string
	HookName  string
	EntryName string
	Matcher   string
}

type qwenHookMatcher struct {
	Matcher    string          `json:"matcher,omitempty"`
	Sequential *bool           `json:"sequential,omitempty"`
	Hooks      []qwenHookEntry `json:"hooks"`
}

type qwenHookEntry struct {
	Type        string `json:"type"`
	Command     string `json:"command"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
	Async       bool   `json:"async,omitempty"`
	Shell       string `json:"shell,omitempty"`
}

type qwenHookInputRaw struct {
	SessionID            string          `json:"session_id"`
	TranscriptPath       string          `json:"transcript_path"`
	CWD                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	Timestamp            string          `json:"timestamp"`
	Prompt               string          `json:"prompt,omitempty"`
	Model                string          `json:"model,omitempty"`
	Source               string          `json:"source,omitempty"`
	Reason               string          `json:"reason,omitempty"`
	PermissionMode       string          `json:"permission_mode,omitempty"`
	StopHookActive       bool            `json:"stop_hook_active,omitempty"`
	LastAssistantMessage string          `json:"last_assistant_message,omitempty"`
	ToolName             string          `json:"tool_name,omitempty"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	ToolInput            json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse         json.RawMessage `json:"tool_response,omitempty"`
	Error                string          `json:"error,omitempty"`
	ErrorDetails         string          `json:"error_details,omitempty"`
	Trigger              string          `json:"trigger,omitempty"`
	CustomInstructions   string          `json:"custom_instructions,omitempty"`
	CompactSummary       string          `json:"compact_summary,omitempty"`
	LLMRequest           struct {
		Model string `json:"model"`
	} `json:"llm_request,omitempty"`
}

type sidecarRecord struct {
	V                    int             `json:"v"`
	Agent                string          `json:"agent"`
	Event                string          `json:"event"`
	SessionID            string          `json:"session_id"`
	TS                   string          `json:"ts"`
	CWD                  string          `json:"cwd,omitempty"`
	NativeTranscriptPath string          `json:"native_transcript_path,omitempty"`
	Prompt               string          `json:"prompt,omitempty"`
	Model                string          `json:"model,omitempty"`
	Reason               string          `json:"reason,omitempty"`
	Source               string          `json:"source,omitempty"`
	Trigger              string          `json:"trigger,omitempty"`
	ToolName             string          `json:"tool_name,omitempty"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	ToolInput            json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse         json.RawMessage `json:"tool_response,omitempty"`
	Error                string          `json:"error,omitempty"`
	ErrorDetails         string          `json:"error_details,omitempty"`
	LastAssistantMessage string          `json:"last_assistant_message,omitempty"`
	CompactSummary       string          `json:"compact_summary,omitempty"`
}
