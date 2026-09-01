package zcode

import "encoding/json"

// ExportMessage is one line of the JSONL transcript this agent writes. It is
// a lossy, protocol-friendly projection of ZCode's native message+part rows:
// hidden/synthetic content is dropped, and tool parts are flattened.
type ExportMessage struct {
	ID     string        `json:"id"`
	Role   string        `json:"role"`
	Kind   string        `json:"kind,omitempty"` // semantics.kind, e.g. user_prompt
	Time   int64         `json:"time"`           // unix milliseconds
	Model  string        `json:"model,omitempty"`
	Text   string        `json:"text,omitempty"` // visible text parts joined by newline
	Tokens *ExportTokens `json:"tokens,omitempty"`
	Tools  []ExportTool  `json:"tools,omitempty"`
}

type ExportTokens struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
}

type ExportTool struct {
	Tool   string          `json:"tool"`
	Status string          `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output string          `json:"output,omitempty"`
}

// Native row shapes from ZCode's SQLite store (message.data / part.data).

type dbMessageData struct {
	Role string `json:"role"`
	Time struct {
		Created int64 `json:"created"` // unix milliseconds
	} `json:"time"`
	ModelID string `json:"modelID"`
	Model   struct {
		ModelID string `json:"modelID"`
	} `json:"model"`
	Tokens *struct {
		Input  int `json:"input"`
		Output int `json:"output"`
		Cache  *struct {
			Read  int `json:"read"`
			Write int `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Semantics *struct {
		Origin               string `json:"origin"`
		Kind                 string `json:"kind"`
		TranscriptVisibility string `json:"transcriptVisibility"`
	} `json:"semantics"`
}

type dbPartData struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic"`
	Tool      string `json:"tool"`
	State     *struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input"`
		Output string          `json:"output"`
	} `json:"state"`
}

type dbSessionRow struct {
	ID          string `json:"id"`
	ParentID    string `json:"parent_id"`
	Title       string `json:"title"`
	Directory   string `json:"directory"`
	TimeCreated int64  `json:"time_created"`
	TimeUpdated int64  `json:"time_updated"`
}
