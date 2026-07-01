package grok

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

const compactCLIVersion = "unknown"

type compactLine struct {
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

type compactToolUseBlock struct {
	Type   string             `json:"type"`
	ID     string             `json:"id,omitempty"`
	Name   string             `json:"name"`
	Input  any                `json:"input,omitempty"`
	Result *compactToolResult `json:"result,omitempty"`
}

type compactToolResult struct {
	Output string `json:"output"`
	Status string `json:"status"`
}

func (a *Agent) CompactTranscript(sessionRef string) (protocol.CompactTranscriptResponse, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return protocol.CompactTranscriptResponse{}, err
	}
	compacted, err := compactTranscriptBytes(data)
	if err != nil {
		return protocol.CompactTranscriptResponse{}, err
	}
	return protocol.CompactTranscriptResponse{Transcript: base64.StdEncoding.EncodeToString(compacted)}, nil
}

func compactTranscriptBytes(data []byte) ([]byte, error) {
	records, err := parseSidecarRecords(data)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	for _, record := range records {
		switch record.Event {
		case "UserPromptSubmit":
			if record.Prompt == "" {
				continue
			}
			if err := writeCompactLine(&buf, compactLine{
				V:          1,
				Agent:      AgentName,
				CLIVersion: compactCLIVersion,
				Type:       "user",
				TS:         record.TS,
				Content:    []compactUserTextBlock{{Text: record.Prompt}},
			}); err != nil {
				return nil, err
			}
		case "PostToolUse", "PostToolUseFailure":
			block := compactToolUseBlock{
				Type:  "tool_use",
				ID:    record.ToolUseID,
				Name:  record.ToolName,
				Input: decodeRawObject(record.ToolInput),
			}
			if len(record.ToolResponse) > 0 {
				block.Result = &compactToolResult{Output: string(record.ToolResponse), Status: "success"}
			}
			if record.Error != "" || record.ErrorDetails != "" {
				block.Result = &compactToolResult{Output: record.Error + " " + record.ErrorDetails, Status: "error"}
			}
			if err := writeCompactLine(&buf, compactLine{
				V:          1,
				Agent:      AgentName,
				CLIVersion: compactCLIVersion,
				Type:       "assistant",
				TS:         record.TS,
				ID:         record.ToolUseID,
				Content:    []any{block},
			}); err != nil {
				return nil, err
			}
		case "Stop", "StopFailure":
			text := record.LastAssistantMessage
			if text == "" {
				text = record.ErrorDetails
			}
			if text == "" {
				continue
			}
			if err := writeCompactLine(&buf, compactLine{
				V:          1,
				Agent:      AgentName,
				CLIVersion: compactCLIVersion,
				Type:       "assistant",
				TS:         record.TS,
				Content:    []compactAssistantTextBlock{{Type: "text", Text: text}},
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

func parseSidecarRecords(data []byte) ([]sidecarRecord, error) {
	lines := bytes.Split(data, []byte("\n"))
	records := make([]sidecarRecord, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var record sidecarRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("parse sidecar record: %w", err)
		}
		records = append(records, record)
	}
	return records, nil
}

func writeCompactLine(buf *bytes.Buffer, line compactLine) error {
	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	buf.Write(data)
	buf.WriteByte('\n')
	return nil
}

func decodeRawObject(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}
