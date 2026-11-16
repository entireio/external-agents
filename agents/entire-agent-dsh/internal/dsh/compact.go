package dsh

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-dsh/internal/protocol"
)

const (
	compactAgent      = agentName
	compactCLIVersion = "unknown"
)

type compactLine struct {
	V                int            `json:"v"`
	Agent            string         `json:"agent"`
	CLIVersion       string         `json:"cli_version"`
	Type             string         `json:"type"`
	TS               string         `json:"ts,omitempty"`
	ID               string         `json:"id,omitempty"`
	Model            string         `json:"model,omitempty"`
	InputTokens      int            `json:"input_tokens,omitempty"`
	OutputTokens     int            `json:"output_tokens,omitempty"`
	CacheReadTokens  int            `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int            `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int            `json:"reasoning_tokens,omitempty"`
	Content          []compactBlock `json:"content"`
}

type compactBlock struct {
	Type   string         `json:"type,omitempty"`
	Text   string         `json:"text,omitempty"`
	ID     string         `json:"id,omitempty"`
	Name   string         `json:"name,omitempty"`
	Input  any            `json:"input,omitempty"`
	Result *compactResult `json:"result,omitempty"`
}

type compactResult struct {
	Output string `json:"output"`
	Status string `json:"status"`
}

type callLocation struct {
	line  int
	block int
}

func (a *Agent) CompactTranscript(sessionRef string) (protocol.CompactTranscriptResponse, error) {
	data, err := readTranscriptFile(sessionRef)
	if err != nil {
		return protocol.CompactTranscriptResponse{}, fmt.Errorf("read transcript: %w", err)
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
	lines := make([]compactLine, 0)
	locations := make(map[string]callLocation)
	results := make(map[string]compactResult)
	seenCalls := make(map[string]struct{})

	forEachEventLine(data, 0, func(raw []byte) {
		var envelope struct {
			Type string `json:"type"`
			Time int64  `json:"time"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return
		}
		switch envelope.Type {
		case "user/message":
			var event eventEnvelope[userMessageData]
			if err := json.Unmarshal(raw, &event); err != nil || !isPromptSource(event.Data.Source) {
				return
			}
			text := blocksTextOrPlaceholder(event.Data.Content)
			if text == "" {
				return
			}
			lines = append(lines, compactLine{
				V:          1,
				Agent:      compactAgent,
				CLIVersion: compactCLIVersion,
				Type:       "user",
				TS:         compactTimestamp(envelope.Time),
				Content:    []compactBlock{{Text: text}},
			})
		case "assistant/message":
			var event eventEnvelope[assistantMessageData]
			if err := json.Unmarshal(raw, &event); err != nil {
				return
			}
			blocks := make([]compactBlock, 0, len(event.Data.Message.Content))
			lineIndex := len(lines)
			for _, block := range event.Data.Message.Content {
				switch block.Type {
				case "text":
					if block.Text == "" {
						continue
					}
					blocks = append(blocks, compactBlock{Type: "text", Text: block.Text})
				case "tool-call":
					if _, exists := seenCalls[block.ID]; exists {
						continue
					}
					input := parseToolInput(block.Arguments)
					blocks = append(blocks, compactBlock{Type: "tool_use", ID: block.ID, Name: block.Name, Input: input})
					locations[block.ID] = callLocation{line: lineIndex, block: len(blocks) - 1}
					seenCalls[block.ID] = struct{}{}
				}
			}
			if len(blocks) == 0 && event.Data.Usage == nil {
				return
			}
			line := compactLine{
				V:          1,
				Agent:      compactAgent,
				CLIVersion: compactCLIVersion,
				Type:       "assistant",
				TS:         compactTimestamp(envelope.Time),
				ID:         event.Data.Message.ID,
				Model:      event.Data.Message.Source.Model,
				Content:    blocks,
			}
			if usage := event.Data.Usage; usage != nil {
				line.InputTokens = usage.InputTokens
				line.OutputTokens = usage.OutputTokens
				line.CacheReadTokens = usage.CacheReadTokens
				line.CacheWriteTokens = usage.CacheWriteTokens
			}
			lines = append(lines, line)
		case "tool/call":
			var event eventEnvelope[toolCallData]
			if err := json.Unmarshal(raw, &event); err != nil {
				return
			}
			if event.Data.CallID == "" {
				return
			}
			if _, ok := seenCalls[event.Data.CallID]; ok {
				return
			}
			lineIndex := len(lines)
			lines = append(lines, compactLine{
				V:          1,
				Agent:      compactAgent,
				CLIVersion: compactCLIVersion,
				Type:       "assistant",
				TS:         compactTimestamp(envelope.Time),
				Content: []compactBlock{{
					Type:  "tool_use",
					ID:    event.Data.CallID,
					Name:  event.Data.Name,
					Input: parseToolInput(event.Data.Arguments),
				}},
			})
			locations[event.Data.CallID] = callLocation{line: lineIndex, block: 0}
			seenCalls[event.Data.CallID] = struct{}{}
		case "tool/result":
			var event eventEnvelope[toolResultData]
			if err := json.Unmarshal(raw, &event); err != nil {
				return
			}
			if callID := toolResultCallID(event.Data); callID != "" {
				status := "success"
				if event.Data.Message.IsError || event.Data.Error != nil {
					status = "error"
				}
				results[callID] = compactResult{Output: blocksTextOrPlaceholder(event.Data.Message.Content), Status: status}
			}
		}
	})

	for callID, result := range results {
		location, ok := locations[callID]
		if !ok {
			continue
		}
		if location.line < 0 || location.line >= len(lines) {
			continue
		}
		line := &lines[location.line]
		if location.block < 0 || location.block >= len(line.Content) {
			continue
		}
		copied := result
		line.Content[location.block].Result = &copied
	}

	if len(lines) == 0 {
		return nil, errors.New("transcript has no user or assistant messages")
	}

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	for _, line := range lines {
		if line.Content == nil {
			line.Content = []compactBlock{}
		}
		if err := encoder.Encode(line); err != nil {
			return nil, fmt.Errorf("encode compact transcript line: %w", err)
		}
	}
	return out.Bytes(), nil
}

func compactTimestamp(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339Nano)
}

func parseToolInput(arguments string) any {
	if strings.TrimSpace(arguments) == "" {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal([]byte(arguments), &value); err != nil {
		return arguments
	}
	if value == nil {
		return map[string]any{}
	}
	return value
}

func toolResultCallID(data toolResultData) string {
	if data.Message.ToolCallID != "" {
		return data.Message.ToolCallID
	}
	return data.Message.Source.CallID
}
