package kiro

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"
)

const (
	compactTranscriptAgent      = "kiro"
	compactTranscriptCLIVersion = "unknown"
	compactToolResultSuccess    = "success"
	compactToolResultError      = "error"
)

var compactToolNameMap = map[string]string{
	"fs_write": "Write",
	"fs_edit":  "Edit",
}

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

type kiroToolUseResultsContent struct {
	ToolUseResults struct {
		ToolUseResults []kiroToolUseResult `json:"tool_use_results"`
	} `json:"ToolUseResults"`
}

type kiroToolUseResult struct {
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Status  string          `json:"status,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
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
	transcript, err := parseTranscript(data)
	if err != nil {
		return nil, err
	}
	if len(transcript.History) == 0 {
		return nil, errors.New("transcript has no history entries")
	}
	cliVersion := compactCLIVersion(transcript)

	var buf bytes.Buffer
	for i := range transcript.History {
		entry := transcript.History[i]

		if prompt := extractUserPrompt(entry.User.Content); prompt != "" {
			if err := writeCompactTranscriptLine(&buf, compactTranscriptLine{
				V:          1,
				Agent:      compactTranscriptAgent,
				CLIVersion: cliVersion,
				Type:       "user",
				TS:         entry.User.Timestamp,
				Content:    []compactUserTextBlock{{Text: prompt}},
			}); err != nil {
				return nil, err
			}
		}

		nextUserContent := json.RawMessage(nil)
		if i+1 < len(transcript.History) {
			nextUserContent = transcript.History[i+1].User.Content
		}

		line, ok := compactAssistantEntry(entry, nextUserContent, cliVersion)
		if !ok {
			continue
		}
		if err := writeCompactTranscriptLine(&buf, line); err != nil {
			return nil, err
		}
	}

	if buf.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return buf.Bytes(), nil
}

func compactAssistantEntry(entry kiroHistoryEntry, nextUserContent json.RawMessage, cliVersion string) (compactTranscriptLine, bool) {
	if len(entry.Assistant) == 0 {
		return compactTranscriptLine{}, false
	}

	base := compactTranscriptLine{
		V:          1,
		Agent:      compactTranscriptAgent,
		CLIVersion: cliVersion,
		Type:       "assistant",
		TS:         entry.User.Timestamp,
	}

	var responseContent kiroResponseContent
	if err := json.Unmarshal(entry.Assistant, &responseContent); err == nil && responseContent.Response.Content != "" {
		base.ID = responseContent.Response.MessageID
		base.Content = []compactAssistantTextBlock{{
			Type: "text",
			Text: responseContent.Response.Content,
		}}
		return base, true
	}

	var toolUseContent kiroToolUseContent
	if err := json.Unmarshal(entry.Assistant, &toolUseContent); err == nil && len(toolUseContent.ToolUse.ToolUses) > 0 {
		base.ID = toolUseContent.ToolUse.MessageID
		results := extractCompactToolResults(nextUserContent)
		blocks := make([]compactAssistantToolUseBlock, 0, len(toolUseContent.ToolUse.ToolUses))
		for _, call := range toolUseContent.ToolUse.ToolUses {
			block := compactAssistantToolUseBlock{
				Type:  "tool_use",
				ID:    call.ID,
				Name:  normalizeCompactToolName(call.Name),
				Input: decodeCompactInput(call.Args),
			}
			if result, ok := results[call.ID]; ok {
				block.Result = &result
			}
			blocks = append(blocks, block)
		}
		base.Content = blocks
		return base, true
	}

	return compactTranscriptLine{}, false
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

func compactCLIVersion(transcript *kiroTranscript) string {
	if transcript != nil {
		if version := strings.TrimSpace(transcript.CLIVersion); version != "" {
			return version
		}
	}
	if version := currentCLIVersion(); version != "" {
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

func extractCompactToolResults(content json.RawMessage) map[string]compactToolResultJSON {
	var parsed kiroToolUseResultsContent
	if err := json.Unmarshal(content, &parsed); err != nil || len(parsed.ToolUseResults.ToolUseResults) == 0 {
		return nil
	}

	results := make(map[string]compactToolResultJSON, len(parsed.ToolUseResults.ToolUseResults))
	for _, result := range parsed.ToolUseResults.ToolUseResults {
		if result.ID == "" {
			continue
		}
		results[result.ID] = compactToolResultJSON{
			Output: compactResultOutput(result.Result),
			Status: compactResultStatus(result),
		}
	}
	return results
}

func compactResultOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return string(raw)
	}
	return string(encoded)
}

func compactResultStatus(result kiroToolUseResult) string {
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case compactToolResultSuccess, compactToolResultError:
		return strings.ToLower(strings.TrimSpace(result.Status))
	}
	if result.IsError {
		return compactToolResultError
	}
	return compactToolResultSuccess
}
