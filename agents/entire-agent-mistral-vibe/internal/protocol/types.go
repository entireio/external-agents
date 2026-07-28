package protocol

import "encoding/json"

const ProtocolVersion = 1

type DeclaredCapabilities struct {
	Hooks                  bool `json:"hooks"`
	TranscriptAnalyzer     bool `json:"transcript_analyzer"`
	TranscriptPreparer     bool `json:"transcript_preparer"`
	TokenCalculator        bool `json:"token_calculator"`
	TextGenerator          bool `json:"text_generator"`
	HookResponseWriter     bool `json:"hook_response_writer"`
	SubagentAwareExtractor bool `json:"subagent_aware_extractor"`
	UsesTerminal           bool `json:"uses_terminal"`
}

type InfoResponse struct {
	ProtocolVersion int                  `json:"protocol_version"`
	Name            string               `json:"name"`
	Type            string               `json:"type"`
	Description     string               `json:"description"`
	IsPreview       bool                 `json:"is_preview"`
	ProtectedDirs   []string             `json:"protected_dirs"`
	HookNames       []string             `json:"hook_names"`
	Capabilities    DeclaredCapabilities `json:"capabilities"`
}

type DetectResponse struct {
	Present bool `json:"present"`
}

type SessionIDResponse struct {
	SessionID string `json:"session_id"`
}

type SessionDirResponse struct {
	SessionDir string `json:"session_dir"`
}

type SessionFileResponse struct {
	SessionFile string `json:"session_file"`
}

type ChunkResponse struct {
	Chunks [][]byte `json:"chunks"`
}

type ResumeCommandResponse struct {
	Command string `json:"command"`
}

type HooksInstalledCountResponse struct {
	HooksInstalled int `json:"hooks_installed"`
}

type AreHooksInstalledResponse struct {
	Installed bool `json:"installed"`
}

type TranscriptPositionResponse struct {
	Position int `json:"position"`
}

type ExtractFilesResponse struct {
	Files           []string `json:"files"`
	CurrentPosition int      `json:"current_position"`
}

type ExtractPromptsResponse struct {
	Prompts []string `json:"prompts"`
}

type ExtractSummaryResponse struct {
	Summary    string `json:"summary"`
	HasSummary bool   `json:"has_summary"`
}

type AgentSessionJSON struct {
	SessionID     string   `json:"session_id"`
	AgentName     string   `json:"agent_name"`
	RepoPath      string   `json:"repo_path"`
	SessionRef    string   `json:"session_ref"`
	StartTime     string   `json:"start_time"`
	NativeData    []byte   `json:"native_data"`
	ModifiedFiles []string `json:"modified_files"`
	NewFiles      []string `json:"new_files"`
	DeletedFiles  []string `json:"deleted_files"`
}

type HookInputJSON struct {
	HookType   string                 `json:"hook_type"`
	SessionID  string                 `json:"session_id"`
	SessionRef string                 `json:"session_ref"`
	Timestamp  string                 `json:"timestamp"`
	UserPrompt string                 `json:"user_prompt,omitempty"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolUseID  string                 `json:"tool_use_id,omitempty"`
	ToolInput  json.RawMessage        `json:"tool_input,omitempty"`
	RawData    map[string]interface{} `json:"raw_data,omitempty"`
}

type EventJSON struct {
	Type              int               `json:"type"`
	SessionID         string            `json:"session_id"`
	PreviousSessionID string            `json:"previous_session_id,omitempty"`
	SessionRef        string            `json:"session_ref,omitempty"`
	Prompt            string            `json:"prompt,omitempty"`
	Model             string            `json:"model,omitempty"`
	Timestamp         string            `json:"timestamp,omitempty"`
	ToolUseID         string            `json:"tool_use_id,omitempty"`
	SubagentID        string            `json:"subagent_id,omitempty"`
	ToolInput         json.RawMessage   `json:"tool_input,omitempty"`
	SubagentType      string            `json:"subagent_type,omitempty"`
	TaskDescription   string            `json:"task_description,omitempty"`
	ResponseMessage   string            `json:"response_message,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}
