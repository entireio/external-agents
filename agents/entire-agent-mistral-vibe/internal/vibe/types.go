package vibe

import (
	"encoding/json"
	"time"
)

// Entry types in Vibe's public session history
// (vibe.public-session-state/v1, read through vibe-app-server).
const (
	EntryMessage   = "message"
	EntryReasoning = "reasoning"
	EntryEffect    = "effect"
	EntryNotice    = "notice"

	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Entry is one line of the stored transcript: a Vibe history entry, plus an
// optional token snapshot Entire adds when it stores the line.
type Entry struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId,omitempty"`
	TurnID    string         `json:"turnId,omitempty"`
	CreatedAt int64          `json:"createdAt,omitempty"`
	Type      string         `json:"type"`
	Role      string         `json:"role,omitempty"`
	Content   []ContentBlock `json:"content,omitempty"`
	Text      string         `json:"text,omitempty"`
	Title     string         `json:"title,omitempty"`
	Detail    *EffectDetail  `json:"detail,omitempty"`
	State     *EffectState   `json:"state,omitempty"`

	// Meta is added by this adapter to the last line of each refresh.
	Meta *EntryMeta `json:"entire,omitempty"`
}

// metaKey is the JSON key of Entry.Meta.
const metaKey = "entire"

// EntryMeta records what the session looked like when a line was the last one
// stored. Usage is cumulative for the whole session.
type EntryMeta struct {
	Usage *TokenUsage `json:"usage,omitempty"`
	Model string      `json:"model,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// EffectDetail describes a tool call. Notices reuse the "detail" key with a
// different shape, which is why every field here is optional.
type EffectDetail struct {
	ToolName string          `json:"toolName,omitempty"`
	Kind     string          `json:"kind,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

type EffectState struct {
	Status     string          `json:"status,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	OutputText string          `json:"outputText,omitempty"`
	Error      json.RawMessage `json:"error,omitempty"`
}

// TokenUsage mirrors the session's tokenUsage object. InputTokens includes the
// cached tokens.
type TokenUsage struct {
	InputTokens       int `json:"inputTokens"`
	OutputTokens      int `json:"outputTokens"`
	TotalTokens       int `json:"totalTokens,omitempty"`
	CachedInputTokens int `json:"cachedInputTokens,omitempty"`
}

func (e Entry) createdTime() time.Time {
	if e.CreatedAt == 0 {
		return time.Time{}
	}
	return time.UnixMilli(e.CreatedAt).UTC()
}

// text returns the joined text blocks of a message entry.
func (e Entry) text() string {
	var out string
	for _, block := range e.Content {
		if block.Type != "text" || block.Text == "" {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += block.Text
	}
	return out
}
