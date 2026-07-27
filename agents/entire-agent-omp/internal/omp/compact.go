package omp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

type compactLine struct {
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

type compactUserBlock struct {
	Text string `json:"text"`
}

type compactTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type compactToolBlock struct {
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
	// Use the tolerant parser: Entire scopes checkpoint transcripts from a
	// mid-session offset before calling compact-transcript, so the title/session
	// header is often absent. The transcript_analyzer methods already parse
	// loosely; compaction must too, or scoped slices fail to summarize.
	session, err := parseSessionLoose(data)
	if err != nil {
		return nil, err
	}
	results := collectOMPToolResults(session.Active)
	var output bytes.Buffer
	for _, record := range session.Active {
		message := record.Entry.Message
		if message == nil {
			continue
		}
		switch message.Role {
		case "user":
			content := compactOMPUserBlocks(message.Content)
			if len(content) == 0 {
				continue
			}
			if err := writeCompactLine(&output, compactLine{
				V: 1, Agent: "omp", CLIVersion: compactCLIVersion(), Type: "user",
				TS: record.Entry.Timestamp, Content: content,
			}); err != nil {
				return nil, err
			}
		case "assistant":
			content := compactAssistantBlocks(message.Content, results)
			if len(content) == 0 {
				continue
			}
			line := compactLine{
				V: 1, Agent: "omp", CLIVersion: compactCLIVersion(), Type: "assistant",
				TS: record.Entry.Timestamp, ID: record.Entry.ID, Content: content,
			}
			if message.Usage != nil {
				line.InputTokens = message.Usage.Input
				line.OutputTokens = message.Usage.Output
			}
			if err := writeCompactLine(&output, line); err != nil {
				return nil, err
			}
		}
	}
	if output.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return output.Bytes(), nil
}

func compactOMPUserBlocks(raw json.RawMessage) []compactUserBlock {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if text == "" {
			return nil
		}
		return []compactUserBlock{{Text: text}}
	}
	var output []compactUserBlock
	for _, block := range messageBlocks(raw) {
		if block.Type == "text" && block.Text != "" {
			output = append(output, compactUserBlock{Text: block.Text})
		}
	}
	return output
}

func collectOMPToolResults(entries []parsedEntry) map[string]compactToolResult {
	results := make(map[string]compactToolResult)
	for _, record := range entries {
		message := record.Entry.Message
		if message == nil || message.Role != "toolResult" || message.ToolCallID == "" {
			continue
		}
		status := "success"
		if message.IsError {
			status = "error"
		}
		results[message.ToolCallID] = compactToolResult{Output: messageText(message.Content), Status: status}
	}
	return results
}

func compactAssistantBlocks(raw json.RawMessage, results map[string]compactToolResult) []any {
	var output []any
	for _, block := range messageBlocks(raw) {
		switch block.Type {
		case "text":
			if block.Text != "" {
				output = append(output, compactTextBlock{Type: "text", Text: block.Text})
			}
		case "toolCall":
			if block.Name == "" {
				continue
			}
			tool := compactToolBlock{
				Type: "tool_use", ID: block.ID, Name: compactToolName(block.Name), Input: decodeToolArguments(block.Arguments),
			}
			if result, ok := results[block.ID]; ok {
				tool.Result = &result
			}
			output = append(output, tool)
		}
	}
	return output
}

func compactToolName(name string) string {
	normalized := normalizeToolName(name)
	switch normalized {
	case "read", "write", "edit":
		return normalized
	case "apply_patch":
		return "edit"
	default:
		return name
	}
}

func decodeToolArguments(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return map[string]any{}
	}
	return value
}

func writeCompactLine(output *bytes.Buffer, line compactLine) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return err
	}
	output.Write(encoded)
	return output.WriteByte('\n')
}

func compactCLIVersion() string {
	if version := strings.TrimSpace(os.Getenv("ENTIRE_CLI_VERSION")); version != "" {
		return version
	}
	return "unknown"
}
