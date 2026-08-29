package amp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxTranscriptLine = 10 * 1024 * 1024

// The on-disk transcript is stored as JSONL: exactly one ThreadMessage per
// line, in order, with no thread header line. Entire scopes external-agent
// transcripts by LINE offset (transcript.SliceFromLine) before handing them to
// compact-transcript, so a single-JSON-blob layout would be destroyed by that
// slicing. One-message-per-line keeps line index == message index, which is
// also the unit Entire records via get-transcript-position, so scoping lands on
// message boundaries and header-less slices remain valid JSONL.
//
// The thread id lives on each message (ThreadMessage.ThreadID) rather than in a
// header, so it survives in a scoped slice that starts past line 0.
//
// Line SHAPE matters as much as line COUNT. Entire compacts an unrecognised
// agent's transcript with the generic JSONL reader in
// cmd/entire/cli/transcript/compact/compact.go, which only keeps a line when
// BOTH of these hold:
//
//   - normalizeKind (compact.go:197) finds a TOP-LEVEL "type" or "role" of
//     "user" or "assistant". Amp's message already carries "role", so this
//     half was satisfied by the JSONL layout alone.
//   - parseMessage (compact.go:617) finds the content under a TOP-LEVEL
//     "message" object: "All JSONL agents nest content inside a 'message'
//     object." Amp keeps content in a top-level "content" array, so every
//     compacted line came out with an empty content array — the right number
//     of lines, carrying nothing.
//
// Each record therefore carries the native ThreadMessage fields, authoritative
// for Amp's own decoding, plus an Entire-facing projection (type, timestamp,
// message) built from them. The projection is derived data: decodeTranscript
// ignores it, so a transcript written by an older build still reads, and the
// next export rewrites it with the projection attached.

// errTranscriptNotPrepared reports a session file that still holds raw hook
// payloads: `amp threads export` has not run for it yet.
var errTranscriptNotPrepared = errors.New("amp transcript is not prepared: run prepare-transcript first")

// transcriptRecord is one on-disk JSONL line: Amp's native message with the
// Entire-facing projection alongside it. ThreadMessage is embedded, so its
// keys stay at the top level exactly as before.
type transcriptRecord struct {
	ThreadMessage
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

// wireUsage uses Entire's snake_case token field names (compact.go:517); Amp's
// native camelCase inputTokens/outputTokens read as zero there.
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

// wireToolResultBlock is a user tool result. extractUserContent (compact.go:665)
// reads snake_case tool_use_id, a string "content", and is_error; Entire then
// inlines it into the preceding assistant's matching tool_use block.
type wireToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// newTranscriptRecord projects an Amp message into the on-disk record.
func newTranscriptRecord(msg ThreadMessage) transcriptRecord {
	record := transcriptRecord{
		ThreadMessage: msg,
		Type:          string(msg.Role),
		Timestamp:     threadMessageTimestamp(msg),
	}
	// Only "user" and "assistant" mean anything to normalizeKind. Info
	// messages stay unprojected, matching compactTranscriptBytes, which gives
	// them no compact representation either.
	if msg.Role != ThreadMessageRoleUser && msg.Role != ThreadMessageRoleAssistant {
		return record
	}
	record.Message = &wireMessage{
		Role:    string(msg.Role),
		ID:      threadMessageIDString(msg.MessageID),
		Content: wireContent(msg.Content),
	}
	if msg.Usage != nil {
		record.Message.Usage = &wireUsage{
			InputTokens:  msg.Usage.InputTokens,
			OutputTokens: msg.Usage.OutputTokens,
		}
	}
	return record
}

// wireContent converts Amp content blocks into the blocks Entire's compactJSONL
// understands. Thinking blocks are dropped (compact.go:700 strips them anyway).
func wireContent(blocks []ThreadContentBlock) []any {
	out := make([]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ThreadContentText:
			if block.Text != "" {
				out = append(out, wireTextBlock{Type: "text", Text: block.Text})
			}
		case ThreadContentToolUse, ThreadContentServerToolUse:
			out = append(out, wireToolUseBlock{
				Type:  "tool_use",
				ID:    block.ID,
				Name:  block.Name,
				Input: decodeToolInput(block.Input),
			})
		case ThreadContentToolResult:
			if block.Run == nil {
				continue
			}
			out = append(out, wireToolResultBlock{
				Type:      "tool_result",
				ToolUseID: block.ToolUseID,
				Content:   toolRunOutput(block.Run),
				IsError:   block.Run.Status == "error" || block.Run.Status == "cancelled" || block.Run.Error != nil,
			})
		case ThreadContentImage:
			// Image blocks pass through verbatim: extractUserContent
			// (compact.go:673) keeps them as-is.
			if raw, err := json.Marshal(block); err == nil {
				out = append(out, json.RawMessage(raw))
			}
		case ThreadContentThinking, ThreadContentToolSearchResult:
		}
	}
	return out
}

