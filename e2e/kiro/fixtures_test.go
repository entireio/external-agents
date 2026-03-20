//go:build e2e

package kiro

import (
	"encoding/json"
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// KiroTranscript builds Kiro-format transcript files for testing.
type KiroTranscript struct {
	ConversationID string             `json:"conversation_id"`
	History        []kiroHistoryEntry `json:"history"`
}

type kiroHistoryEntry struct {
	User      kiroUserMessage `json:"user"`
	Assistant json.RawMessage `json:"assistant"`
}

type kiroUserMessage struct {
	Content   json.RawMessage `json:"content"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// NewKiroTranscript creates a new transcript builder.
func NewKiroTranscript(id string) *KiroTranscript {
	return &KiroTranscript{ConversationID: id}
}

func marshalPromptContent(prompt string) json.RawMessage {
	content, _ := json.Marshal(map[string]interface{}{
		"Prompt": map[string]string{"prompt": prompt},
	})
	return content
}

// AddPrompt adds a user prompt entry with no assistant response.
func (kt *KiroTranscript) AddPrompt(prompt string) *KiroTranscript {
	kt.History = append(kt.History, kiroHistoryEntry{
		User: kiroUserMessage{Content: marshalPromptContent(prompt)},
	})
	return kt
}

// AddPromptWithFileEdit adds a user prompt paired with an assistant response that contains a file edit tool use.
func (kt *KiroTranscript) AddPromptWithFileEdit(prompt, filePath string) *KiroTranscript {
	toolUse := map[string]interface{}{
		"ToolUse": map[string]interface{}{
			"message_id": "msg-001",
			"tool_uses": []map[string]interface{}{
				{
					"id":   "tool-001",
					"name": "fs_write",
					"args": map[string]string{"path": filePath},
				},
			},
		},
	}
	assistantContent, _ := json.Marshal(toolUse)

	kt.History = append(kt.History, kiroHistoryEntry{
		User:      kiroUserMessage{Content: marshalPromptContent(prompt)},
		Assistant: assistantContent,
	})
	return kt
}

// AddResponse adds a user prompt paired with an assistant text response.
func (kt *KiroTranscript) AddResponse(prompt, response string) *KiroTranscript {
	userContent := marshalPromptContent(prompt)

	responseContent := map[string]interface{}{
		"Response": map[string]interface{}{
			"message_id": "msg-resp",
			"content":    response,
		},
	}
	assistantContent, _ := json.Marshal(responseContent)

	kt.History = append(kt.History, kiroHistoryEntry{
		User:      kiroUserMessage{Content: userContent},
		Assistant: assistantContent,
	})
	return kt
}

// JSON returns the JSON-encoded transcript string.
func (kt *KiroTranscript) JSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(kt)
	if err != nil {
		t.Fatalf("marshal KiroTranscript: %v", err)
	}
	return string(data)
}

// WriteToFile writes the transcript to a file and returns the absolute path.
func (kt *KiroTranscript) WriteToFile(t *testing.T, env *e2e.TestEnv, relPath string) string {
	t.Helper()
	env.WriteFile(relPath, kt.JSON(t))
	return env.AbsPath(relPath)
}
