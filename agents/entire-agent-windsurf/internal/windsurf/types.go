package windsurf

import "encoding/json"

const (
	HookNamePreUserPrompt       = "pre_user_prompt"
	HookNamePostWriteCode       = "post_write_code"
	HookNamePostCascadeResponse = "post_cascade_response"

	transcriptRecordVersion = 1
	transcriptTypePrompt    = "prompt"
	transcriptTypeFile      = "file"
	transcriptTypeResponse  = "response"
)

type hookInputRaw struct {
	AgentActionName string          `json:"agent_action_name"`
	TrajectoryID    string          `json:"trajectory_id"`
	ExecutionID     string          `json:"execution_id"`
	Timestamp       string          `json:"timestamp"`
	ModelName       string          `json:"model_name"`
	ToolInfo        json.RawMessage `json:"tool_info"`
}

type toolInfoPreUserPrompt struct {
	UserPrompt string `json:"user_prompt"`
}

type toolInfoPostWriteCode struct {
	FilePath string `json:"file_path"`
}

type toolInfoPostCascadeResponse struct {
	Response string `json:"response"`
}

// transcriptRecord is one JSONL line in the Windsurf session transcript.
type transcriptRecord struct {
	V       int    `json:"v"`
	Type    string `json:"type"`             // "prompt", "file", or "response"
	Content string `json:"content,omitempty"` // used by prompt and response
	Path    string `json:"path,omitempty"`    // used by file
	TS      string `json:"ts,omitempty"`
}

type windsurfHooksConfig struct {
	Hooks windsurfHookMap `json:"hooks"`
}

type windsurfHookMap struct {
	PreUserPrompt       []windsurfHookEntry `json:"pre_user_prompt,omitempty"`
	PostWriteCode       []windsurfHookEntry `json:"post_write_code,omitempty"`
	PostCascadeResponse []windsurfHookEntry `json:"post_cascade_response,omitempty"`
}

type windsurfHookEntry struct {
	Command    string `json:"command,omitempty"`
	PowerShell string `json:"powershell,omitempty"`
}
