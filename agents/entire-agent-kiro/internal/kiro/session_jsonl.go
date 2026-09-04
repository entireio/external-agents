package kiro

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

// roleUser and roleAssistant are the only two kinds normalizeKind
// (cmd/entire/cli/transcript/compact/compact.go:197) accepts. A line that
// yields neither is dropped from the compact transcript.
const (
	roleUser      = "user"
	roleAssistant = "assistant"
)

// The materialized transcript is stored as JSONL: one message per line, in
// order, with no header line. Entire scopes external-agent transcripts by LINE
// offset (transcript.SliceFromLine, cmd/entire/cli/transcript/parse.go:130)
// before compacting them — see cmd/entire/cli/explain.go:1883
// (scopeTranscriptForCheckpoint) and
// cmd/entire/cli/strategy/manual_commit_condensation.go:1006 — so kiro's native
// single-JSON-document transcript was destroyed by that slicing: every
// checkpoint compacted a fragment of one pretty-printed object and produced
// nothing at all.
//
// Line SHAPE matters as much as line COUNT. An external agent's transcript is
// compacted by the generic JSONL reader in
// cmd/entire/cli/transcript/compact/compact.go, which keeps a line only when
// BOTH of these hold:
//
//   - normalizeKind (compact.go:197) finds a TOP-LEVEL "type", falling back to
//     "role", of "user" or "assistant".
//   - parseMessage (compact.go:617) finds the content under a TOP-LEVEL
//     "message" object: "All JSONL agents nest content inside a 'message'
//     object."
//
// Kiro satisfied neither: its history entries carry content under "user" and
// "assistant" keys of a paired entry, which is not where Entire looks, and
// there is no top-level "type" at all.
//
// ONE HISTORY ENTRY BECOMES TWO LINES. A kiroHistoryEntry is PAIRED —
// {"user":…,"assistant":…} — and normalizeKind yields exactly one kind per
// line, so a paired entry cannot survive as a single line without discarding
// one of its halves. The user half and the assistant half are therefore
// emitted as separate records carrying the same "entry" index, which is what
// decodeTranscriptJSONL groups them back by.
//
// That doubling is why get-transcript-position now reports a LINE count rather
// than len(History). The unit change is safe because a position and the bytes
// it indexes are always stored together: a checkpoint keeps its own transcript
// blob (checkpoint.SessionContent.Transcript, api/checkpoint/metadata.go:376)
// alongside its own CheckpointTranscriptStart, and Entire re-applies that
// position only to that blob — "CheckpointTranscriptStart indexes the stored
// format" (cmd/entire/cli/explain.go:2132). A historical checkpoint written by
// an older build therefore keeps reading in its own units forever; nothing ever
// applies an entry-count position to a line-counted file.
//
// The one window where units can cross is a session already in flight when the
// binary is upgraded, whose live state still holds an entry-count
// CheckpointTranscriptStart. Applying it to a line-counted file skips too few
// lines, so that single checkpoint over-includes some already-checkpointed
// content — the bounded head overlap Entire's own compactor documents as
// tolerable (compact.go:139-147) — and never drops any. It self-heals at the
// next condensation, which rewrites CheckpointTranscriptStart from the new
// count (cmd/entire/cli/strategy/manual_commit_hooks.go:1529,1548).
//
// The transcript's session-level fields (conversation_id, cli_version) are
// stamped onto EVERY record rather than kept in a header. They cannot live in a
// header: SliceFromLine hands a mid-session checkpoint a slice that starts past
// line 0, and a header there is gone.

// transcriptRecord is one on-disk JSONL line: one half of a kiro history entry,
// the stamped session-level fields, and the Entire-facing projection.
//
// User and Assistant hold kiro's native payloads verbatim and are authoritative
// for kiro's own decoding. Type/Timestamp/Message are derived data for Entire's
// compactor; parsing ignores them, so a record written by a different build
// still reads.
type transcriptRecord struct {
	ConversationID string `json:"conversation_id,omitempty"`
	CLIVersion     string `json:"cli_version,omitempty"`

	// Entry is the index of the source history entry this record is one half
	// of. It is a pointer with no omitempty so it is always emitted (entry 0
	// included) and its absence is detectable: it is the marker that tells
	// parseTranscript a document is JSONL rather than a legacy whole-document
	// transcript.
	Entry *int `json:"entry"`

	User      *kiroUserMessage `json:"user,omitempty"`
	Assistant json.RawMessage  `json:"assistant,omitempty"`

	Type      string       `json:"type,omitempty"`
	Timestamp string       `json:"timestamp,omitempty"`
	Message   *wireMessage `json:"message,omitempty"`
}

