package kilo

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

const (
	compactTranscriptAgent      = "kilo"
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
	session, err := parseKiloSession(data)
	if err != nil {
		return nil, err
	}
	if len(session.Messages) == 0 {
		return nil, errors.New("compact transcript produced no output")
	}

	cliVersion := compactCLIVersion()
	var buf bytes.Buffer
	for _, msg := range session.Messages {
		switch msg.Info.Role {
		case MessageRoleUser:
			content := compactUserContent(msg.Parts)
			if len(content) == 0 {
				continue
			}
			if err := writeCompactTranscriptLine(&buf, compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "user",
				TS:         messageTimestamp(msg),
				Content:    content,
			}); err != nil {
				return nil, err
			}
		case MessageRoleAssistant:
			content := compactAssistantContent(msg.Parts)
			if len(content) == 0 {
				continue
			}
			line := compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "assistant",
				TS:         messageTimestamp(msg),
				ID:         msg.Info.ID,
				Content:    content,
			}
			if msg.Info.Tokens != nil {
				line.InputTokens = msg.Info.Tokens.Input
				line.OutputTokens = msg.Info.Tokens.Output
			}
			if err := writeCompactTranscriptLine(&buf, line); err != nil {
				return nil, err
			}
		}
	}
	if buf.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return buf.Bytes(), nil
}

func compactUserContent(parts []MessagePart) []compactUserTextBlock {
	out := make([]compactUserTextBlock, 0, len(parts))
	for _, part := range parts {
		if part.Type == PartText && part.Text != "" {
			out = append(out, compactUserTextBlock{Text: part.Text})
		}
	}
	return out
}

func compactAssistantContent(parts []MessagePart) []any {
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case PartText:
			if part.Text != "" {
				out = append(out, compactAssistantTextBlock{Type: "text", Text: part.Text})
			}
		case PartTool:
			tool := compactAssistantToolUseBlock{
				Type:  "tool_use",
				ID:    part.CallID,
				Name:  part.Tool,
				Input: decodeRawInput(part.State),
			}
			if result := toolResultFromState(part.State); result != nil {
				tool.Result = result
			}
			out = append(out, tool)
		case PartReasoning, PartFile, PartSubtask:
			// Not relevant to transcript compaction — skip.
		}
	}
	return out
}

func decodeRawInput(state *ToolState) any {
	if state == nil || len(state.Input) == 0 {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(state.Input, &decoded); err != nil {
		return string(state.Input)
	}
	return decoded
}

func toolResultFromState(state *ToolState) *compactToolResultJSON {
	if state == nil {
		return nil
	}
	if state.Output == "" && state.Error == "" {
		return nil
	}
	status := "success"
	output := state.Output
	if state.Error != "" || strings.EqualFold(state.Status, "error") {
		status = "error"
		if output == "" {
			output = state.Error
		}
	}
	return &compactToolResultJSON{Output: output, Status: status}
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
