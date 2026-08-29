package goose

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxTranscriptLine = 10 * 1024 * 1024

// The materialized transcript is stored as JSONL: exactly one conversation
// message per line, in order, with no header line. Entire scopes
// external-agent transcripts by LINE offset (transcript.SliceFromLine,
// cmd/entire/cli/transcript/parse.go:130) before compacting them — see
// cmd/entire/cli/explain.go:1883 and
// cmd/entire/cli/strategy/manual_commit_condensation.go:1006 — so goose's
// native single-JSON-document export was destroyed by that slicing: every
// checkpoint compacted a fragment of a pretty-printed object and produced
// nothing. One message per line keeps line index == message index, which is
// also the unit get-transcript-position reports (len(Conversation)), so scoping
// lands on message boundaries and header-less slices stay valid JSONL.
//
// Line SHAPE matters as much as line COUNT. An external agent's transcript is
// compacted by the generic JSONL reader in
// cmd/entire/cli/transcript/compact/compact.go, which keeps a line only when
// BOTH of these hold:
//
//   - normalizeKind (compact.go:197) finds a TOP-LEVEL "type", falling back to
//     "role", of "user" or "assistant". Goose messages already carry "role", so
//     JSONL alone satisfies this half.
//   - parseMessage (compact.go:617) finds the content under a TOP-LEVEL
//     "message" object: "All JSONL agents nest content inside a 'message'
//     object." Goose keeps content in a top-level "content" array, so a line
//     that survived the first check would still compact to an empty content
//     array — the right number of lines, carrying nothing.
//
// Each record therefore carries the native gooseMessage fields, authoritative
// for goose's own decoding, plus an Entire-facing projection (type, timestamp,
// message) built from them. The projection is derived data: parseGooseExport
// ignores it, so a transcript written by an older build still reads, and the
// next export rewrites it with the projection attached.
//
// The export's session-level fields (name, accumulated token totals,
// model_config) are stamped onto EVERY record, the way amp stamps its thread
// id. They cannot live in a header: SliceFromLine hands a mid-session
// checkpoint a slice that starts past line 0, and a header there is gone. With
// the fields on each record, ExtractSummary, CalculateTokens and
// modelFromSessionRef keep working on any scoped slice.

// gooseSessionMeta is the export's session-level header. It is embedded in
// gooseExport, so a native `goose session export --format json` document still
// unmarshals with these keys at the top level, and it is stamped onto every
// stored JSONL record under "session".
type gooseSessionMeta struct {
	ID                     string            `json:"id,omitempty"`
	WorkingDir             string            `json:"working_dir,omitempty"`
	Name                   string            `json:"name,omitempty"`
	CreatedAt              string            `json:"created_at,omitempty"`
	AccumulatedInputTokens int               `json:"accumulated_input_tokens,omitempty"`
	AccumulatedOutput      int               `json:"accumulated_output_tokens,omitempty"`
	ProviderName           string            `json:"provider_name,omitempty"`
	ModelConfig            *gooseModelConfig `json:"model_config,omitempty"`
}

type gooseModelConfig struct {
	ModelName string `json:"model_name,omitempty"`
}

// transcriptRecord is one on-disk JSONL line: goose's native message with the
// stamped session header and the Entire-facing projection alongside it.
// gooseMessage is embedded, so its id/role/created/content keys stay at the top
// level exactly as they appear inside the native export's conversation array.
type transcriptRecord struct {
	gooseMessage
	Session   *gooseSessionMeta `json:"session,omitempty"`
	Type      string            `json:"type,omitempty"`
	Timestamp string            `json:"timestamp,omitempty"`
	Message   *wireMessage      `json:"message,omitempty"`
}

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
// the block, a pre-computed result included, is discarded.
type wireToolUseBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

// wireToolResultBlock is a user tool result. extractUserContent (compact.go:665)
// reads snake_case tool_use_id, a string "content" and is_error; Entire then
// inlines the output into the preceding assistant's matching tool_use block,
// exactly as it does for Claude Code. Goose already records the request and the
// response as two separate conversation messages, so the pairing is natural and
// keeps one message per line.
type wireToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// newTranscriptRecord projects one conversation message into a stored record.
func newTranscriptRecord(meta gooseSessionMeta, msg gooseMessage) transcriptRecord {
	stamped := meta
	record := transcriptRecord{
		gooseMessage: msg,
		Session:      &stamped,
		Type:         msg.Role,
		Timestamp:    recordTimestamp(msg.Created),
	}
	// Only "user" and "assistant" mean anything to normalizeKind; any other
	// role stays unprojected, which drops the line — the same outcome
	// compactTranscriptBytes gives it.
	if msg.Role != "user" && msg.Role != "assistant" {
		return record
	}
	record.Message = &wireMessage{
		Role:    msg.Role,
		ID:      msg.ID,
		Content: wireContent(msg.Content),
	}
	return record
}

