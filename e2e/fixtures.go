//go:build e2e

package e2e

import (
	"encoding/json"
	"testing"
)

// HookInput builds stdin payloads for hook-related subcommands.
type HookInput struct {
	HookType   string                 `json:"hook_type,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	SessionRef string                 `json:"session_ref,omitempty"`
	Timestamp  string                 `json:"timestamp,omitempty"`
	UserPrompt string                 `json:"user_prompt,omitempty"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolUseID  string                 `json:"tool_use_id,omitempty"`
	ToolInput  json.RawMessage        `json:"tool_input,omitempty"`
	RawData    map[string]interface{} `json:"raw_data,omitempty"`
}

// JSON returns the JSON-encoded string for use as stdin.
func (h HookInput) JSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal HookInput: %v", err)
	}
	return string(data)
}

// ParseHookInput builds stdin payloads for the parse-hook subcommand.
type ParseHookInput struct {
	HookEventName string          `json:"hook_event_name,omitempty"`
	CWD           string          `json:"cwd,omitempty"`
	Prompt        string          `json:"prompt,omitempty"`
	ToolName      string          `json:"tool_name,omitempty"`
	ToolInput     json.RawMessage `json:"tool_input,omitempty"`
}

// JSON returns the JSON-encoded string.
func (p ParseHookInput) JSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal ParseHookInput: %v", err)
	}
	return string(data)
}

// KiroTranscript builds Kiro-format transcript files for testing.
type KiroTranscript struct {
	ConversationID string              `json:"conversation_id"`
	History        []kiroHistoryEntry  `json:"history"`
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

// AddPrompt adds a user prompt entry with no assistant response.
func (kt *KiroTranscript) AddPrompt(prompt string) *KiroTranscript {
	promptContent := map[string]interface{}{
		"Prompt": map[string]string{"prompt": prompt},
	}
	content, _ := json.Marshal(promptContent)
	kt.History = append(kt.History, kiroHistoryEntry{
		User: kiroUserMessage{Content: content},
	})
	return kt
}

// AddPromptWithFileEdit adds a user prompt paired with an assistant response that contains a file edit tool use.
func (kt *KiroTranscript) AddPromptWithFileEdit(prompt, filePath string) *KiroTranscript {
	promptContent := map[string]interface{}{
		"Prompt": map[string]string{"prompt": prompt},
	}
	userContent, _ := json.Marshal(promptContent)

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
		User:      kiroUserMessage{Content: userContent},
		Assistant: assistantContent,
	})
	return kt
}

// AddResponse adds a user prompt paired with an assistant text response.
func (kt *KiroTranscript) AddResponse(prompt, response string) *KiroTranscript {
	promptContent := map[string]interface{}{
		"Prompt": map[string]string{"prompt": prompt},
	}
	userContent, _ := json.Marshal(promptContent)

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
func (kt *KiroTranscript) WriteToFile(t *testing.T, env *TestEnv, relPath string) string {
	t.Helper()
	env.WriteFile(relPath, kt.JSON(t))
	return env.AbsPath(relPath)
}
