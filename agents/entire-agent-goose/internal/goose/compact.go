package goose

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// CompactTranscript converts a materialized goose export into Entire
// Transcript Format (JSONL), which Entire uses to render conversations in
// `entire explain` and the web UI. Without it only user prompts (from
// extract-prompts) are visible.

const (
	compactTranscriptAgent      = "goose"
	compactTranscriptCLIVersion = "unknown"
)

type compactTranscriptLine struct {
	V          int    `json:"v"`
	Agent      string `json:"agent"`
	CLIVersion string `json:"cli_version"`
	Type       string `json:"type"`
	TS         string `json:"ts,omitempty"`
	ID         string `json:"id,omitempty"`
	Content    any    `json:"content"`
}

type compactUserTextBlock struct {
	Text string `json:"text"`
}

type compactAssistantTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type compactAssistantToolUseBlock struct {
	Type   string                 `json:"type"`
	ID     string                 `json:"id,omitempty"`
	Name   string                 `json:"name"`
	Input  any                    `json:"input"`
	Result *compactToolResultJSON `json:"result,omitempty"`
}

type compactToolResultJSON struct {
	Output string `json:"output"`
	Status string `json:"status"`
}

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

func compactTranscriptBytes(data []byte) ([]byte, error) {
	export, err := parseGooseExport(data)
	if err != nil {
		return nil, err
	}
	if len(export.Conversation) == 0 {
		return nil, errors.New("compact transcript produced no output")
	}

	cliVersion := compactCLIVersion()
	results := collectToolResults(export.Conversation)

	var buf bytes.Buffer
	for _, msg := range export.Conversation {
		switch msg.Role {
		case "user":
			// User messages carrying only toolResponse blocks are tool
			// results, not prompts; they surface attached to the assistant's
			// tool_use blocks instead.
			content := compactUserContent(msg.Content)
			if len(content) == 0 {
				continue
			}
			if err := writeCompactTranscriptLine(&buf, compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "user",
				TS:         compactTimestamp(msg.Created),
				Content:    content,
			}); err != nil {
				return nil, err
			}
		case "assistant":
			content := compactAssistantContent(msg.Content, results)
			if len(content) == 0 {
				continue
			}
			if err := writeCompactTranscriptLine(&buf, compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "assistant",
				TS:         compactTimestamp(msg.Created),
				ID:         msg.ID,
				Content:    content,
			}); err != nil {
				return nil, err
			}
		}
	}
	if buf.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return buf.Bytes(), nil
}

func compactUserContent(blocks []gooseContent) []compactUserTextBlock {
	out := []compactUserTextBlock{}
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			out = append(out, compactUserTextBlock{Text: block.Text})
		}
	}
	return out
}

func compactAssistantContent(blocks []gooseContent, results map[string]compactToolResultJSON) []any {
	out := []any{}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				out = append(out, compactAssistantTextBlock{Type: "text", Text: block.Text})
			}
		case "toolRequest":
			if block.ToolCall == nil {
				continue
			}
			tool := compactAssistantToolUseBlock{
				Type:  "tool_use",
				ID:    block.ID,
				Name:  block.ToolCall.Value.Name,
				Input: decodeToolInput(block.ToolCall.Value.Arguments),
			}
			if result, ok := results[block.ID]; ok && block.ID != "" {
				tool.Result = &result
			}
			out = append(out, tool)
		}
	}
	return out
}

// collectToolResults indexes toolResponse blocks (carried in user-role
// messages) by tool call ID.
func collectToolResults(messages []gooseMessage) map[string]compactToolResultJSON {
	results := map[string]compactToolResultJSON{}
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type != "toolResponse" || block.ToolResult == nil || block.ID == "" {
				continue
			}
			status := "success"
			if block.ToolResult.Status != "success" || block.ToolResult.Value.IsError {
				status = "error"
			}
			var parts []string
			for _, content := range block.ToolResult.Value.Content {
				if content.Type == "text" && content.Text != "" {
					parts = append(parts, content.Text)
				}
			}
			results[block.ID] = compactToolResultJSON{
				Output: strings.Join(parts, "\n"),
				Status: status,
			}
		}
	}
	return results
}

func decodeToolInput(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}

func compactTimestamp(created int64) string {
	if created <= 0 {
		return ""
	}
	return time.Unix(created, 0).UTC().Format(time.RFC3339)
}

func writeCompactTranscriptLine(buf *bytes.Buffer, line compactTranscriptLine) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return err
	}
	buf.Write(encoded)
	return buf.WriteByte('\n')
}

func compactCLIVersion() string {
	if version := strings.TrimSpace(os.Getenv("ENTIRE_CLI_VERSION")); version != "" {
		return version
	}
	return compactTranscriptCLIVersion
}
