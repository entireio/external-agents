package zcode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-zcode/internal/protocol"
)

func writeTranscript(t *testing.T, dir string, messages []ExportMessage) string {
	t.Helper()
	encoded, err := encodeMessagesJSONL(messages)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess_1.jsonl")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sampleMessages() []ExportMessage {
	return []ExportMessage{
		{ID: "m1", Role: "user", Kind: "user_prompt", Time: 1000, Text: "first prompt"},
		{ID: "m2", Role: "assistant", Time: 2000, Model: "GLM-5.3", Text: "working",
			Tokens: &ExportTokens{Input: 10, Output: 5, CacheRead: 1, CacheWrite: 2},
			Tools:  []ExportTool{{Tool: "Write", Status: "completed", Input: json.RawMessage(`{"file_path":"/repo/a.go"}`)}}},
		{ID: "m3", Role: "user", Kind: "user_prompt", Time: 3000, Text: "second prompt"},
		{ID: "m4", Role: "assistant", Time: 4000, Text: "final answer",
			Tokens: &ExportTokens{Input: 20, Output: 8},
			Tools: []ExportTool{
				{Tool: "Edit", Status: "completed", Input: json.RawMessage(`{"file_path":"/repo/b.go"}`)},
				{Tool: "Edit", Status: "completed", Input: json.RawMessage(`{"file_path":"/repo/a.go"}`)}, // duplicate
				{Tool: "Read", Status: "completed", Input: json.RawMessage(`{"file_path":"/repo/c.go"}`)}, // not mutating
			}},
	}
}

