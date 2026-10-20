package protocol

import "encoding/json"

type HookInput struct {
	SessionID  string                     `json:"session_id"`
	SessionRef string                     `json:"session_ref"`
	Timestamp  string                     `json:"timestamp"`
	RawData    map[string]json.RawMessage `json:"raw_data,omitempty"`
}

type Session struct {
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

type Chunks struct {
	Chunks [][]byte `json:"chunks"`
}
type Tokens struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	APICallCount int     `json:"api_call_count"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}
