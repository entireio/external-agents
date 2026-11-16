package sarvam

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-sarvam/internal/protocol"
)

// CompactTranscript converts a native rollout JSONL into Entire Transcript
// Format. Entire calls this on each checkpoint write with a temp file of the
// already-redacted transcript; without it, Entire's generic JSONL compactor
// drops sarvam's `response_item` records because they carry no top-level
// role/message shape it recognizes.
//
// Projection (mirrors the wire shapes Entire's compactor reads):
//
//	response_item message/user     -> {"type":"user","message":{...}}
//	response_item message/assistant-> {"type":"assistant","message":{...}}
//	function_call                  -> assistant message with a tool_use block
//	function_call_output           -> user message with a tool_result block
//
// Developer messages (injected harness instructions), reasoning, and the
// event stream are dropped: none of them belong in a checkpoint transcript.
func (a *Agent) CompactTranscript(sessionRef string) (protocol.CompactTranscriptResponse, error) {
	lines, err := readRolloutLines(sessionRef)
	if err != nil {
		if os.IsNotExist(err) {
			lines = nil
		} else {
			return protocol.CompactTranscriptResponse{}, err
		}
	}

	var out strings.Builder
	for _, line := range lines {
		for _, record := range projectRolloutLine(line) {
			encoded, err := json.Marshal(wireRecord{Type: record.Kind, Message: record.Message})
			if err != nil {
				continue
			}
			out.Write(encoded)
			out.WriteByte('\n')
		}
	}

	return protocol.CompactTranscriptResponse{
		Transcript: base64.StdEncoding.EncodeToString([]byte(out.String())),
	}, nil
}

// wireRecord is the top-level shape Entire's transcript compactor reads: a
// top-level "type" (user/assistant) with the conversation nested under
// "message".
type wireRecord struct {
	Type    string      `json:"type"`
	Message wireMessage `json:"message"`
}

// projectedRecord pairs the wire kind with the message it carries.
type projectedRecord struct {
	Kind    string
	Message wireMessage
}

func projectRolloutLine(line rolloutLine) []projectedRecord {
	if line.Type != "response_item" {
		return nil
	}

	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line.Payload, &probe) != nil {
		return nil
	}

	switch probe.Type {
	case "message":
		var message rolloutMessage
		if json.Unmarshal(line.Payload, &message) != nil {
			return nil
		}
		return projectMessage(message)

	case "function_call":
		var call rolloutFunctionCall
		if json.Unmarshal(line.Payload, &call) != nil {
			return nil
		}
		return []projectedRecord{{
			Kind: "assistant",
			Message: wireMessage{
				Role: "assistant",
				Content: []any{wireToolUseBlock{
					Type:  "tool_use",
					ID:    call.CallID,
					Name:  call.Name,
					Input: decodeToolArguments(call.Arguments),
				}},
			},
		}}

	case "function_call_output":
		var output rolloutFunctionCallOutput
		if json.Unmarshal(line.Payload, &output) != nil {
			return nil
		}
		return []projectedRecord{{
			Kind: "user",
			Message: wireMessage{
				Role: "user",
				Content: []any{wireToolResultBlock{
					Type:      "tool_result",
					ToolUseID: output.CallID,
					Content:   output.Output,
				}},
			},
		}}

	default:
		return nil
	}
}

func projectMessage(message rolloutMessage) []projectedRecord {
	switch message.Role {
	case "user":
		text := messageText(message, "input_text")
		if text == "" || isHarnessContext(text) {
			return nil
		}
		return []projectedRecord{{
			Kind:    "user",
			Message: wireMessage{Role: "user", Content: []any{wireTextBlock{Type: "text", Text: text}}},
		}}
	case "assistant":
		text := messageText(message, "output_text")
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []projectedRecord{{
			Kind:    "assistant",
			Message: wireMessage{Role: "assistant", Content: []any{wireTextBlock{Type: "text", Text: text}}},
		}}
	default:
		return nil
	}
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

// wireMessage is the "message" wrapper Entire's transcript compactor reads.
type wireMessage struct {
	Role    string `json:"role"`
	ID      string `json:"id,omitempty"`
	Content []any  `json:"content"`
}

type wireTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireToolUseBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

type wireToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}
