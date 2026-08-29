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

// errTranscriptNotPrepared reports a session file that still holds raw hook
// payloads: `amp threads export` has not run for it yet.
var errTranscriptNotPrepared = errors.New("amp transcript is not prepared: run prepare-transcript first")

// encodeMessagesJSONL serializes messages as one JSON object per line.
func encodeMessagesJSONL(messages []ThreadMessage) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf) // Encode writes directly into buf and appends '\n'.
	for i := range messages {
		if err := enc.Encode(&messages[i]); err != nil {
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
// header-less scoped slices decode cleanly.
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