func TestGetTranscriptPosition(t *testing.T) {
	path := writeTranscript(t, t.TempDir(), sampleMessages())
	pos, err := (&Agent{}).GetTranscriptPosition(path)
	if err != nil || pos != 4 {
		t.Fatalf("position = %d, %v; want 4", pos, err)
	}
	pos, err = (&Agent{}).GetTranscriptPosition(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || pos != 0 {
		t.Fatalf("missing file: position = %d, %v; want 0", pos, err)
	}
}

func TestExtractModifiedFiles(t *testing.T) {
	path := writeTranscript(t, t.TempDir(), sampleMessages())
	files, current, err := (&Agent{}).ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/repo/a.go", "/repo/b.go"} // sorted, deduped, read excluded
	if len(files) != 2 || files[0] != want[0] || files[1] != want[1] {
		t.Fatalf("files = %v, want %v", files, want)
	}
	if current != 4 {
		t.Fatalf("current position = %d, want 4", current)
	}

	// Offset scopes to messages after the first write.
	files, _, err = (&Agent{}).ExtractModifiedFiles(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 { // m4 edits both a.go and b.go
		t.Fatalf("offset files = %v", files)
	}
}

func TestExtractPromptsRespectsOffsetAndKind(t *testing.T) {
	msgs := sampleMessages()
	msgs = append(msgs, ExportMessage{ID: "m5", Role: "user", Kind: "system_reminder", Time: 5000, Text: "hidden"})
	path := writeTranscript(t, t.TempDir(), msgs)

	prompts, err := (&Agent{}).ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 || prompts[0] != "first prompt" || prompts[1] != "second prompt" {
		t.Fatalf("prompts = %v", prompts)
	}
	prompts, err = (&Agent{}).ExtractPrompts(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "second prompt" {
		t.Fatalf("offset prompts = %v", prompts)
	}
}

func TestExtractSummary(t *testing.T) {
	path := writeTranscript(t, t.TempDir(), sampleMessages())
	summary, ok, err := (&Agent{}).ExtractSummary(path)
	if err != nil || !ok || summary != "final answer" {
		t.Fatalf("summary = %q ok=%v err=%v", summary, ok, err)
	}
}

func TestCalculateTokens(t *testing.T) {
	data, err := encodeMessagesJSONL(sampleMessages())
	if err != nil {
		t.Fatal(err)
	}
	usage, err := (&Agent{}).CalculateTokens(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 30 || usage.OutputTokens != 13 ||
		usage.CacheReadTokens != 1 || usage.CacheCreationTokens != 2 || usage.APICallCount != 2 {
		t.Fatalf("usage = %+v", usage)
	}

	// Offset skips m2's tokens.
	usage, err = (&Agent{}).CalculateTokens(data, 2)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 20 || usage.APICallCount != 1 {
		t.Fatalf("offset usage = %+v", usage)
	}
}

func TestChunkReassembleRoundTrip(t *testing.T) {
	agent := &Agent{}
	content := []byte("abcdefghijklmnopqrstuvwxyz")
	chunks, err := agent.ChunkTranscript(content, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(chunks))
	}
	back, err := agent.ReassembleTranscript(chunks)
	if err != nil || string(back) != string(content) {
		t.Fatalf("round trip mismatch: %q %v", back, err)
	}
	if _, err := agent.ChunkTranscript(content, 0); err == nil {
		t.Fatal("max-size 0 must be rejected")
	}
}

func TestReadSessionFallsBackToTranscriptFile(t *testing.T) {
	t.Setenv("ZCODE_HOME", t.TempDir()) // no db → file fallback
	path := writeTranscript(t, t.TempDir(), sampleMessages())

	session, err := (&Agent{DBQuerier: &SQLiteQuerier{}}).ReadSession(&protocol.HookInputJSON{
		HookType:   "session_start",
		SessionID:  "sess_1",
		SessionRef: path,
	})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.AgentName != "zcode" || session.SessionID != "sess_1" {
		t.Fatalf("session = %+v", session)
	}
	if session.SessionRef == "" || len(session.NativeData) == 0 {
		t.Fatalf("session ref/native data: %+v", session)
	}
	if len(session.ModifiedFiles) != 2 {
		t.Fatalf("modified files = %v", session.ModifiedFiles)
	}
	if _, err := (&Agent{}).ReadSession(&protocol.HookInputJSON{}); err == nil {
		t.Fatal("missing session_ref/session_id must error")
	}
}

func TestReadSessionEchoesStoredSnapshot(t *testing.T) {
	t.Setenv("ZCODE_HOME", t.TempDir()) // no db interference
	dir := t.TempDir()
	ref := filepath.Join(dir, "sess_snap.json")
	stored := protocol.AgentSessionJSON{
		SessionID:     "sess_snap",
		AgentName:     "zcode",
		RepoPath:      "/repo",
		SessionRef:    ref,
		StartTime:     "2026-09-01T00:00:00Z",
		NativeData:    []byte(`{"test": true}`),
		ModifiedFiles: []string{"file1.go"},
		NewFiles:      []string{"file3.go"},
		DeletedFiles:  []string{},
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	readBack, err := (&Agent{}).ReadSession(&protocol.HookInputJSON{
		HookType:   "session_start",
		SessionID:  "sess_snap",
		SessionRef: ref,
	})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if string(readBack.NativeData) != `{"test": true}` || readBack.SessionRef != ref {
		t.Fatalf("snapshot not echoed: %+v", readBack)
	}
}

func TestPrepareTranscriptNeverClobbersWithEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_HOME", t.TempDir()) // no db
	ref := filepath.Join(dir, "sess_keep.jsonl")
	existing, err := encodeMessagesJSONL(sampleMessages())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref, existing, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&Agent{DBQuerier: &SQLiteQuerier{}}).PrepareTranscript(ref); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(ref)
	if len(data) != len(existing) {
		t.Fatal("empty export must not clobber the existing transcript")
	}
}

func TestPrepareTranscriptExportsFromStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZCODE_HOME", home)
	db := filepath.Join(home, "cli", "db", "db.sqlite")
	if err := os.MkdirAll(filepath.Dir(db), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := map[string]any{"role": "assistant", "time": map[string]any{"created": 1}, "modelID": "GLM-5.3"}
	querier := &fakeQuerier{responses: map[string]string{
		"from message m left join part p": fmt.Sprintf(`[{"message_id":"m1","mseq":0,"mdata":%s,"pseq":0,"pdata":null}]`, jsonColumn(msg)),
		"from session where id":           `[{"id":"sess_1","parent_id":"","title":"t","directory":"","time_created":1,"time_updated":1}]`,
	}}
	agent := &Agent{DBQuerier: querier}
	ref := filepath.Join(t.TempDir(), "sess_1.jsonl")
	if err := agent.PrepareTranscript(ref); err != nil {
		t.Fatalf("PrepareTranscript: %v", err)
	}
	messages, err := readMessages(ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Model != "GLM-5.3" {
		t.Fatalf("exported = %+v", messages)
	}
}

func TestWriteAndReadTranscriptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ref := filepath.Join(dir, "sess_w.jsonl")
	agent := &Agent{}
	messages := sampleMessages()
	encoded, err := encodeMessagesJSONL(messages)
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionRef: ref,
		NativeData: encoded,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := agent.ReadTranscript(ref)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	decoded, err := decodeTranscript(data)
	if err != nil || len(decoded) != len(messages) {
		t.Fatalf("round trip: %d messages, err %v", len(decoded), err)
	}
}
