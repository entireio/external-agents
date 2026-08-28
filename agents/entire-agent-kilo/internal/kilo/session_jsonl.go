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
	enc := json.NewEncoder(&buf) // Encode writes directly into buf and appends '\n'.
	for i := range messages {
		if err := enc.Encode(&messages[i]); err != nil {
			return nil, fmt.Errorf("encode message: %w", err)
		}
	}
	return buf.Bytes(), nil
}

// decodeTranscript reads the on-disk transcript, which is always one JSON
// message per line (see encodeMessagesJSONL). Blank/unparseable lines are
// skipped, so header-less scoped slices decode cleanly. Kilo's native session
// export blob is handled separately by parseKiloExport at the ingestion
// boundary — it never reaches disk.
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
