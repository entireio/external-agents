package windsurf

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/entireio/external-agents/agents/entire-agent-windsurf/internal/protocol"
)

const compactAgent = "windsurf"

type compactLine struct {
	V          int    `json:"v"`
	Agent      string `json:"agent"`
	CLIVersion string `json:"cli_version"`
	Type       string `json:"type"` // "user" or "assistant"
	TS         string `json:"ts,omitempty"`
	Content    any    `json:"content"`
}

type compactTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
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
	records, err := parseTranscriptBytes(data)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("transcript has no records")
	}

	cliVersion := currentCLIVersion()
	var buf bytes.Buffer
	for _, rec := range records {
		var line compactLine
		switch rec.Type {
		case transcriptTypePrompt:
			line = compactLine{
				V:          1,
				Agent:      compactAgent,
				CLIVersion: cliVersion,
				Type:       "user",
				TS:         rec.TS,
				Content:    []map[string]string{{"text": rec.Content}},
			}
		case transcriptTypeResponse:
			line = compactLine{
				V:          1,
				Agent:      compactAgent,
				CLIVersion: cliVersion,
				Type:       "assistant",
				TS:         rec.TS,
				Content:    []compactTextBlock{{Type: "text", Text: rec.Content}},
			}
		default:
			continue
		}
		encoded, err := json.Marshal(line)
		if err != nil {
			return nil, fmt.Errorf("marshal compact line: %w", err)
		}
		buf.Write(encoded)
		buf.WriteByte('\n')
	}

	if buf.Len() == 0 {
		return nil, errors.New("compact transcript produced no output")
	}
	return buf.Bytes(), nil
}

func currentCLIVersion() string {
	v := os.Getenv("ENTIRE_CLI_VERSION")
	if v == "" {
		return "unknown"
	}
	return v
}
