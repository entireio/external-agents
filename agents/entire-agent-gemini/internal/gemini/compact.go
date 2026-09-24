package gemini

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-gemini/internal/protocol"
)

func (a *Agent) CompactTranscript(sessionRef string) (protocol.CompactTranscriptResponse, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return protocol.CompactTranscriptResponse{}, fmt.Errorf("read transcript: %w", err)
	}

	// Parse sidecar JSONL and build compact representation
	type compactEntry struct {
		Event      string `json:"event"`
		Prompt     string `json:"prompt,omitempty"`
		Model      string `json:"model,omitempty"`
		ToolName   string `json:"tool_name,omitempty"`
		Response   string `json:"last_assistant_message,omitempty"`
		Summary    string `json:"compact_summary,omitempty"`
		TS         string `json:"ts,omitempty"`
	}

	lines := splitLines(data)
	var entries []compactEntry
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var rec sidecarRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		entries = append(entries, compactEntry{
			Event:    rec.Event,
			Prompt:   rec.Prompt,
			Model:    rec.Model,
			ToolName: rec.ToolName,
			Response: rec.LastAssistantMessage,
			Summary:  rec.CompactSummary,
			TS:       rec.TS,
		})
	}

	compact, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return protocol.CompactTranscriptResponse{}, err
	}

	return protocol.CompactTranscriptResponse{
		Transcript: string(compact),
	}, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
