package gemini

import "encoding/json"

const (
	AgentName        = "gemini"
	AgentType        = "Gemini CLI"
	geminiBinary     = "gemini"
	stubSessionID    = "gemini-session-000"
	settingsFileName = "settings.json"
)

const (
	HookNameSessionStart       = "session-start"
	HookNameBeforeAgent        = "before-agent"
	HookNameAfterAgent         = "after-agent"
	HookNameBeforeTool         = "before-tool"
	HookNameAfterTool          = "after-tool"
	HookNamePreCompress        = "pre-compress"
	HookNameNotification       = "notification"
	HookNameSessionEnd         = "session-end"
)

var hookSpecs = []hookSpec{
	{GeminiEvent: "SessionStart", HookName: HookNameSessionStart, EntryName: "entire-session-start"},
	{GeminiEvent: "BeforeAgent", HookName: HookNameBeforeAgent, EntryName: "entire-before-agent"},
	{GeminiEvent: "AfterAgent", HookName: HookNameAfterAgent, EntryName: "entire-after-agent"},
	{GeminiEvent: "BeforeTool", HookName: HookNameBeforeTool, EntryName: "entire-before-tool", Matcher: "*"},
	{GeminiEvent: "AfterTool", HookName: HookNameAfterTool, EntryName: "entire-after-tool", Matcher: "*"},
	{GeminiEvent: "PreCompress", HookName: HookNamePreCompress, EntryName: "entire-pre-compress"},
	{GeminiEvent: "Notification", HookName: HookNameNotification, EntryName: "entire-notification", Matcher: "*"},
	{GeminiEvent: "SessionEnd", HookName: HookNameSessionEnd, EntryName: "entire-session-end"},
}

type hookSpec struct {
	GeminiEvent string
	HookName    string
	EntryName   string
	Matcher     string
}

// Gemini hook config structure (matches .gemini/settings.json hooks schema)
type geminiHookMatcher struct {
	Matcher    string                     `json:"matcher,omitempty"`
	Hooks      []geminiHookEntry           `json:"hooks"`
	Extra      map[string]json.RawMessage  `json:"-"`
}

type geminiHookEntry struct {
	Type        string                     `json:"type"`
	Command     string                     `json:"command,omitempty"`
	Name        string                     `json:"name,omitempty"`
	Description string                     `json:"description,omitempty"`
	Timeout     int                        `json:"timeout,omitempty"`
	Extra       map[string]json.RawMessage `json:"-"`
}

// Gemini CLI hook input payload (from stdin)
type geminiHookInputRaw struct {
	SessionID            string          `json:"session_id"`
	TranscriptPath       string          `json:"transcript_path"`
	CWD                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	Timestamp            string          `json:"timestamp"`
	Prompt               string          `json:"prompt,omitempty"`
	UserPrompt           string          `json:"user_prompt,omitempty"`
	Model                string          `json:"model,omitempty"`
	LastAssistantMessage string          `json:"last_assistant_message,omitempty"`
	ToolName             string          `json:"tool_name,omitempty"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	ToolInput            json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse         json.RawMessage `json:"tool_response,omitempty"`
	Error                string          `json:"error,omitempty"`
	ErrorDetails         string          `json:"error_details,omitempty"`
	CompactSummary       string          `json:"compact_summary,omitempty"`
	NotificationType     string          `json:"notification_type,omitempty"`
	Message              string          `json:"message,omitempty"`
	Reason               string          `json:"reason,omitempty"`
	Source               string          `json:"source,omitempty"`
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
	ToolName             string          `json:"tool_name,omitempty"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	ToolInput            json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse         json.RawMessage `json:"tool_response,omitempty"`
	Error                string          `json:"error,omitempty"`
	ErrorDetails         string          `json:"error_details,omitempty"`
	LastAssistantMessage string          `json:"last_assistant_message,omitempty"`
	CompactSummary       string          `json:"compact_summary,omitempty"`
	NotificationType     string          `json:"notification_type,omitempty"`
	Message              string          `json:"message,omitempty"`
}