// encodeMessagesJSONL serializes messages as one JSON object per line.
func encodeMessagesJSONL(messages []ThreadMessage) ([]byte, error) {
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

// materializeThread converts a native `amp threads export` document into the
// stored JSONL layout, stamping the thread id onto every message so a scoped
// slice still identifies its thread.
func materializeThread(thread *Thread) ([]byte, error) {
	if thread == nil {
		return nil, nil
	}
	return encodeMessagesJSONL(stampThreadID(thread.Messages, thread.ID))
}

// stampThreadID returns a copy of messages with threadID recorded on each.
func stampThreadID(messages []ThreadMessage, threadID string) []ThreadMessage {
	out := make([]ThreadMessage, len(messages))
	copy(out, messages)
	if id := strings.TrimSpace(threadID); id != "" {
		for i := range out {
			out[i].ThreadID = id
		}
	}
	return out
}

// decodeTranscript reads a stored transcript, which is one JSON message per
// line (see encodeMessagesJSONL). Blank and non-message lines are skipped, so
// header-less scoped slices decode cleanly. Only the native ThreadMessage
// fields are read: the Entire-facing projection is derived data and is ignored
// here, so a transcript written before the projection existed still decodes.
//
// A whole-document `amp threads export` blob is still accepted so session files
// written before amp materialized JSONL keep reading; the next export rewrites
// them as JSONL. A file that holds only unprepared hook payloads reports
// errTranscriptNotPrepared, as the single-document parser used to.
func decodeTranscript(data []byte) ([]ThreadMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return []ThreadMessage{}, nil
	}
	if export, ok := legacyThreadExport(trimmed); ok {
		// Stamp the id the same way materializeThread does, so a legacy file
		// and its materialized replacement read identically.
		return stampThreadID(export.Messages, export.ID), nil
	}

	messages := make([]ThreadMessage, 0)
	unprepared := false
	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	scanner.Buffer(make([]byte, 64*1024), maxTranscriptLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg ThreadMessage
		if json.Unmarshal(line, &msg) == nil && !isEmptyThreadMessage(msg) {
			messages = append(messages, msg)
			continue
		}
		if isHookPayloadLine(line) {
			unprepared = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan amp transcript: %w", err)
	}
	if len(messages) == 0 && unprepared {
		return nil, errTranscriptNotPrepared
	}
	return messages, nil
}

// parseAmpExport reads Amp's native thread export (a single JSON document with
// a "messages" array). Used only when ingesting a fresh export from
// `amp threads export`; the result is stored as JSONL via materializeThread.
func parseAmpExport(raw []byte) (*Thread, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return &Thread{}, nil
	}
	var thread Thread
	if err := json.Unmarshal(trimmed, &thread); err != nil {
		return nil, fmt.Errorf("parse amp thread export: %w", err)
	}
	return &thread, nil
}

// legacyThreadExport reports whether data is a whole-document thread export
// rather than JSONL. A JSONL payload of two or more lines fails to unmarshal as
// one object, and a single JSONL message line carries neither "id" nor
// "messages", so it is not mistaken for an export.
func legacyThreadExport(data []byte) (*Thread, bool) {
	var thread Thread
	if json.Unmarshal(data, &thread) != nil {
		return nil, false
	}
	if strings.TrimSpace(thread.ID) == "" && thread.Messages == nil {
		return nil, false
	}
	return &thread, true
}

// isEmptyThreadMessage reports a line that carries no message content at all,
// such as a thread header or an unrelated JSON object.
func isEmptyThreadMessage(msg ThreadMessage) bool {
	return msg.Role == "" && msg.MessageID == "" && len(msg.Content) == 0
}

// isHookPayloadLine reports a raw Amp hook payload — what the plugin writes
// before `amp threads export` has materialized the thread.
func isHookPayloadLine(line []byte) bool {
	var payload ampHookPayload
	if json.Unmarshal(line, &payload) != nil {
		return false
	}
	return strings.TrimSpace(payload.ThreadID) != "" || strings.TrimSpace(payload.Type) != ""
}

// atomicWriteFile writes data to path via a temp file + rename so a crash
// mid-write cannot leave a partially-written transcript behind.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".amp-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
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
