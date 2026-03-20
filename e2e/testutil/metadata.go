package testutil

import "time"

// TokenUsage tracks API token consumption for a checkpoint.
type TokenUsage struct {
	InputTokens         int `json:"input_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
	OutputTokens        int `json:"output_tokens"`
	APICallCount        int `json:"api_call_count"`
}

// Attribution tracks code authorship for a checkpoint.
type Attribution struct {
	CalculatedAt    time.Time `json:"calculated_at"`
	AgentLines      int       `json:"agent_lines"`
	HumanAdded      int       `json:"human_added"`
	HumanModified   int       `json:"human_modified"`
	HumanRemoved    int       `json:"human_removed"`
	TotalCommitted  int       `json:"total_committed"`
	AgentPercentage float64   `json:"agent_percentage"`
}

// CheckpointMetadata represents the top-level metadata.json for a checkpoint.
type CheckpointMetadata struct {
	CLIVersion       string       `json:"cli_version"`
	CheckpointID     string       `json:"checkpoint_id"`
	Strategy         string       `json:"strategy"`
	Branch           string       `json:"branch"`
	CheckpointsCount int          `json:"checkpoints_count"`
	FilesTouched     []string     `json:"files_touched"`
	Sessions         []SessionRef `json:"sessions"`
	TokenUsage       TokenUsage   `json:"token_usage"`
}

// SessionRef is a pointer to a session within checkpoint metadata.
type SessionRef struct {
	Metadata    string `json:"metadata"`
	Transcript  string `json:"transcript"`
	Context     string `json:"context"`
	ContentHash string `json:"content_hash"`
	Prompt      string `json:"prompt"`
}

// SessionMetadata represents a session's metadata.json within a checkpoint.
type SessionMetadata struct {
	CLIVersion         string      `json:"cli_version"`
	CheckpointID       string      `json:"checkpoint_id"`
	SessionID          string      `json:"session_id"`
	Strategy           string      `json:"strategy"`
	CreatedAt          time.Time   `json:"created_at"`
	Branch             string      `json:"branch"`
	Agent              string      `json:"agent"`
	CheckpointsCount   int         `json:"checkpoints_count"`
	FilesTouched       []string    `json:"files_touched"`
	TokenUsage         TokenUsage  `json:"token_usage"`
	InitialAttribution Attribution `json:"initial_attribution"`
	TranscriptPath     string      `json:"transcript_path"`
}

// CheckpointPath converts a checkpoint ID to its two-level directory path.
func CheckpointPath(id string) string {
	return id[:2] + "/" + id[2:]
}
