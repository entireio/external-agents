package grok

import "encoding/json"

const (
	AgentName     = "grok"
	AgentType     = "Grok Build"
	grokBinary    = "grok"
	stubSessionID = "grok-session-000"
	hooksFileName = "entire.json"

	restoredSummaryTitle = "Restored by Entire"
)

// grokSessionSummary is the subset of Grok's summary.json needed for it to
// recognize and load a restored session directory.
type grokSessionSummary struct {
	Info              grokSessionSummaryInfo `json:"info"`
	SessionSummary    string                 `json:"session_summary"`
	CurrentModelID    string                 `json:"current_model_id,omitempty"`
	CreatedAt         string                 `json:"created_at"`
	UpdatedAt         string                 `json:"updated_at"`
	LastActiveAt      string                 `json:"last_active_at"`
	NumMessages       int                    `json:"num_messages"`
	NumChatMessages   int                    `json:"num_chat_messages"`
	ChatFormatVersion int                    `json:"chat_format_version"`
	GitRootDir        string                 `json:"git_root_dir"`
	GrokHome          string                 `json:"grok_home"`
	GeneratedTitle    string                 `json:"generated_title"`
}

type grokSessionSummaryInfo struct {
	ID  string `json:"id"`
	CWD string `json:"cwd"`
}

const (
	HookNameSessionStart       = "session-start"
	HookNameUserPromptSubmit   = "user-prompt-submit"
	HookNamePreToolUse         = "pre-tool-use"
	HookNameStop               = "stop"
	HookNameStopCancelled      = "stop-cancelled"
	HookNameStopFailure        = "stop-failure"
	HookNameSessionEnd         = "session-end"
	HookNamePreCompact         = "pre-compact"
	HookNamePostCompact        = "post-compact"
	HookNamePostToolUse        = "post-tool-use"
	HookNamePostToolUseFailure = "post-tool-use-failure"
	HookNameNotification       = "notification"
	HookNamePermissionRequest  = "permission-request"
	HookNameSubagentStart      = "subagent-start"
	HookNameSubagentStop       = "subagent-stop"
)

var hookSpecs = []hookSpec{
	{GrokEvent: "SessionStart", HookName: HookNameSessionStart, EntryName: "entire-session-start"},
	{GrokEvent: "UserPromptSubmit", HookName: HookNameUserPromptSubmit, EntryName: "entire-user-prompt-submit"},
	{GrokEvent: "PreToolUse", HookName: HookNamePreToolUse, EntryName: "entire-pre-tool-use", Matcher: "*"},
	{GrokEvent: "Stop", HookName: HookNameStop, EntryName: "entire-stop"},
	// Grok fires StopCancelled instead of Stop when a turn ends without completing:
	// user interrupt (Ctrl+C/Esc), a declined permission prompt, --max-turns, or a
	// no-progress bail-out. Without this the turn produces no end-of-turn checkpoint.
	{GrokEvent: "StopCancelled", HookName: HookNameStopCancelled, EntryName: "entire-stop-cancelled"},
	{GrokEvent: "StopFailure", HookName: HookNameStopFailure, EntryName: "entire-stop-failure"},
	{GrokEvent: "SessionEnd", HookName: HookNameSessionEnd, EntryName: "entire-session-end"},
	{GrokEvent: "PreCompact", HookName: HookNamePreCompact, EntryName: "entire-pre-compact"},
	{GrokEvent: "PostCompact", HookName: HookNamePostCompact, EntryName: "entire-post-compact"},
	{GrokEvent: "PostToolUse", HookName: HookNamePostToolUse, EntryName: "entire-post-tool-use", Matcher: "*"},
	{GrokEvent: "PostToolUseFailure", HookName: HookNamePostToolUseFailure, EntryName: "entire-post-tool-use-failure", Matcher: "*"},
	{GrokEvent: "Notification", HookName: HookNameNotification, EntryName: "entire-notification", Matcher: "*"},
	{GrokEvent: "PermissionDenied", HookName: HookNamePermissionRequest, EntryName: "entire-permission-request", Matcher: "*"},
	{GrokEvent: "SubagentStart", HookName: HookNameSubagentStart, EntryName: "entire-subagent-start", Matcher: "*"},
	{GrokEvent: "SubagentStop", HookName: HookNameSubagentStop, EntryName: "entire-subagent-stop", Matcher: "*"},
}

