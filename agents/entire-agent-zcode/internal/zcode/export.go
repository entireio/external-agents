package zcode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const maxTranscriptLine = 10 * 1024 * 1024

// dbJoinedRow is one row of the message⨝part export query. Messages without
// parts produce one row with a NULL pdata.
type dbJoinedRow struct {
	MessageID string          `json:"message_id"`
	MSequence *int64          `json:"mseq"`
	MData     json.RawMessage `json:"mdata"`
	PSequence *int64          `json:"pseq"`
	PData     json.RawMessage `json:"pdata"`
}

// querySessionRow loads the session row for id, or (nil, nil) when absent.
func (a *Agent) querySessionRow(ctx context.Context, sessionID string) (*dbSessionRow, error) {
	querier := a.querier()
	if querier == nil {
		return nil, nil
	}
	query := "select id, coalesce(parent_id,'') as parent_id, title, coalesce(directory,'') as directory, time_created, time_updated from session where id = " + sqlLiteral(sessionID)
	out, err := querier.Query(ctx, dbPath(), query)
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var rows []dbSessionRow
	if err := json.Unmarshal(trimmed, &rows); err != nil {
		return nil, fmt.Errorf("parse session rows: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// exportSession reads a session's messages+parts from ZCode's SQLite store
// and projects them into the export format. Hidden/synthetic content is
// dropped at this boundary; the JSONL on disk only holds visible content.
func (a *Agent) exportSession(ctx context.Context, sessionID string) ([]ExportMessage, error) {
	querier := a.querier()
	if querier == nil {
		return nil, nil
	}
	query := "select m.id as message_id, m.sequence as mseq, m.data as mdata, p.sequence as pseq, p.data as pdata " +
		"from message m left join part p on p.message_id = m.id " +
		"where m.session_id = " + sqlLiteral(sessionID) + " " +
		"order by coalesce(m.sequence, 0), m.time_created, m.id, coalesce(p.sequence, 0), p.id"
	out, err := querier.Query(ctx, dbPath(), query)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	trimmed := bytes.TrimSpace(out)
	var rows []dbJoinedRow
	if len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &rows); err != nil {
			return nil, fmt.Errorf("parse message rows: %w", err)
		}
	}

	var messages []ExportMessage
	byID := map[string]int{}
	for _, row := range rows {
		var msgData dbMessageData
		if !decodeJSONColumn(row.MData, &msgData) {
			continue
		}
		if !isVisibleMessage(&msgData) {
			continue
		}
		idx, ok := byID[row.MessageID]
		if !ok {
			messages = append(messages, ExportMessage{
				ID:    row.MessageID,
				Role:  msgData.Role,
				Kind:  semanticsKind(&msgData),
				Time:  msgData.Time.Created,
				Model: messageModel(&msgData),
			})
			idx = len(messages) - 1
			byID[row.MessageID] = idx
		}
		applyTokens(&messages[idx], &msgData)
		var partData dbPartData
		if decodeJSONColumn(row.PData, &partData) {
			applyPart(&messages[idx], partData)
		}
	}
	return messages, nil
}

// decodeJSONColumn decodes a TEXT column from `sqlite3 -json` output. Such
// columns arrive as JSON strings containing the row text, so a struct payload
// is double-encoded; both the direct and string-wrapped forms are accepted.
func decodeJSONColumn(raw json.RawMessage, dst any) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return false
	}
	if err := json.Unmarshal(trimmed, dst); err == nil {
		return true
	}
	var wrapped string
	if err := json.Unmarshal(trimmed, &wrapped); err != nil {
		return false
	}
	inner := bytes.TrimSpace([]byte(wrapped))
	if len(inner) == 0 || string(inner) == "null" {
		return false
	}
	return json.Unmarshal(inner, dst) == nil
}

func (a *Agent) querier() DBQuerier {
	if a.DBQuerier == nil {
		return nil
	}
	return a.DBQuerier
}

func isVisibleMessage(msg *dbMessageData) bool {
	// ZCode tags hidden/synthetic messages (system reminders, internal
	// continuations); anything explicitly hidden stays out of the export.
	if msg.Semantics != nil && msg.Semantics.TranscriptVisibility == "hidden" {
		return false
	}
	return msg.Role == "user" || msg.Role == "assistant"
}

func semanticsKind(msg *dbMessageData) string {
	if msg.Semantics == nil {
		return ""
	}
	return msg.Semantics.Kind
}

func messageModel(msg *dbMessageData) string {
	if msg.ModelID != "" {
		return msg.ModelID
	}
	return msg.Model.ModelID
}

func applyTokens(msg *ExportMessage, data *dbMessageData) {
	if data.Tokens == nil {
		return
	}
	tokens := &ExportTokens{
		Input:  data.Tokens.Input,
		Output: data.Tokens.Output,
	}
	if data.Tokens.Cache != nil {
		tokens.CacheRead = data.Tokens.Cache.Read
		tokens.CacheWrite = data.Tokens.Cache.Write
	}
	msg.Tokens = tokens
}

func applyPart(msg *ExportMessage, part dbPartData) {
	switch part.Type {
	case "text":
		if part.Text != "" && !part.Synthetic {
			if msg.Text == "" {
				msg.Text = part.Text
			} else {
				msg.Text += "\n" + part.Text
			}
		}
	case "tool":
		if part.Tool == "" {
			return
		}
		tool := ExportTool{Tool: part.Tool}
		if part.State != nil {
			tool.Status = part.State.Status
			tool.Input = part.State.Input
			tool.Output = part.State.Output
		}
		msg.Tools = append(msg.Tools, tool)
	}
}

// encodeMessagesJSONL serializes messages as one JSON object per line so
// Entire's line-based scoping lands on message boundaries.
func encodeMessagesJSONL(messages []ExportMessage) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for i := range messages {
		if err := enc.Encode(&messages[i]); err != nil {
			return nil, fmt.Errorf("encode message: %w", err)
		}
	}
	return buf.Bytes(), nil
}

// decodeTranscript reads the on-disk JSONL export. Blank/unparseable lines
// are skipped so header-less scoped slices decode cleanly.
func decodeTranscript(data []byte) ([]ExportMessage, error) {
	messages := make([]ExportMessage, 0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxTranscriptLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg ExportMessage
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		if msg.ID == "" && msg.Role == "" {
			continue
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan zcode transcript: %w", err)
	}
	return messages, nil
}

// atomicWriteFile writes data to path via a temp file + rename so a crash
// mid-write cannot leave a partially-written transcript behind.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".zcode-write-*")
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
