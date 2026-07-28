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

// The on-disk transcript is stored as JSONL: exactly one SessionMessage per
// line, in order, with no header line. Entire scopes external-agent
// transcripts by LINE offset (transcript.SliceFromLine) before handing them to
// compact-transcript, so a single-JSON-blob layout would be destroyed by that
// slicing. One-message-per-line keeps line index == message index, which is
// also the unit Entire records via get-transcript-position, so scoping lands on
// message boundaries and header-less slices remain valid JSONL.

// encodeMessagesJSONL serializes messages as one JSON object per line.
func encodeMessagesJSONL(messages []SessionMessage) ([]byte, error) {
	var buf bytes.Buffer
	for i := range messages {
		encoded, err := json.Marshal(messages[i])
		if err != nil {
			return nil, fmt.Errorf("marshal message: %w", err)
		}
		buf.Write(encoded)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// decodeTranscript reads a transcript into its messages. It accepts both the
// JSONL layout written by this adapter and a raw Kilo session blob (a single
// JSON object with a "messages" array) so ingestion output and legacy files
// still parse. Missing/unparseable lines are skipped, so header-less scoped
// slices decode cleanly.
func decodeTranscript(data []byte) ([]SessionMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}

	// A raw Kilo session blob is a single JSON object with a "messages" array
	// (compact or pretty-printed). A message line has "info"/"parts" and no
	// "messages", so it falls through to the JSONL scanner below.
	var blob struct {
		Messages *[]SessionMessage `json:"messages"`
		Info     *json.RawMessage  `json:"info"`
	}
	if json.Unmarshal(trimmed, &blob) == nil && blob.Messages != nil && blob.Info == nil {
		return *blob.Messages, nil
	}

	// JSONL: one message per line. Blank/unparseable lines are skipped so
	// header-less scoped slices decode cleanly.
	messages := make([]SessionMessage, 0)
	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
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
