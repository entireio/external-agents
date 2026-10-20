package hermes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-hermes/internal/protocol"
)

// compactCLIVersion is the Entire CLI version the CLI passes to plugins.
func compactCLIVersion() string {
	if v := strings.TrimSpace(os.Getenv("ENTIRE_CLI_VERSION")); v != "" {
		return v
	}
	return "unknown"
}

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

// maxCallerTranscriptSize bounds a caller-provided compact-transcript input.
const maxCallerTranscriptSize = 128 << 20

func (a *Agent) CompactTranscript(path string) (protocol.CompactTranscriptResponse, error) {
	entries, _, err := readEntries(path, 0)
	if errors.Is(err, errTranscriptPathNotOwned) {
		// The Entire CLI compacts from its own temp copy of the redacted
		// transcript, so accept a plain caller-provided file for this command only.
		entries, err = readCallerTranscript(path)
	}
	if err != nil {
		return protocol.CompactTranscriptResponse{}, err
	}
	var output bytes.Buffer
	version := compactCLIVersion()
	for _, entry := range entries {
		var content any
		typ := entry.Type
		switch entry.Type {
		case entryTypeUser:
			if entry.Content == "" {
				continue
			}
			content = []compactUserBlock{{Text: entry.Content}}
		case entryTypeAssistant:
			if entry.Content == "" {
				continue
			}
			content = []compactAssistantBlock{{Type: "text", Text: entry.Content}}
		case entryTypeTool:
			typ = entryTypeAssistant
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
		line := compactLine{Version: 1, Agent: agentName, CLIVersion: version, Type: typ, Timestamp: entry.Timestamp, Content: content}
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

// readCallerTranscript reads an absolute, regular, non-symlink file of bounded
// size and parses it as an observer transcript.
func readCallerTranscript(path string) ([]transcriptEntry, error) {
	if !filepath.IsAbs(path) {
		return nil, errTranscriptPathNotOwned
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxCallerTranscriptSize {
		return nil, errTranscriptPathNotOwned
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errTranscriptPathNotOwned
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCallerTranscriptSize+1))
	if err != nil {
		return nil, fmt.Errorf("read caller transcript: %w", err)
	}
	if len(data) > maxCallerTranscriptSize {
		return nil, errTranscriptPathNotOwned
	}
	entries, _, err := parseEntries(data, 0)
	return entries, err
}
