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

// The on-disk transcript is stored as JSONL: exactly one Kilo message per
// line, in order, with no header line. Entire scopes external-agent
// transcripts by LINE offset (transcript.SliceFromLine) before handing them to
// compact-transcript, so a single-JSON-blob layout would be destroyed by that
// slicing. One-message-per-line keeps line index == message index, which is
// also the unit Entire records via get-transcript-position, so scoping lands on
// message boundaries and header-less slices remain valid JSONL.
//
// Each line is Kilo's own message object as `kilo export` printed it, not a
// re-encoding of SessionMessage: SessionMessage models only what this adapter
// reads, and `kilo import` rejects messages missing the fields it drops
// (parentID, path, cost, agent and others). The first line also carries the
// export's session envelope under sessionEnvelopeKey, which import needs to
// recreate the session. It rides on a message line instead of its own line so
// line index stays message index; decodeTranscript ignores it.
const sessionEnvelopeKey = "kilo_session"

// kiloExport is Kilo's native session export (`kilo export <id>`): the session
// envelope ("info") and every message exactly as Kilo wrote it.
type kiloExport struct {
	Info     json.RawMessage   `json:"info,omitempty"`
	Messages []json.RawMessage `json:"messages"`
}

// encodeExportJSONL stores an export as one message object per line, with the
// session envelope on the first line. Entries that are not JSON objects cannot
// be messages and are dropped.
func encodeExportJSONL(export kiloExport) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf) // Encode compacts each value onto one line and appends '\n'.
	enc.SetEscapeHTML(false)
	envelope := bytes.TrimSpace(export.Info)
	first := true
	for _, raw := range export.Messages {
		if !isJSONObject(raw) {
			continue
		}
		if first && len(envelope) > 0 && !bytes.Equal(envelope, []byte("null")) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				return nil, fmt.Errorf("decode message: %w", err)
			}
			fields[sessionEnvelopeKey] = envelope
			if err := enc.Encode(fields); err != nil {
				return nil, fmt.Errorf("encode message: %w", err)
			}
			first = false
			continue
		}
		if err := enc.Encode(raw); err != nil {
			return nil, fmt.Errorf("encode message: %w", err)
		}
		first = false
	}
	return buf.Bytes(), nil
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// decodeTranscript reads the on-disk transcript, which is always one JSON
// message per line (see encodeExportJSONL). Blank/unparseable lines are
// skipped, so header-less scoped slices decode cleanly, and the envelope key on
// the first line is ignored. Kilo's native session export blob is handled
// separately by parseKiloExport at the ingestion boundary — it never reaches
// disk.
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

// parseKiloExport reads Kilo's native session export (a single JSON object
// with "info" and a "messages" array). Used only when ingesting a fresh export
// from the plugin payload or the Kilo CLI; the result is stored as JSONL via
// encodeExportJSONL.
func parseKiloExport(raw []byte) (kiloExport, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return kiloExport{}, nil
	}
	var export kiloExport
	if err := json.Unmarshal(trimmed, &export); err != nil {
		return kiloExport{}, fmt.Errorf("parse kilo session export: %w", err)
	}
	return export, nil
}

// exportFromTranscript rebuilds the `kilo import` input from a stored
// transcript: the envelope from the first line that carries one, and every
// line decodeTranscript would count as a message, minus the envelope key. ok
// is false when no line carries an envelope, which is every transcript written
// before the envelope was stored; those also lack the message fields import
// needs, so there is nothing to import.
func exportFromTranscript(data []byte) (kiloExport, bool, error) {
	var export kiloExport
	for rawLine := range bytes.SplitSeq(data, []byte{'\n'}) {
		line := bytes.TrimSpace(rawLine)
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
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			continue
		}
		if envelope, found := fields[sessionEnvelopeKey]; found {
			if len(export.Info) == 0 {
				export.Info = envelope
			}
			delete(fields, sessionEnvelopeKey)
			stripped, err := marshalNoHTMLEscape(fields)
			if err != nil {
				return kiloExport{}, false, err
			}
			line = stripped
		}
		export.Messages = append(export.Messages, json.RawMessage(bytes.Clone(line)))
	}
	if len(export.Info) == 0 || len(export.Messages) == 0 {
		return kiloExport{}, false, nil
	}
	return export, true, nil
}

// marshalNoHTMLEscape is json.Marshal without rewriting <, > and & as \u
// escapes, so Kilo gets its strings back byte for byte.
func marshalNoHTMLEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// atomicWriteFile writes data to path via a temp file + rename so a crash
// mid-write cannot leave a partially-written transcript behind.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error { //nolint:unparam // Keep permissions configurable for callers needing other file modes.
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
