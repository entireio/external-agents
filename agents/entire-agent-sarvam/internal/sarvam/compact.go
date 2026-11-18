package sarvam

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-sarvam/internal/protocol"
)

// CompactTranscript projects the native rollout JSONL into Entire
// Transcript Format: one v:1 unified record per line, the shape the
// server's unified parser admits (isUnifiedTranscript requires a
// top-level content array on the first line) and renders — the same
// envelope qwen's and grok's compactors emit. The earlier projection
// mirrored the wire shapes Entire's in-CLI compactor READS
// ({"type","message":{…}}), which the server rejects: checkpoints stored
// it, but the session page rendered nothing.
//
// Projection:
//
//	response_item message/user      -> user line, content [{"text": …}]
//	  (input_text, minus the injected <environment_context> harness block)
//	response_item message/assistant -> assistant line, content
//	  [{"type":"text","text":…}]
//	function_call                   -> assistant line with a tool_use block;
//	  the matching function_call_output is nested as the block's result,
//	  with a non-zero exec exit code reported as status "error"
//
// Developer messages, reasoning, and the event stream are dropped: none of
// them belong in a checkpoint transcript.
//
// The input is a temp copy of an already-redacted snapshot, so a final line
// without a trailing newline is finished data and is kept when it parses —
// unlike the live-transcript readers, which must never cover a
// half-written record.
func (a *Agent) CompactTranscript(sessionRef string) (protocol.CompactTranscriptResponse, error) {
	lines, err := readFinishedRolloutLines(sessionRef)
	if err != nil {
		if os.IsNotExist(err) {
			lines = nil
		} else {
			return protocol.CompactTranscriptResponse{}, err
		}
	}

	// First pass: the CLI version from session_meta and each call's output,
	// so a tool_use block can nest the result that the rollout records as
	// a separate, later line.
	cliVersion := "unknown"
	toolResults := map[string]compactToolResult{}
	for _, line := range lines {
		if line.Type == "session_meta" {
			var meta struct {
				CLIVersion string `json:"cli_version"`
			}
			if json.Unmarshal(line.Payload, &meta) == nil && strings.TrimSpace(meta.CLIVersion) != "" {
				cliVersion = strings.TrimSpace(meta.CLIVersion)
			}
			continue
		}
		if line.Type != rolloutLineTypeResponseItem {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line.Payload, &probe) != nil || probe.Type != "function_call_output" {
			continue
		}
		var output rolloutFunctionCallOutput
		if json.Unmarshal(line.Payload, &output) != nil {
			continue
		}
		toolResults[output.CallID] = compactToolResultFromOutput(output.Output)
	}

	var out bytes.Buffer
	for _, line := range lines {
		if line.Type != rolloutLineTypeResponseItem {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line.Payload, &probe) != nil {
			continue
		}
		switch probe.Type {
		case "message":
			var message rolloutMessage
			if json.Unmarshal(line.Payload, &message) != nil {
				continue
			}
			switch message.Role {
			case roleUser:
				text := messageText(message, "input_text")
				if text == "" || isHarnessContext(text) {
					continue
				}
				if err := writeCompactLine(&out, compactLine{
					V:          1,
					Agent:      AgentName,
					CLIVersion: cliVersion,
					Type:       roleUser,
					TS:         line.Timestamp,
					Content:    []compactUserTextBlock{{Text: text}},
				}); err != nil {
					return protocol.CompactTranscriptResponse{}, err
				}
			case roleAssistant:
				text := messageText(message, "output_text")
				if strings.TrimSpace(text) == "" {
					continue
				}
				if err := writeCompactLine(&out, compactLine{
					V:          1,
					Agent:      AgentName,
					CLIVersion: cliVersion,
					Type:       roleAssistant,
					TS:         line.Timestamp,
					Content:    []compactAssistantTextBlock{{Type: "text", Text: text}},
				}); err != nil {
					return protocol.CompactTranscriptResponse{}, err
				}
			}
		case "function_call":
			var call rolloutFunctionCall
			if json.Unmarshal(line.Payload, &call) != nil {
				continue
			}
			block := compactToolUseBlock{
				Type:  "tool_use",
				ID:    call.CallID,
				Name:  call.Name,
				Input: decodeToolArguments(call.Arguments),
			}
			if result, ok := toolResults[call.CallID]; ok {
				block.Result = &result
			}
			if err := writeCompactLine(&out, compactLine{
				V:          1,
				Agent:      AgentName,
				CLIVersion: cliVersion,
				Type:       roleAssistant,
				TS:         line.Timestamp,
				ID:         call.CallID,
				Content:    []any{block},
			}); err != nil {
				return protocol.CompactTranscriptResponse{}, err
			}
		}
	}

	if out.Len() == 0 {
		return protocol.CompactTranscriptResponse{}, errors.New("compact transcript produced no output")
	}
	return protocol.CompactTranscriptResponse{
		Transcript: base64.StdEncoding.EncodeToString(out.Bytes()),
	}, nil
}

// compactLine is one Entire Transcript Format record: the top-level
// content array the server's unified parser reads, with the v, agent and
// ts envelope qwen's compactor emits.
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

// compactToolResultFromOutput derives the status the way qwen's compactor
// does: a non-zero exec exit code is an error, anything else success. An
// output that is not the CLI's {"output",...,"metadata"} envelope is kept
// verbatim.
func compactToolResultFromOutput(output string) compactToolResult {
	var parsed struct {
		Output   string `json:"output"`
		Metadata struct {
			ExitCode int `json:"exit_code"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(output), &parsed) == nil && parsed.Output != "" {
		status := "success"
		if parsed.Metadata.ExitCode != 0 {
			status = "error"
		}
		return compactToolResult{Output: parsed.Output, Status: status}
	}
	return compactToolResult{Output: output, Status: "success"}
}

func writeCompactLine(buf *bytes.Buffer, line compactLine) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return err
	}
	buf.Write(encoded)
	buf.WriteByte('\n')
	return nil
}

// decodeToolArguments parses the JSON-encoded arguments string into an object;
// a non-object or unparseable payload is forwarded as-is.
func decodeToolArguments(arguments string) any {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return map[string]any{}
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return trimmed
	}
	return value
}
