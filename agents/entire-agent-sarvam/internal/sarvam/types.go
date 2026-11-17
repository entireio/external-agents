package sarvam

import (
	"encoding/json"
	"errors"
)

const (
	AgentName     = "sarvam"
	AgentType     = "Sarvam Code"
	sarvamBinary  = "sarvam-code"
	stubSessionID = "sarvam-session-000"
)

var (
	errNegativeOffset    = errors.New("offset must not be negative")
	errMissingSessionRef = errors.New("session_ref or session_id is required")
	errEmptySessionData  = errors.New("refusing to write an empty session snapshot")
)

// Protocol hook names (kebab-case) and the sarvam-native event they ride on.
type hookSpec struct {
	SarvamEvent string // PascalCase event key in .sarvam/config.toml
	HookName    string // kebab-case name Entire calls us with
	EntryName   string // stable name for our hook entries
	EventType   int    // protocol EventJSON type
}

// hookSpecs lists the events we install. Sarvam also has PreToolUse,
// PostToolUse and PermissionRequest, but Entire has no event type for
// tool/permission activity, and the native rollout transcript already
// records tool calls, so installing them would only add /hooks review
// burden without feeding Entire anything it consumes.
var hookSpecs = []hookSpec{
	{SarvamEvent: "SessionStart", HookName: "session-start", EntryName: "entire-session-start", EventType: 1},
	{SarvamEvent: "UserPromptSubmit", HookName: "user-prompt-submit", EntryName: "entire-user-prompt-submit", EventType: 2},
	{SarvamEvent: "Stop", HookName: "stop", EntryName: "entire-stop", EventType: 3},
	{SarvamEvent: "PreCompact", HookName: "pre-compact", EntryName: "entire-pre-compact", EventType: 4},
	{SarvamEvent: "PostCompact", HookName: "post-compact", EntryName: "entire-post-compact", EventType: 4},
}

// sarvamHookInput is the JSON a sarvam-code lifecycle hook receives on stdin.
// Field set verified against live payloads from sarvam-code 0.58.0; see
// testdata/hook-payloads/. No event carries all fields.
type sarvamHookInput struct {
	SessionID            string          `json:"session_id"`
	TurnID               string          `json:"turn_id"`
	TranscriptPath       string          `json:"transcript_path"`
	CWD                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	Model                string          `json:"model"`
	PermissionMode       string          `json:"permission_mode"`
	Prompt               string          `json:"prompt"`
	Source               string          `json:"source"`
	Trigger              string          `json:"trigger"`
	ToolName             string          `json:"tool_name"`
	ToolUseID            string          `json:"tool_use_id"`
	ToolInput            json.RawMessage `json:"tool_input"`
	ToolResponse         string          `json:"tool_response"`
	StopHookActive       bool            `json:"stop_hook_active"`
	LastAssistantMessage string          `json:"last_assistant_message"`
}

// --- Native rollout JSONL (session file) types ---

type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"` // session_meta | turn_context | event_msg | response_item
	Payload   json.RawMessage `json:"payload"`
}

type rolloutMessage struct {
	Type    string                `json:"type"` // "message"
	Role    string                `json:"role"` // user | assistant | developer
	Content []rolloutContentBlock `json:"content"`
}

type rolloutContentBlock struct {
	Type string `json:"type"` // input_text | output_text
	Text string `json:"text"`
}

type rolloutFunctionCall struct {
	Type      string `json:"type"` // "function_call"
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
}

type rolloutFunctionCallOutput struct {
	Type   string `json:"type"` // "function_call_output"
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type rolloutTokenCount struct {
	Type string            `json:"type"` // "token_count"
	Info *rolloutTokenInfo `json:"info"`
}

type rolloutTokenInfo struct {
	TotalTokenUsage rolloutTokenUsage `json:"total_token_usage"`
	LastTokenUsage  rolloutTokenUsage `json:"last_token_usage"`
}

type rolloutTokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}