type hookSpec struct {
	GrokEvent string
	HookName  string
	EntryName string
	Matcher   string
}

type grokHookMatcher struct {
	Matcher    string                     `json:"matcher,omitempty"`
	Sequential *bool                      `json:"sequential,omitempty"`
	Hooks      []grokHookEntry            `json:"hooks"`
	Extra      map[string]json.RawMessage `json:"-"`
}

type grokHookEntry struct {
	Type        string                     `json:"type"`
	Command     string                     `json:"command,omitempty"`
	Name        string                     `json:"name,omitempty"`
	Description string                     `json:"description,omitempty"`
	Timeout     int                        `json:"timeout,omitempty"`
	Async       bool                       `json:"async,omitempty"`
	Shell       string                     `json:"shell,omitempty"`
	Extra       map[string]json.RawMessage `json:"-"`
}

type grokHookInputRaw struct {
	SessionID            string          `json:"session_id"`
	SessionIDCamel       string          `json:"sessionId"`
	TranscriptPath       string          `json:"transcript_path"`
	CWD                  string          `json:"cwd"`
	WorkspaceRoot        string          `json:"workspaceRoot"`
	HookEventName        string          `json:"hook_event_name"`
	HookEventNameCamel   string          `json:"hookEventName"`
	Timestamp            string          `json:"timestamp"`
	Prompt               string          `json:"prompt,omitempty"`
	UserPrompt           string          `json:"user_prompt,omitempty"`
	Model                string          `json:"model,omitempty"`
	Source               string          `json:"source,omitempty"`
	Reason               string          `json:"reason,omitempty"`
	PermissionMode       string          `json:"permission_mode,omitempty"`
	StopHookActive       bool            `json:"stop_hook_active,omitempty"`
	LastAssistantMessage string          `json:"last_assistant_message,omitempty"`
	ToolName             string          `json:"tool_name,omitempty"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	ToolInput            json.RawMessage `json:"tool_input,omitempty"`
	Input                json.RawMessage `json:"input,omitempty"`
	Inputs               json.RawMessage `json:"inputs,omitempty"`
	ToolResponse         json.RawMessage `json:"tool_response,omitempty"`
	ToolResult           json.RawMessage `json:"tool_result,omitempty"`
	Response             json.RawMessage `json:"response,omitempty"`
	Error                string          `json:"error,omitempty"`
	ErrorType            string          `json:"error_type,omitempty"`
	ErrorDetails         string          `json:"error_details,omitempty"`
	IsInterrupt          bool            `json:"is_interrupt,omitempty"`
	IsTimeout            bool            `json:"is_timeout,omitempty"`
	Trigger              string          `json:"trigger,omitempty"`
	NotificationType     string          `json:"notification_type,omitempty"`
	Message              string          `json:"message,omitempty"`
	AgentID              string          `json:"agent_id,omitempty"`
	AgentType            string          `json:"agent_type,omitempty"`
	AgentTranscriptPath  string          `json:"agent_transcript_path,omitempty"`
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
	PermissionMode       string          `json:"permission_mode,omitempty"`
	StopHookActive       bool            `json:"stop_hook_active,omitempty"`
	Trigger              string          `json:"trigger,omitempty"`
	NotificationType     string          `json:"notification_type,omitempty"`
	Message              string          `json:"message,omitempty"`
	AgentID              string          `json:"agent_id,omitempty"`
	AgentType            string          `json:"agent_type,omitempty"`
	AgentTranscriptPath  string          `json:"agent_transcript_path,omitempty"`
	ToolName             string          `json:"tool_name,omitempty"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	ToolInput            json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse         json.RawMessage `json:"tool_response,omitempty"`
	Error                string          `json:"error,omitempty"`
	ErrorType            string          `json:"error_type,omitempty"`
	ErrorDetails         string          `json:"error_details,omitempty"`
	IsInterrupt          bool            `json:"is_interrupt,omitempty"`
	IsTimeout            bool            `json:"is_timeout,omitempty"`
	LastAssistantMessage string          `json:"last_assistant_message,omitempty"`
	CustomInstructions   string          `json:"custom_instructions,omitempty"`
	CompactSummary       string          `json:"compact_summary,omitempty"`
}