// wireContent converts goose content blocks into the blocks compactJSONL
// understands. Goose records no per-message token usage (see CalculateTokens),
// so no usage object is projected.
func wireContent(blocks []gooseContent) []any {
	out := make([]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				out = append(out, wireTextBlock{Type: "text", Text: block.Text})
			}
		case "toolRequest":
			if block.ToolCall == nil {
				continue
			}
			out = append(out, wireToolUseBlock{
				Type:  "tool_use",
				ID:    block.ID,
				Name:  block.ToolCall.Value.Name,
				Input: decodeToolInput(block.ToolCall.Value.Arguments),
			})
		case "toolResponse":
			if block.ToolResult == nil {
				continue
			}
			out = append(out, wireToolResultBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   toolResponseText(block),
				IsError:   block.ToolResult.Status != "success" || block.ToolResult.Value.IsError,
			})
		}
	}
	return out
}

// toolResponseText flattens a goose toolResult's text parts, matching how
// collectToolResults builds the adapter's own compact output.
func toolResponseText(block gooseContent) string {
	var parts []string
	for _, content := range block.ToolResult.Value.Content {
		if content.Type == "text" && content.Text != "" {
			parts = append(parts, content.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func recordTimestamp(created int64) string {
	if created <= 0 {
		return ""
	}
	return time.Unix(created, 0).UTC().Format(time.RFC3339)
}

// encodeSessionJSONL serializes an export as one conversation message per line.
func encodeSessionJSONL(export *gooseExport) ([]byte, error) {
	if export == nil {
		return nil, errors.New("nil goose export")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf) // Encode writes into buf and appends '\n'.
	for _, msg := range export.Conversation {
		record := newTranscriptRecord(export.gooseSessionMeta, msg)
		if err := enc.Encode(&record); err != nil {
			return nil, fmt.Errorf("encode goose message: %w", err)
		}
	}
	return buf.Bytes(), nil
}

// decodeSessionJSONL rebuilds an export from stored JSONL. The session header
// comes from the records themselves (see the stamping note above); the last
// record carrying one wins, so the most recent export's totals are used. Only
// the native gooseMessage fields are read — the Entire-facing projection is
// derived data and is ignored, so a transcript written before the projection
// existed still decodes and the next export self-heals it.
func decodeSessionJSONL(data []byte) (*gooseExport, error) {
	export := &gooseExport{Conversation: []gooseMessage{}}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxTranscriptLine)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var record transcriptRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, fmt.Errorf("parse goose transcript line %d: %w", line, err)
		}
		if record.Session != nil {
			export.gooseSessionMeta = *record.Session
		}
		if record.Role == "" && record.ID == "" && len(record.Content) == 0 {
			continue
		}
		export.Conversation = append(export.Conversation, record.gooseMessage)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan goose transcript: %w", err)
	}
	return export, nil
}

// legacyGooseExport reports whether data is a native, whole-document
// `goose session export --format json` result rather than stored JSONL. The
// "conversation" key is the discriminator: a stored record never has one, and a
// JSONL payload of two or more lines does not unmarshal as a single object.
func legacyGooseExport(data []byte) (*gooseExport, bool) {
	var probe struct {
		Conversation *json.RawMessage `json:"conversation"`
	}
	if json.Unmarshal(data, &probe) != nil || probe.Conversation == nil {
		return nil, false
	}
	var export gooseExport
	if json.Unmarshal(data, &export) != nil {
		return nil, false
	}
	return &export, true
}

// materializeExport converts a native export document into the stored JSONL
// layout.
func materializeExport(raw []byte) ([]byte, bool) {
	export, ok := legacyGooseExport(bytes.TrimSpace(raw))
	if !ok {
		return nil, false
	}
	data, err := encodeSessionJSONL(export)
	if err != nil {
		return nil, false
	}
	return data, true
}

// atomicWriteFile writes data via a temp file + rename so a crash mid-write
// cannot leave a partially-written transcript behind.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".goose-write-*")
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
