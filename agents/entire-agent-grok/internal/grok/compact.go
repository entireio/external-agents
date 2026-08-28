package grok

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	if looksLikeChatHistory(data) {
		return compactChatHistoryBytes(data)
	}
	records := parseSidecarRecords(data)
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

// chatHistoryTypes are the line types only a Grok chat_history.jsonl carries.
// Entire's own sidecar records use an "event" field instead of "type".
var chatHistoryTypes = map[string]bool{
	"user": true, "assistant": true, "system": true,
	"reasoning": true, "tool_result": true,
}

// looksLikeChatHistory decides which parser to use by decoding line types rather
// than substring-matching the raw bytes. A byte match is whitespace-sensitive
// (`"type": "user"` with a space would miss) and can be fooled by a checkpoint
// slice whose lines happen not to contain the literal it looks for.
func looksLikeChatHistory(data []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Type  string `json:"type"`
			Event string `json:"event"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if probe.Event != "" {
			return false
		}
		if chatHistoryTypes[probe.Type] {
			return true
		}
	}
	return false
}

// parseSidecarRecords parses Entire's own sidecar JSONL, skipping any line it
// cannot decode so a truncated slice does not fail the whole transcript.
func parseSidecarRecords(data []byte) []sidecarRecord {
	lines := bytes.Split(data, []byte("\n"))
	records := make([]sidecarRecord, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var record sidecarRecord
		if err := json.Unmarshal(line, &record); err != nil {
			// Tolerate a truncated first/last line from checkpoint scoping or a
			// concurrent write, the same way the chat-history scanner does.
			continue
		}
		records = append(records, record)
	}
	return records
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
