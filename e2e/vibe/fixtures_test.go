//go:build e2e

package vibe

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	e2e "github.com/entireio/external-agents/e2e"
)

// VibeTranscript builds Vibe-format JSONL transcript files for testing.
type VibeTranscript struct {
	messages []vibeTranscriptMessage
}

type vibeTranscriptMessage struct {
	Role       string                    `json:"role"`
	Content    string                    `json:"content,omitempty"`
	ToolCalls  []vibeTranscriptToolCall  `json:"tool_calls,omitempty"`
	MessageID  string                    `json:"message_id,omitempty"`
	Name       string                    `json:"name,omitempty"`
	ToolCallID string                    `json:"tool_call_id,omitempty"`
}

type vibeTranscriptToolCall struct {
	ID       string                        `json:"id"`
	Index    int                           `json:"index"`
	Function vibeTranscriptToolCallFunction `json:"function"`
	Type     string                        `json:"type"`
}

type vibeTranscriptToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// NewVibeTranscript creates a new JSONL transcript builder.
func NewVibeTranscript() *VibeTranscript {
	return &VibeTranscript{}
}

// AddPrompt adds a user message to the transcript.
func (vt *VibeTranscript) AddPrompt(prompt string) *VibeTranscript {
	vt.messages = append(vt.messages, vibeTranscriptMessage{
		Role:      "user",
		Content:   prompt,
		MessageID: fmt.Sprintf("msg-user-%d", len(vt.messages)),
	})
	return vt
}

// AddResponse adds a user prompt followed by an assistant response.
func (vt *VibeTranscript) AddResponse(prompt, response string) *VibeTranscript {
	vt.AddPrompt(prompt)
	vt.messages = append(vt.messages, vibeTranscriptMessage{
		Role:      "assistant",
		Content:   response,
		MessageID: fmt.Sprintf("msg-asst-%d", len(vt.messages)),
	})
	return vt
}

// AddAssistantMessage adds a standalone assistant message.
func (vt *VibeTranscript) AddAssistantMessage(content string) *VibeTranscript {
	vt.messages = append(vt.messages, vibeTranscriptMessage{
		Role:      "assistant",
		Content:   content,
		MessageID: fmt.Sprintf("msg-asst-%d", len(vt.messages)),
	})
	return vt
}

// AddToolUse adds an assistant message with a tool call, followed by the tool result.
func (vt *VibeTranscript) AddToolUse(prompt, toolName, filePath string) *VibeTranscript {
	vt.AddPrompt(prompt)
	argsMap := map[string]string{"path": filePath, "content": "test content"}
	argsJSON, _ := json.Marshal(argsMap)

	vt.messages = append(vt.messages, vibeTranscriptMessage{
		Role:      "assistant",
		MessageID: fmt.Sprintf("msg-asst-%d", len(vt.messages)),
		ToolCalls: []vibeTranscriptToolCall{
			{
				ID:    fmt.Sprintf("call-%d", len(vt.messages)),
				Index: 0,
				Function: vibeTranscriptToolCallFunction{
					Name:      toolName,
					Arguments: string(argsJSON),
				},
				Type: "function",
			},
		},
	})
	// Add tool result
	vt.messages = append(vt.messages, vibeTranscriptMessage{
		Role:       "tool",
		Content:    fmt.Sprintf("path: %s\nbytes_written: 12", filePath),
		Name:       toolName,
		ToolCallID: fmt.Sprintf("call-%d", len(vt.messages)-1),
	})
	return vt
}

// JSONL returns the JSONL-encoded transcript string.
func (vt *VibeTranscript) JSONL(t *testing.T) string {
	t.Helper()
	var lines []string
	for _, msg := range vt.messages {
		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal VibeTranscript message: %v", err)
		}
		lines = append(lines, string(data))
	}
	return strings.Join(lines, "\n") + "\n"
}

// WriteToFile writes the transcript JSONL to a file and returns the absolute path.
func (vt *VibeTranscript) WriteToFile(t *testing.T, env *e2e.TestEnv, relPath string) string {
	t.Helper()
	env.WriteFile(relPath, vt.JSONL(t))
	return env.AbsPath(relPath)
}

// VibeHookPayload builds stdin payloads matching Vibe's native hook format.
type VibeHookPayload struct {
	HookEventName string      `json:"hook_event_name"`
	CWD           string      `json:"cwd"`
	SessionID     string      `json:"session_id"`
	Prompt        string      `json:"prompt,omitempty"`
	ToolName      string      `json:"tool_name,omitempty"`
	ToolInput     interface{} `json:"tool_input,omitempty"`
	ToolOutcome   string      `json:"tool_outcome,omitempty"`
	ToolResponse  interface{} `json:"tool_response,omitempty"`
	ToolError     string      `json:"tool_error,omitempty"`
}

// JSON returns the JSON-encoded string for use as stdin.
func (p VibeHookPayload) JSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal VibeHookPayload: %v", err)
	}
	return string(data)
}
