package vibe

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

const compactTranscriptCLIVersion = "unknown"

type compactLine struct {
	V          int    `json:"v"`
	Agent      string `json:"agent"`
	CLIVersion string `json:"cli_version"`
	Type       string `json:"type"`
	TS         string `json:"ts,omitempty"`
	ID         string `json:"id,omitempty"`
	Content    any    `json:"content"`
}

type compactUserText struct {
	Text string `json:"text"`
}

type compactText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type compactToolUse struct {
	Type   string             `json:"type"`
	ID     string             `json:"id,omitempty"`
	Name   string             `json:"name"`
	Input  any                `json:"input"`
	Result *compactToolResult `json:"result,omitempty"`
}

type compactToolResult struct {
	Output string `json:"output"`
	Status string `json:"status"`
}

// CompactTranscript converts stored history into Entire Transcript Format.
// sessionRef may be a temp file holding a slice of the transcript.
func (a *Agent) CompactTranscript(sessionRef string) (protocol.CompactTranscriptResponse, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return protocol.CompactTranscriptResponse{}, fmt.Errorf("failed to read transcript: %w", err)
	}
	compacted, err := compactTranscriptBytes(data)
	if err != nil {
		return protocol.CompactTranscriptResponse{}, err
	}
	return protocol.CompactTranscriptResponse{Transcript: base64.StdEncoding.EncodeToString(compacted)}, nil
}

// compactTranscriptBytes emits one line per user message, assistant message
// and tool call. Reasoning is dropped.
func compactTranscriptBytes(data []byte) ([]byte, error) {
	entries, err := decodeTranscript(data)
	if err != nil {
		return nil, err
	}
	cliVersion := compactTranscriptCLIVersion
	if version := strings.TrimSpace(os.Getenv("ENTIRE_CLI_VERSION")); version != "" {
		cliVersion = version
	}

	var buf bytes.Buffer
	for _, entry := range entries {
		line := compactLine{V: 1, Agent: agentName, CLIVersion: cliVersion, TS: entryTimestamp(entry)}
		switch {
		case entry.Type == EntryMessage && entry.Role == RoleUser:
			// Context Vibe injected is not something the user said.
			if !entry.isPrompt() {
				continue
			}
			text := entry.text()
			if text == "" {
				continue
			}
			line.Type = "user"
			line.Content = []compactUserText{{Text: text}}
		case entry.Type == EntryMessage && entry.Role == RoleAssistant:
			text := entry.text()
			if text == "" {
				continue
			}
			line.Type = "assistant"
			line.ID = entry.ID
			line.Content = []any{compactText{Type: "text", Text: text}}
		case entry.Type == EntryEffect:
			line.Type = "assistant"
			line.ID = entry.ID
			line.Content = []any{compactTool(entry)}
		default:
			continue
		}
		encoded, err := json.Marshal(line)
		if err != nil {
			return nil, err
		}
		buf.Write(encoded)
		buf.WriteByte('\n')
	}
	if buf.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return buf.Bytes(), nil
}

func compactTool(entry Entry) compactToolUse {
	tool := compactToolUse{Type: "tool_use", ID: entry.ID, Name: entry.Title, Input: map[string]any{}}
	if entry.Detail != nil {
		if entry.Detail.ToolName != "" {
			tool.Name = entry.Detail.ToolName
		}
		if len(entry.Detail.Input) > 0 {
			var input any
			if json.Unmarshal(entry.Detail.Input, &input) == nil && input != nil {
				tool.Input = input
			}
		}
	}
	if entry.State == nil {
		return tool
	}
	status := "success"
	output := entry.State.OutputText
	if output == "" && len(entry.State.Output) > 0 && string(entry.State.Output) != "null" {
		output = string(entry.State.Output)
	}
	switch entry.State.Status {
	case "failed", "cancelled", "skipped", "blocked":
		status = "error"
		if output == "" && len(entry.State.Error) > 0 && string(entry.State.Error) != "null" {
			output = string(entry.State.Error)
		}
	case "completed":
	default:
		// Still pending or running: there is no result yet.
		return tool
	}
	tool.Result = &compactToolResult{Output: output, Status: status}
	return tool
}

func entryTimestamp(entry Entry) string {
	if t := entry.createdTime(); !t.IsZero() {
		return t.Format(time.RFC3339)
	}
	return ""
}
