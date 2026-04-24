package pi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

const (
	compactTranscriptAgent      = "pi"
	compactTranscriptCLIVersion = "unknown"
	contentTypeText             = "text"
	compactToolResultSuccess    = "success"
	compactToolResultError      = "error"
)

var compactToolNameMap = map[string]string{
	"edit":  "Edit",
	"read":  "Read",
	"write": "Write",
}

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

	return protocol.CompactTranscriptResponse{
		Transcript: base64.StdEncoding.EncodeToString(compacted),
	}, nil
}

func compactTranscriptBytes(data []byte) ([]byte, error) {
	active := resolveActiveBranch(data)
	results, err := collectCompactToolResults(data, active)
	if err != nil {
		return nil, err
	}

	cliVersion := compactCLIVersion()
	var buf bytes.Buffer

	scanner := newJSONLScanner(data)
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != entryTypeMessage {
			continue
		}
		if active != nil && !active[entry.ID] {
			continue
		}

		switch entry.Message.Role {
		case "user":
			content := compactUserContent(entry.Message.Content)
			if len(content) == 0 {
				continue
			}
			if err := writeCompactTranscriptLine(&buf, compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "user",
				TS:         entry.Timestamp,
				Content:    content,
			}); err != nil {
				return nil, err
			}

		case "assistant":
			content := compactAssistantContent(entry.Message.Content, results)
			if len(content) == 0 {
				continue
			}
			line := compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "assistant",
				TS:         entry.Timestamp,
				ID:         entry.ID,
				Content:    content,
			}
			if entry.Message.Usage != nil {
				line.InputTokens = entry.Message.Usage.Input
				line.OutputTokens = entry.Message.Usage.Output
			}
			if err := writeCompactTranscriptLine(&buf, line); err != nil {
				return nil, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan transcript: %w", err)
	}
	if buf.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return buf.Bytes(), nil
}

func compactUserContent(raw json.RawMessage) []compactUserTextBlock {
	if text := decodeCompactString(raw); text != "" {
		return []compactUserTextBlock{{Text: text}}
	}

	var items []contentItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}

	blocks := make([]compactUserTextBlock, 0, len(items))
	for _, item := range items {
		if item.Type == contentTypeText && item.Text != "" {
			blocks = append(blocks, compactUserTextBlock{Text: item.Text})
		}
	}
	return blocks
}

func compactAssistantContent(raw json.RawMessage, results map[string]compactToolResultJSON) []any {
	if text := decodeCompactString(raw); text != "" {
		return []any{compactAssistantTextBlock{Type: contentTypeText, Text: text}}
	}

	var items []contentItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}

	blocks := make([]any, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case contentTypeText:
			if item.Text != "" {
				blocks = append(blocks, compactAssistantTextBlock{
					Type: contentTypeText,
					Text: item.Text,
				})
			}
		case "toolCall":
			block := compactAssistantToolUseBlock{
				Type:  "tool_use",
				ID:    item.ID,
				Name:  normalizeCompactToolName(item.Name),
				Input: decodeCompactInput(item.Arguments),
			}
			if result, ok := results[item.ID]; ok {
				block.Result = &result
			}
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func collectCompactToolResults(data []byte, active map[string]bool) (map[string]compactToolResultJSON, error) {
	results := map[string]compactToolResultJSON{}
	scanner := newJSONLScanner(data)
	for scanner.Scan() {
		var entry messageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != entryTypeMessage || entry.Message.Role != "toolResult" {
			continue
		}
		if active != nil && !active[entry.ID] {
			continue
		}
		if entry.Message.ToolCallID == "" {
			continue
		}
		results[entry.Message.ToolCallID] = compactToolResultJSON{
			Output: compactResultOutput(entry.Message.Content),
			Status: compactResultStatus(entry.Message.IsError),
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan tool results: %w", err)
	}
	return results, nil
}

func writeCompactTranscriptLine(buf *bytes.Buffer, line compactTranscriptLine) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("marshal compact transcript line: %w", err)
	}
	if _, err := buf.Write(encoded); err != nil {
		return fmt.Errorf("write compact transcript line: %w", err)
	}
	if err := buf.WriteByte('\n'); err != nil {
		return fmt.Errorf("terminate compact transcript line: %w", err)
	}
	return nil
}

func compactCLIVersion() string {
	if version := strings.TrimSpace(os.Getenv("ENTIRE_CLI_VERSION")); version != "" {
		return version
	}
	return compactTranscriptCLIVersion
}

func normalizeCompactToolName(name string) string {
	if normalized, ok := compactToolNameMap[name]; ok {
		return normalized
	}
	return name
}

func decodeCompactInput(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}

func decodeCompactString(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ""
	}
	return text
}

func compactResultOutput(raw json.RawMessage) string {
	if text := decodeCompactString(raw); text != "" {
		return text
	}

	var items []contentItem
	if err := json.Unmarshal(raw, &items); err == nil {
		texts := make([]string, 0, len(items))
		for _, item := range items {
			if item.Type == contentTypeText && item.Text != "" {
				texts = append(texts, item.Text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		encoded, err := json.Marshal(decoded)
		if err == nil {
			return string(encoded)
		}
	}
	return string(raw)
}

func compactResultStatus(isError bool) string {
	if isError {
		return compactToolResultError
	}
	return compactToolResultSuccess
}
