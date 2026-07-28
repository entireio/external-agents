package kilo

import (
	"encoding/json"
	"time"
)

type UnixMillis int64

func (m UnixMillis) Time() time.Time {
	if m == 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(m)).UTC()
}

type SessionMessage struct {
	Info  MessageInfo   `json:"info"`
	Parts []MessagePart `json:"parts,omitempty"`
}

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
)

type MessageInfo struct {
	ID         string       `json:"id"`
	Role       MessageRole  `json:"role"`
	SessionID  string       `json:"sessionID,omitempty"`
	ProviderID string       `json:"providerID,omitempty"`
	ModelID    string       `json:"modelID,omitempty"`
	Mode       string       `json:"mode,omitempty"`
	Time       *MessageTime `json:"time,omitempty"`
	Tokens     *Tokens      `json:"tokens,omitempty"`
}

type MessageTime struct {
	Created   UnixMillis `json:"created"`
	Completed UnixMillis `json:"completed,omitempty"`
}

type Tokens struct {
	Input     int          `json:"input"`
	Output    int          `json:"output"`
	Reasoning int          `json:"reasoning,omitempty"`
	Cache     *CacheTokens `json:"cache,omitempty"`
}

type CacheTokens struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

type PartType string

const (
	PartText      PartType = "text"
	PartReasoning PartType = "reasoning"
	PartFile      PartType = "file"
	PartTool      PartType = "tool"
	PartSubtask   PartType = "subtask"
)

type MessagePart struct {
	ID        string          `json:"id,omitempty"`
	Type      PartType        `json:"type"`
	MessageID string          `json:"messageID,omitempty"`
	SessionID string          `json:"sessionID,omitempty"`
	Text      string          `json:"text,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	CallID    string          `json:"callID,omitempty"`
	State     *ToolState      `json:"state,omitempty"`
	URL       string          `json:"url,omitempty"`
	Filename  string          `json:"filename,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type ToolState struct {
	Status   string          `json:"status,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Output   string          `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
	Title    string          `json:"title,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	Time     *ToolStateTime  `json:"time,omitempty"`
}

type ToolStateTime struct {
	Start UnixMillis `json:"start,omitempty"`
	End   UnixMillis `json:"end,omitempty"`
}