// wireMessage is the "message" wrapper Entire's compactJSONL reads content from
// (compact.go:617).
type wireMessage struct {
	Role    string `json:"role"`
	ID      string `json:"id,omitempty"`
	Content []any  `json:"content"`
}

// wireTextBlock is a text content block, read by both the user path
// (extractUserContent, compact.go:665) and the assistant path
// (stripAssistantContent, compact.go:704).
type wireTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// wireToolUseBlock is an assistant tool call. stripAssistantContent
// (compact.go:704) keeps exactly type/id/name/input from a "tool_use" block and
// discards everything else, so those are the only fields worth emitting.
type wireToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

// wireToolResultBlock is a user-side tool result. extractUserContent
// (compact.go:665) reads snake_case "tool_use_id" and a STRING "content";
// inlineToolResults (compact.go:454) then matches it back onto the assistant's
// tool_use block by that id.
type wireToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// materializeTranscript converts a native kiro transcript document into the
// stored JSONL form. It reports false when data is not a transcript this build
// understands, in which case callers store the bytes verbatim — an unrecognised
// kiro version must not break checkpointing.
func materializeTranscript(data []byte) ([]byte, bool) {
	parsed, err := parseTranscript(data)
	if err != nil || len(parsed.History) == 0 {
		return nil, false
	}
	encoded, err := encodeTranscriptJSONL(parsed)
	if err != nil || len(encoded) == 0 {
		return nil, false
	}
	return encoded, true
}

// encodeTranscriptJSONL renders a transcript as JSONL, emitting the user half
// and the assistant half of each history entry as separate records sharing an
// "entry" index.
//
// A half with no native payload is skipped rather than emitted empty: a line
// carrying a "type" but no content would compact into an empty envelope, which
// is indistinguishable from the bug this format exists to fix. A half whose
// payload this build cannot project into content blocks is still written (so
// kiro's own extractors keep seeing it) but carries no "type", so normalizeKind
// (compact.go:197) drops it instead of emitting an empty envelope.
func encodeTranscriptJSONL(t *kiroTranscript) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	for i := range t.History {
		entry := t.History[i]
		idx := i

		if len(entry.User.Content) > 0 {
			rec := transcriptRecord{
				ConversationID: t.ConversationID,
				CLIVersion:     t.CLIVersion,
				Entry:          &idx,
				User:           &entry.User,
				Timestamp:      entry.User.Timestamp,
			}
			if blocks := userContentBlocks(entry.User.Content); len(blocks) > 0 {
				rec.Type = roleUser
				rec.Message = &wireMessage{Role: roleUser, Content: blocks}
			}
			if err := enc.Encode(rec); err != nil {
				return nil, fmt.Errorf("encode kiro transcript: %w", err)
			}
		}

		if len(entry.Assistant) > 0 {
			rec := transcriptRecord{
				ConversationID: t.ConversationID,
				CLIVersion:     t.CLIVersion,
				Entry:          &idx,
				Assistant:      entry.Assistant,
				Timestamp:      entry.User.Timestamp,
			}
			if blocks, id := assistantContentBlocks(entry.Assistant); len(blocks) > 0 {
				rec.Type = roleAssistant
				rec.Message = &wireMessage{Role: roleAssistant, ID: id, Content: blocks}
			}
			if err := enc.Encode(rec); err != nil {
				return nil, fmt.Errorf("encode kiro transcript: %w", err)
			}
		}
	}

	return buf.Bytes(), nil
}

// userContentBlocks projects a kiro user payload — a tagged union of Prompt and
// ToolUseResults — into Claude content blocks.
func userContentBlocks(content json.RawMessage) []any {
	if prompt := extractUserPrompt(content); prompt != "" {
		return []any{wireTextBlock{Type: "text", Text: prompt}}
	}

	var results kiroToolUseResultsContent
	if err := json.Unmarshal(content, &results); err != nil {
		return nil
	}
	var blocks []any
	for _, r := range results.ToolUseResults.ToolUseResults {
		blocks = append(blocks, wireToolResultBlock{
			Type:      "tool_result",
			ToolUseID: r.ID,
			Content:   toolResultText(r.Result),
			IsError:   r.IsError || r.Status == compactToolResultError,
		})
	}
	return blocks
}

