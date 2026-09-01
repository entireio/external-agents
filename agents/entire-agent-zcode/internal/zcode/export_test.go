package zcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeQuerier serves canned `sqlite3 -json` output per query substring.
type fakeQuerier struct {
	responses map[string]string
	queries   []string
}

func (q *fakeQuerier) Query(_ context.Context, dbPath, query string) ([]byte, error) {
	q.queries = append(q.queries, query)
	for needle, response := range q.responses {
		if strings.Contains(query, needle) {
			return []byte(response), nil
		}
	}
	return nil, nil
}

// jsonColumn mimics `sqlite3 -json`: TEXT columns are rendered as JSON strings
// (double-encoded), which the export path must tolerate.
func jsonColumn(v any) string {
	raw, _ := json.Marshal(v)
	return fmt.Sprintf("%q", string(raw))
}

func TestExportSessionProjectsVisibleContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZCODE_HOME", home)
	db := filepath.Join(home, "cli", "db", "db.sqlite")
	if err := os.MkdirAll(filepath.Dir(db), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	userMsg := map[string]any{
		"role": "user",
		"time": map[string]any{"created": 1788277204342},
		"semantics": map[string]any{
			"kind": "user_prompt", "origin": "real_user", "transcriptVisibility": "visible",
		},
	}
	assistantMsg := map[string]any{
		"role":    "assistant",
		"time":    map[string]any{"created": 1788277209312},
		"modelID": "GLM-5.3",
		"tokens":  map[string]any{"input": 100, "output": 20, "cache": map[string]any{"read": 5, "write": 7}},
	}
	hiddenMsg := map[string]any{
		"role": "user",
		"semantics": map[string]any{
			"kind": "system_reminder", "transcriptVisibility": "hidden",
		},
	}
	writeToolPart := map[string]any{
		"type": "tool", "tool": "Write",
		"state": map[string]any{
			"status": "completed",
			"input":  map[string]any{"file_path": "/repo/main.go"},
			"output": "ok",
		},
	}
	_ = writeToolPart
	syntheticPart := map[string]any{"type": "text", "text": "<system-reminder>", "synthetic": true}
	_ = syntheticPart
	textPart := map[string]any{"type": "text", "text": "Created the file."}
	_ = textPart

	querier := &fakeQuerier{responses: map[string]string{
		"from message m left join part p": fmt.Sprintf(`[
 {"message_id":"m1","mseq":0,"mdata":%s,"pseq":0,"pdata":%s},
 {"message_id":"m1","mseq":0,"mdata":%s,"pseq":1,"pdata":%s},
 {"message_id":"m2","mseq":1,"mdata":%s,"pseq":0,"pdata":%s},
 {"message_id":"m2","mseq":1,"mdata":%s,"pseq":1,"pdata":%s},
 {"message_id":"m2","mseq":1,"mdata":%s,"pseq":2,"pdata":%s},
 {"message_id":"m3","mseq":2,"mdata":%s,"pseq":0,"pdata":null}
]`,
			jsonColumn(userMsg), jsonColumn(map[string]any{"type": "text", "text": "make a file"}),
			jsonColumn(userMsg), jsonColumn(map[string]any{"type": "text", "text": "line2"}),
			jsonColumn(assistantMsg), jsonColumn(syntheticPart),
			jsonColumn(assistantMsg), jsonColumn(textPart),
			jsonColumn(assistantMsg), jsonColumn(writeToolPart),
			jsonColumn(hiddenMsg)),
		"from session where id": `[{"id":"sess_1","parent_id":"","title":"t","directory":"","time_created":1788277204342,"time_updated":1}]`,
	}}
	agent := &Agent{DBQuerier: querier}

	messages, err := agent.exportSession(context.Background(), "sess_1")
	if err != nil {
		t.Fatalf("exportSession: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2 (hidden dropped): %+v", len(messages), messages)
	}

	first := messages[0]
	if first.Role != "user" || first.Kind != "user_prompt" {
		t.Fatalf("first message: %+v", first)
	}
	if first.Text != "make a file\nline2" {
		t.Fatalf("text parts not joined: %q", first.Text)
	}

	second := messages[1]
	if second.Model != "GLM-5.3" || second.Tokens == nil {
		t.Fatalf("assistant message: %+v", second)
	}
	if second.Text != "Created the file." {
		t.Fatalf("synthetic part leaked / text lost: %q", second.Text)
	}
	if second.Tokens.Input != 100 || second.Tokens.Output != 20 ||
		second.Tokens.CacheRead != 5 || second.Tokens.CacheWrite != 7 {
		t.Fatalf("tokens: %+v", second.Tokens)
	}
	if len(second.Tools) != 1 || second.Tools[0].Tool != "Write" {
		t.Fatalf("tools: %+v", second.Tools)
	}

	// Synthetic text and hidden messages must not appear in the encoded output.
	encoded, err := encodeMessagesJSONL(messages)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "system-reminder") {
		t.Fatalf("synthetic content leaked: %s", encoded)
	}
}

func TestExportSessionEmptyDBOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZCODE_HOME", home)
	agent := &Agent{DBQuerier: &fakeQuerier{}} // no responses → empty output
	messages, err := agent.exportSession(context.Background(), "sess_x")
	if err != nil {
		t.Fatalf("exportSession: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("want 0 messages, got %d", len(messages))
	}
}

func TestDecodeJSONColumnBothShapes(t *testing.T) {
	var direct struct{ A string }
	if !decodeJSONColumn(json.RawMessage(`{"A":"x"}`), &direct) || direct.A != "x" {
		t.Fatalf("direct object failed: %+v", direct)
	}
	var wrapped struct{ A string }
	if !decodeJSONColumn(json.RawMessage(`"{\"A\":\"y\"}"`), &wrapped) || wrapped.A != "y" {
		t.Fatalf("string-wrapped failed: %+v", wrapped)
	}
	if decodeJSONColumn(json.RawMessage(`null`), &wrapped) {
		t.Fatal("null should not decode")
	}
	if decodeJSONColumn(json.RawMessage(`"not json"`), &wrapped) {
		t.Fatal("invalid inner JSON should not decode")
	}
}

func TestEncodeDecodeTranscriptRoundTrip(t *testing.T) {
	messages := []ExportMessage{
		{ID: "m1", Role: "user", Kind: "user_prompt", Time: 1, Text: "hi"},
		{ID: "m2", Role: "assistant", Time: 2, Model: "GLM-5.3", Text: "hello",
			Tokens: &ExportTokens{Input: 1, Output: 2},
			Tools:  []ExportTool{{Tool: "Bash", Status: "completed", Input: json.RawMessage(`{"command":"ls"}`)}}},
	}
	encoded, err := encodeMessagesJSONL(messages)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(encoded), "\n") != 2 {
		t.Fatalf("want one line per message: %q", encoded)
	}
	decoded, err := decodeTranscript(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[1].Tools[0].Tool != "Bash" {
		t.Fatalf("round trip: %+v", decoded)
	}
}

func TestSQLLiteralEscaping(t *testing.T) {
	got := sqlLiteral("sess'a'; drop table session; --")
	want := "'sess''a''; drop table session; --'"
	if got != want {
		t.Fatalf("sqlLiteral = %s, want %s", got, want)
	}
}
