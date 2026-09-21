package qwen

import (
	"bytes"
	"encoding/json"
	"strings"
)

// The sidecar is already JSONL — one hook record per line, and
// get-transcript-position reports the record count — so Entire's line-offset
// scoping (transcript.SliceFromLine, cmd/entire/cli/transcript/parse.go:130,
// reached from cmd/entire/cli/explain.go:1883 and
// cmd/entire/cli/strategy/manual_commit_condensation.go:1006) already lands on
// record boundaries. The line COUNT was never the problem.
//
// The line SHAPE was. An external agent's transcript is compacted by the
// generic JSONL reader in cmd/entire/cli/transcript/compact/compact.go, which
// keeps a line only when BOTH of these hold, and a native sidecar record failed
// both, so transcript.jsonl came out at 0 bytes:
//
//   - normalizeKind (compact.go:197) reads a TOP-LEVEL "type", falling back to
//     "role". A sidecar record has neither: its discriminator is "event"
//     ("UserPromptSubmit", "PostToolUse", ...), which normalizeKind does not
//     recognise, so every line was dropped.
//   - parseMessage (compact.go:617) reads content from a TOP-LEVEL "message"
//     OBJECT: "All JSONL agents nest content inside a 'message' object." The
//     sidecar's "message" is a STRING (Qwen's notification text), so even a
//     line that had passed the first check would have carried nothing.
//
// Each record therefore keeps its native fields, authoritative for the
// adapter's own decoding, and gains an Entire-facing projection built from
// them: a top-level "type" and a "message" wrapper. The projection is derived
// data — readSidecarRecords ignores both — so a sidecar written by an older
// build still reads, and later hooks append projected records alongside it.
//
// An adapter-side compact-transcript method does not avoid any of this:
// checkpoint/persistent.go:1136 always runs Entire's generic compactor over the
// raw transcript.

// wireMessage is the "message" wrapper Entire's compactJSONL reads content from
// (compact.go:617).
type wireMessage struct {
	Role    string `json:"role"`
	ID      string `json:"id,omitempty"`
	Content []any  `json:"content"`
}

// wireTextBlock is a text content block, read by both the user and the
// assistant path in compactJSONL.
type wireTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// wireToolUseBlock is an assistant tool call. stripAssistantContent
// (compact.go:704) keeps exactly type, id, name and input — anything else on
// the block, a pre-computed result included, is discarded, which is why the
// result travels on its own record instead.
type wireToolUseBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

// wireToolResultBlock is a user tool result. extractUserContent (compact.go:665)
// reads snake_case tool_use_id, a string "content" and is_error; Entire then
// inlines the output into the preceding assistant's matching tool_use block
// (inlineToolResults, compact.go:454), exactly as it does for Claude Code.
type wireToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// projectSidecarRecord builds the Entire-facing view of a hook record: the
// top-level kind normalizeKind needs, and the message wrapper parseMessage
// needs. It returns ("", nil) for events Entire has no representation for,
// which leaves the record unprojected and therefore dropped from the compact
// transcript — the same outcome compactTranscriptBytes gives them.
//
// Qwen fires PreToolUse before a call and PostToolUse after it, so the pair maps
// onto Entire's assistant tool_use / user tool_result the way Claude Code's own
// transcript does, and each hook still costs exactly one line.
func projectSidecarRecord(record sidecarRecord, nativeMessage string) (string, *wireMessage) {
	switch record.Event {
	case "UserPromptSubmit":
		if strings.TrimSpace(record.Prompt) == "" {
			return "", nil
		}
		return "user", &wireMessage{
			Role:    "user",
			Content: textContent(nativeMessage, record.Prompt),
		}

	case "PreToolUse":
		if strings.TrimSpace(record.ToolName) == "" {
			return "", nil
		}
		content := textContent(nativeMessage)
		content = append(content, wireToolUseBlock{
			Type:  "tool_use",
			ID:    record.ToolUseID,
			Name:  record.ToolName,
			Input: decodeRawObject(record.ToolInput),
		})
		return "assistant", &wireMessage{Role: "assistant", ID: record.ToolUseID, Content: content}

	case "PostToolUse", "PostToolUseFailure":
		output, isError := toolOutcome(record)
		content := textContent(nativeMessage)
		content = append(content, wireToolResultBlock{
			Type:      "tool_result",
			ToolUseID: record.ToolUseID,
			Content:   output,
			IsError:   isError,
		})
		return "user", &wireMessage{Role: "user", Content: content}

	case "Stop", "StopFailure":
		text := record.LastAssistantMessage
		if text == "" {
			text = record.ErrorDetails
		}
		if strings.TrimSpace(text) == "" {
			return "", nil
		}
		return "assistant", &wireMessage{
			Role:    "assistant",
			Content: textContent(nativeMessage, text),
		}

	default:
		return "", nil
	}
}

// toolOutcome renders a tool result as the string Entire expects, and reports
// whether the call failed. tool_response is raw JSON: a JSON string is used as
// its value, anything else as its compact JSON text.
func toolOutcome(record sidecarRecord) (string, bool) {
	if record.Error != "" || record.ErrorDetails != "" {
		return strings.TrimSpace(record.Error + " " + record.ErrorDetails), true
	}
	failed := record.Event == "PostToolUseFailure" || record.IsTimeout || record.IsInterrupt
	return rawMessageText(record.ToolResponse), failed
}

// rawMessageText renders raw JSON as a string: an encoded string keeps its
// value, anything else keeps its JSON text.
func rawMessageText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		return text
	}
	return string(trimmed)
}

// textContent builds the leading text blocks of a projected message, skipping
// empty ones. The record's native "message" text is carried first: the wrapper
// takes over the "message" key, so folding the text into the content is what
// keeps it in the transcript instead of losing it. Qwen only sets it on
// Notification records, which are never projected.
func textContent(texts ...string) []any {
	content := make([]any, 0, len(texts)+1)
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		content = append(content, wireTextBlock{Type: "text", Text: text})
	}
	return content
}

// jsonString encodes s as a JSON string, or nil when it is empty.
func jsonString(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return encoded
}

// jsonObject encodes v, returning nil when it cannot be encoded. A record whose
// projection fails to marshal is written unprojected rather than not at all.
func jsonObject(v any) json.RawMessage {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return encoded
}
