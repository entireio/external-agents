package hermes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/entireio/external-agents/agents/entire-agent-hermes/internal/protocol"
)

type compactLine struct {
	Version    int    `json:"v"`
	Agent      string `json:"agent"`
	CLIVersion string `json:"cli_version"`
	Type       string `json:"type"`
	Timestamp  string `json:"ts,omitempty"`
	Content    any    `json:"content"`
}

type compactUserBlock struct {
	Text string `json:"text"`
}

type compactAssistantBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type compactToolBlock struct {
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Input  map[string]any    `json:"input"`
	Result compactToolResult `json:"result"`
}

type compactToolResult struct {
	Status string `json:"status"`
}

func (a *Agent) CompactTranscript(path string) (protocol.CompactTranscriptResponse, error) {
	entries, _, err := readEntries(path, 0)
	if err != nil {
		return protocol.CompactTranscriptResponse{}, err
	}
	var output bytes.Buffer
	for _, entry := range entries {
		var content any
		typ := entry.Type
		switch entry.Type {
		case "user":
			if entry.Content == "" {
				continue
			}
			content = []compactUserBlock{{Text: entry.Content}}
		case "assistant":
			if entry.Content == "" {
				continue
			}
			content = []compactAssistantBlock{{Type: "text", Text: entry.Content}}
		case "tool":
			typ = "assistant"
			status := entry.Status
			if status == "ok" {
				status = "success"
			}
			if status != "success" && status != "error" && status != "blocked" {
				status = "unknown"
			}
			content = []compactToolBlock{{
				Type:   "tool_use",
				Name:   entry.Name,
				Input:  map[string]any{"modified_files": entry.ModifiedFiles},
				Result: compactToolResult{Status: status},
			}}
		default:
			continue
		}
		line := compactLine{Version: 1, Agent: agentName, CLIVersion: "unknown", Type: typ, Timestamp: entry.Timestamp, Content: content}
		data, err := json.Marshal(line)
		if err != nil {
			return protocol.CompactTranscriptResponse{}, err
		}
		output.Write(data)
		output.WriteByte('\n')
	}
	if output.Len() == 0 {
		return protocol.CompactTranscriptResponse{}, errors.New("compact transcript produced no output")
	}
	return protocol.CompactTranscriptResponse{Transcript: base64.StdEncoding.EncodeToString(output.Bytes())}, nil
}
