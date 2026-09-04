package amp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

const (
	compactTranscriptAgent      = "amp"
	compactTranscriptCLIVersion = "unknown"
)

type compactTranscriptLine struct {
	V            int    `json:"v"`
	Agent        string `json:"agent"`
	CLIVersion   string `json:"cli_version"`
	Type         string `json:"type"`
	TS           string `json:"ts,omitempty"`
	ID           string `json:"id,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	Content      any    `json:"content"`
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
	messages, err := decodeTranscript(data)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, errors.New("compact transcript produced no output")
	}

	cliVersion := compactCLIVersion()
	var buf bytes.Buffer
	for _, msg := range messages {
		switch msg.Role {
		case ThreadMessageRoleUser:
			content := compactUserContent(msg.Content)
			if len(content) == 0 {
				continue
			}
			if err := writeCompactTranscriptLine(&buf, compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "user",
				TS:         threadMessageTimestamp(msg),
				Content:    content,
			}); err != nil {
				return nil, err
			}
		case ThreadMessageRoleAssistant:
			content := compactAssistantContent(msg.Content, messages)
			if len(content) == 0 {
				continue
			}
			line := compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "assistant",
				TS:         threadMessageTimestamp(msg),
				ID:         threadMessageIDString(msg.MessageID),
				Content:    content,
			}
			if msg.Usage != nil {
				line.InputTokens = msg.Usage.InputTokens
				line.OutputTokens = msg.Usage.OutputTokens
			}
			if err := writeCompactTranscriptLine(&buf, line); err != nil {
				return nil, err
			}
		case ThreadMessageRoleInfo:
			// Informational messages have no compact transcript representation.
		}
	}
	if buf.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return buf.Bytes(), nil
}

func compactUserContent(blocks []ThreadContentBlock) []compactUserTextBlock {
	out := make([]compactUserTextBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == ThreadContentText && block.Text != "" {
			out = append(out, compactUserTextBlock{Text: block.Text})
		}
	}
	return out
}

func compactAssistantContent(blocks []ThreadContentBlock, messages []ThreadMessage) []any {
	results := collectToolResults(messages)
	out := make([]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ThreadContentText:
			if block.Text != "" {
				out = append(out, compactAssistantTextBlock{Type: "text", Text: block.Text})
			}
		case ThreadContentToolUse:
			tool := compactAssistantToolUseBlock{
				Type:  "tool_use",
				ID:    block.ID,
				Name:  block.Name,
				Input: decodeToolInput(block.Input),
			}
			if result, ok := results[block.ID]; ok {
				tool.Result = &result
			}
			out = append(out, tool)
		case ThreadContentThinking,
			ThreadContentImage,
			ThreadContentToolResult,
			ThreadContentServerToolUse,
			ThreadContentToolSearchResult:
			// Non-text/non-tool_use blocks are not represented in compact assistant content.
		}
	}
	return out
}

func collectToolResults(messages []ThreadMessage) map[string]compactToolResultJSON {
	results := map[string]compactToolResultJSON{}
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type != ThreadContentToolResult || block.ToolUseID == "" || block.Run == nil {
				continue
			}
			status := "success"
			if block.Run.Status == "error" || block.Run.Status == "cancelled" || block.Run.Error != nil {
				status = "error"
			}
			results[block.ToolUseID] = compactToolResultJSON{Output: toolRunOutput(block.Run), Status: status}
		}
	}
	return results
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

func decodeToolInput(input ThreadToolInput) any {
	if input == nil {
		return map[string]any{}
	}
	decoded := make(map[string]any, len(input))
	for key, raw := range input {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			decoded[key] = string(raw)
			continue
		}
		decoded[key] = value
	}
	return decoded
}

func toolRunOutput(run *ThreadToolRun) string {
	if run == nil {
		return ""
	}
	if run.Error != nil && run.Error.Message != "" {
		return run.Error.Message
	}
	if len(run.Result) == 0 {
		return ""
	}
	var common ThreadCommonToolResult
	if err := json.Unmarshal(run.Result, &common); err == nil {
		if common.Output != "" {
			return common.Output
		}
		if common.Diff != "" {
			return common.Diff
		}
		if len(common.Content) > 0 {
			var parts []string
			for _, block := range common.Content {
				if block.Text != "" {
					parts = append(parts, block.Text)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
	}
	var s string
	if err := json.Unmarshal(run.Result, &s); err == nil {
		return s
	}
	return string(run.Result)
}