// toolResultText renders a kiro tool result payload as the plain string
// extractUserContent (compact.go:665) expects under "content". A result that is
// already a JSON string is unquoted; anything else is kept as its JSON text.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// assistantContentBlocks projects a kiro assistant payload — a tagged union of
// Response and ToolUse — into Claude content blocks, returning the blocks and
// kiro's message id.
func assistantContentBlocks(raw json.RawMessage) ([]any, string) {
	var response kiroResponseContent
	if err := json.Unmarshal(raw, &response); err == nil && response.Response.Content != "" {
		return []any{wireTextBlock{Type: "text", Text: response.Response.Content}}, response.Response.MessageID
	}

	var toolUse kiroToolUseContent
	if err := json.Unmarshal(raw, &toolUse); err == nil && len(toolUse.ToolUse.ToolUses) > 0 {
		var blocks []any
		for _, call := range toolUse.ToolUse.ToolUses {
			blocks = append(blocks, wireToolUseBlock{
				Type:  "tool_use",
				ID:    call.ID,
				Name:  call.Name,
				Input: call.Args,
			})
		}
		return blocks, toolUse.ToolUse.MessageID
	}

	return nil, ""
}

var errNotTranscriptJSONL = errors.New("not kiro transcript JSONL")

// decodeTranscriptJSONL reads the stored JSONL form back into a transcript,
// regrouping records into paired history entries by their "entry" index.
//
// It tolerates a slice that starts mid-entry: SliceFromLine cuts on line
// boundaries, not entry boundaries, so the first entry of a scoped transcript
// may have lost its user half. Such an entry is reconstructed with only the
// half that survived.
func decodeTranscriptJSONL(data []byte) (*kiroTranscript, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxTranscriptLine)

	t := &kiroTranscript{}
	var cur *kiroHistoryEntry
	curEntry := 0
	saw := false

	flush := func() {
		if cur != nil {
			t.History = append(t.History, *cur)
			cur = nil
		}
	}

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec transcriptRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, errNotTranscriptJSONL
		}
		// The "entry" marker is what distinguishes this format from a legacy
		// whole-document transcript that happens to fit on one line.
		if rec.Entry == nil {
			return nil, errNotTranscriptJSONL
		}

		if cur == nil || *rec.Entry != curEntry {
			flush()
			cur = &kiroHistoryEntry{}
			curEntry = *rec.Entry
		}
		if rec.User != nil {
			cur.User = *rec.User
		}
		if len(rec.Assistant) > 0 {
			cur.Assistant = rec.Assistant
		}
		if t.ConversationID == "" {
			t.ConversationID = rec.ConversationID
		}
		if t.CLIVersion == "" {
			t.CLIVersion = rec.CLIVersion
		}
		saw = true
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read kiro transcript: %w", err)
	}
	flush()

	if !saw {
		return nil, errNotTranscriptJSONL
	}
	return t, nil
}

// countTranscriptLines counts the JSONL records in a stored transcript. This is
// the unit get-transcript-position reports and the unit
// transcript.SliceFromLine consumes.
func countTranscriptLines(data []byte) int {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxTranscriptLine)
	n := 0
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) > 0 {
			n++
		}
	}
	return n
}

// sliceTranscriptLines drops the first startLine records of a stored
// transcript, mirroring transcript.SliceFromLine
// (cmd/entire/cli/transcript/parse.go:130) so kiro's extractors scope to the
// same content Entire checkpoints.
func sliceTranscriptLines(data []byte, startLine int) []byte {
	if startLine <= 0 {
		return data
	}
	count, offset := 0, 0
	for i, b := range data {
		if b == '\n' {
			count++
			if count == startLine {
				offset = i + 1
				break
			}
		}
	}
	if count < startLine || offset >= len(data) {
		return nil
	}
	return data[offset:]
}

// atomicWriteFile writes data to path via a temporary file in the same
// directory and renames it into place, so a concurrent reader never observes a
// half-written transcript.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".kiro-transcript-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
