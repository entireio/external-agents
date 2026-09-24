package devin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-devin/internal/protocol"
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

// compactStep is the ATIF step subset the compactor reads: message text,
// tool calls, and the observation block that carries tool results keyed by
// source_call_id.
type compactStep struct {
	Timestamp   string              `json:"timestamp"`
	Source      string              `json:"source"`
	Message     string              `json:"message"`
	ToolCalls   []compactToolCall   `json:"tool_calls,omitempty"`
	Observation *compactObservation `json:"observation,omitempty"`
}

type compactToolCall struct {
	ToolCallID   string          `json:"tool_call_id"`
	FunctionName string          `json:"function_name"`
	Arguments    json.RawMessage `json:"arguments"`
}

type compactObservation struct {
	Results []compactObservationResult `json:"results"`
}

type compactObservationResult struct {
	SourceCallID string `json:"source_call_id"`
	Content      string `json:"content"`
}

// CompactTranscript converts Devin's ATIF transcript into Entire Transcript
// Format (JSONL of user/assistant lines with text and tool_use blocks).
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
	t, err := parseTranscript(data)
	if err != nil {
		return nil, fmt.Errorf("parse ATIF transcript: %w", err)
	}

	var buf bytes.Buffer
	for _, raw := range t.Steps {
		var step compactStep
		if err := json.Unmarshal(raw, &step); err != nil {
			continue // Skip malformed steps
		}
		switch step.Source {
		case "user":
			if step.Message == "" {
				continue
			}
			if err := writeCompactLine(&buf, compactLine{
				V:          1,
				Agent:      AgentName,
				CLIVersion: compactCLIVersion,
				Type:       "user",
				TS:         step.Timestamp,
				Content:    []compactUserTextBlock{{Text: step.Message}},
			}); err != nil {
				return nil, err
			}
		case "agent":
			content := assistantBlocks(step)
			if len(content) == 0 {
				continue
			}
			if err := writeCompactLine(&buf, compactLine{
				V:          1,
				Agent:      AgentName,
				CLIVersion: compactCLIVersion,
				Type:       "assistant",
				TS:         step.Timestamp,
				Content:    content,
			}); err != nil {
				return nil, err
			}
		default:
			// system and other sources carry no conversation content
		}
	}
	return buf.Bytes(), nil
}

// assistantBlocks converts an agent step into compact content blocks: the
// response text (if any) followed by one tool_use block per tool call, with
// results joined from the step's observation by source_call_id.
func assistantBlocks(step compactStep) []any {
	var content []any
	if step.Message != "" {
		content = append(content, compactAssistantTextBlock{Type: "text", Text: step.Message})
	}

	resultsByCall := make(map[string]string)
	if step.Observation != nil {
		for _, result := range step.Observation.Results {
			resultsByCall[result.SourceCallID] = result.Content
		}
	}

	for _, call := range step.ToolCalls {
		block := compactToolUseBlock{
			Type:  "tool_use",
			ID:    call.ToolCallID,
			Name:  call.FunctionName,
			Input: decodeRawObject(call.Arguments),
		}
		if output, ok := resultsByCall[call.ToolCallID]; ok {
			block.Result = &compactToolResult{Output: output, Status: "success"}
		}
		content = append(content, block)
	}
	return content
}

// decodeRawObject decodes raw JSON into a generic value for re-marshaling,
// falling back to the raw string when it isn't valid JSON.
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

func writeCompactLine(buf *bytes.Buffer, line compactLine) error {
	data, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("marshal compact line: %w", err)
	}
	buf.Write(data)
	buf.WriteByte('\n')
	return nil
}
