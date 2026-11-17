package vibe

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// The stored transcript is JSONL: one Vibe history entry per line, in order,
// with no header. Entire scopes external-agent transcripts by line offset
// before handing them to compact-transcript, so line index must equal entry
// index, and a slice that starts mid-file must still decode.

// encodeLines joins already-encoded entries, one per line.
func encodeLines(lines []json.RawMessage) []byte {
	var buf bytes.Buffer
	for _, line := range lines {
		buf.Write(bytes.TrimSpace(line))
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// decodeTranscript reads the stored transcript. Blank and unparseable lines are
// skipped so that a scoped slice decodes cleanly.
//
// Lines have no length limit. The whole transcript is already in memory, and a
// single large entry (a big file read or tool output) must not make every
// reader of the session fail.
func decodeTranscript(data []byte) ([]Entry, error) {
	entries := make([]Entry, 0)
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var entry Entry
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		if entry.ID == "" && entry.Type == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// atomicWriteFile writes data to path via a temp file and rename, so a crash
// mid-write cannot leave a partial transcript behind.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vibe-write-*")
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
