package kilo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const maxTranscriptLine = 10 * 1024 * 1024

// The on-disk transcript is stored as JSONL: exactly one message per line, in
// order, with no header line. Entire scopes external-agent transcripts by LINE
// offset (transcript.SliceFromLine) before handing them to compact-transcript,
// so a single-JSON-blob layout would be destroyed by that slicing.
// One-message-per-line keeps line index == message index, which is also the
// unit Entire records via get-transcript-position, so scoping lands on message
// boundaries and header-less slices remain valid JSONL.
//
// Line SHAPE matters as much as line COUNT. Entire compacts an unrecognised
// agent's transcript with the generic JSONL reader in
// cmd/entire/cli/transcript/compact/compact.go, which only keeps a line when
// BOTH of these hold:
//
//   - normalizeKind (compact.go:197) finds a TOP-LEVEL "type" (or "role")
//     of "user" or "assistant". Kilo's native record nests the role under
//     "info", so every line was dropped and transcript.jsonl came out empty.
//   - parseMessage (compact.go:617) finds the content under a TOP-LEVEL
//     "message" object: "All JSONL agents nest content inside a 'message'
//     object." Kilo's native record keeps content in "parts", so even a line
//     that survived the first check would carry no content.
//
// Each record therefore carries the native fields (info/parts, authoritative
// for Kilo's own decoding) plus an Entire-facing projection (type, timestamp,
// message) built from them. The projection is derived data: decodeTranscript
// ignores it entirely, so a transcript written by an older build still reads,
// and the next hook rewrite (hooks.go) re-encodes it with the projection
// attached.

// transcriptRecord is one on-disk JSONL line: Kilo's native message with the
// Entire-facing projection alongside it. SessionMessage is embedded, so its
// "info" and "parts" keys stay at the top level exactly as before.
type transcriptRecord struct {
	SessionMessage
	Type      string       `json:"type,omitempty"`
	Timestamp string       `json:"timestamp,omitempty"`
	Message   *wireMessage `json:"message,omitempty"`
}

// wireMessage is the "message" wrapper Entire's compactJSONL reads content from.
type wireMessage struct {
	Role    string     `json:"role"`
	ID      string     `json:"id,omitempty"`
	Content []any      `json:"content"`
	Usage   *wireUsage `json:"usage,omitempty"`
}

// wireUsage uses Entire's snake_case token field names (compact.go:517).
type wireUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// wireTextBlock is a text content block, read by both the user and the
// assistant path in compactJSONL.
type wireTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// wireToolUseBlock is an assistant tool call. stripAssistantContent
// (compact.go:704) keeps exactly type, id, name and input.
type wireToolUseBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

// newTranscriptRecord projects a Kilo message into the on-disk record.
func newTranscriptRecord(msg SessionMessage) transcriptRecord {
	record := transcriptRecord{
		SessionMessage: msg,
		Type:           string(msg.Info.Role),
		Timestamp:      messageTimestamp(msg),
	}
	// Only "user" and "assistant" mean anything to normalizeKind; leaving any
	// other role unprojected drops the line, which is what we want.
	if msg.Info.Role != MessageRoleUser && msg.Info.Role != MessageRoleAssistant {
		return record
	}
	record.Message = &wireMessage{
		Role:    string(msg.Info.Role),
		ID:      msg.Info.ID,
		Content: wireContent(msg.Info.Role, msg.Parts),
	}
	if msg.Info.Tokens != nil {
		record.Message.Usage = &wireUsage{
			InputTokens:  msg.Info.Tokens.Input,
			OutputTokens: msg.Info.Tokens.Output,
		}
	}
	return record
}

// wireContent converts Kilo parts into Entire content blocks. Text survives for
// both roles; an assistant tool call becomes a tool_use block so the compact
// transcript records which tool ran with which arguments. Reasoning, file and
// subtask parts have no Entire equivalent and are dropped, matching the
// adapter's own compact output (compact.go:compactAssistantContent).
func wireContent(role MessageRole, parts []MessagePart) []any {
	blocks := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case PartText:
			if part.Text != "" {
				blocks = append(blocks, wireTextBlock{Type: "text", Text: part.Text})
			}
		case PartTool:
			if role != MessageRoleAssistant {
				continue
			}
			blocks = append(blocks, wireToolUseBlock{
				Type:  "tool_use",
				ID:    part.CallID,
				Name:  part.Tool,
				Input: decodeRawInput(part.State),
			})
		case PartReasoning, PartFile, PartSubtask:
		}
	}
	return blocks
}

// encodeMessagesJSONL serializes messages as one JSON object per line.
func encodeMessagesJSONL(messages []SessionMessage) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf) // Encode writes directly into buf and appends '\n'.
	for i := range messages {
		record := newTranscriptRecord(messages[i])
		if err := enc.Encode(&record); err != nil {
			return nil, fmt.Errorf("encode message: %w", err)
		}
	}
	return buf.Bytes(), nil
}

// decodeTranscript reads the on-disk transcript, which is always one JSON
// message per line (see encodeMessagesJSONL). Blank/unparseable lines are
// skipped, so header-less scoped slices decode cleanly. Only the native
// info/parts fields are read: the Entire-facing projection is derived data and
// is ignored here, so a transcript written before the projection existed still
// decodes unchanged. Kilo's native session export blob is handled separately by
// parseKiloExport at the ingestion boundary — it never reaches disk.
func decodeTranscript(data []byte) ([]SessionMessage, error) {
	messages := make([]SessionMessage, 0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxTranscriptLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg SessionMessage
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		if msg.Info.ID == "" && msg.Info.Role == "" && len(msg.Parts) == 0 {
			continue
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan kilo transcript: %w", err)
	}
	return messages, nil
}

// parseKiloExport reads Kilo's native session export (a single JSON object with
// a "messages" array) into its messages. Used only when ingesting a fresh
// export from the plugin payload or `kilo session show`; the result is stored
// as JSONL via encodeMessagesJSONL.
func parseKiloExport(raw []byte) ([]SessionMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var export struct {
		Messages []SessionMessage `json:"messages"`
	}
	if err := json.Unmarshal(trimmed, &export); err != nil {
		return nil, fmt.Errorf("parse kilo session export: %w", err)
	}
	return export.Messages, nil
}

// atomicWriteFile writes data to path via a temp file + rename so a crash
// mid-write cannot leave a partially-written transcript behind.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".kilo-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Write(data); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
