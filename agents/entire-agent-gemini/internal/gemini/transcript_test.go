package gemini

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeTestSidecar(t *testing.T, a *Agent, tmpDir, sessionID string, records []sidecarRecord) string {
	t.Helper()
	dir, _ := a.GetSessionDir(tmpDir)
	os.MkdirAll(dir, 0o755)
	path := a.ResolveSessionFile(dir, sessionID)
	for _, rec := range records {
		data, _ := json.Marshal(rec)
		data = append(data, '\n')
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatalf("write sidecar: %v", err)
		}
		f.Write(data)
		f.Close()
	}
	return path
}

func TestReadTranscript(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	records := []sidecarRecord{
		{V: 1, Agent: AgentName, Event: "SessionStart", SessionID: "t-001", TS: "2026-08-01T12:00:00Z"},
		{V: 1, Agent: AgentName, Event: "BeforeAgent", SessionID: "t-001", TS: "2026-08-01T12:01:00Z", Prompt: "hello"},
		{V: 1, Agent: AgentName, Event: "AfterAgent", SessionID: "t-001", TS: "2026-08-01T12:02:00Z", LastAssistantMessage: "hi there"},
	}
	path := writeTestSidecar(t, a, tmp, "t-001", records)

	data, err := a.ReadTranscript(path)
	if err != nil {
		t.Fatalf("ReadTranscript error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ReadTranscript returned empty data")
	}

	// Verify we can parse the JSONL
	lines := splitLines(data)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestReadTranscriptEmptyRef(t *testing.T) {
	a := New()
	_, err := a.ReadTranscript("")
	if err == nil {
		t.Error("ReadTranscript(\"\") should return error")
	}
}

func TestChunkTranscript(t *testing.T) {
	a := New()
	data := []byte("line1\nline2\nline3\nline4\nline5")

	chunks, err := a.ChunkTranscript(data, 12)
	if err != nil {
		t.Fatalf("ChunkTranscript error: %v", err)
	}
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestChunkTranscriptNoSplit(t *testing.T) {
	a := New()
	data := []byte("small line")

	chunks, err := a.ChunkTranscript(data, 1000)
	if err != nil {
		t.Fatalf("ChunkTranscript error: %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if string(chunks[0]) != "small line" {
		t.Errorf("chunk content = %q, want 'small line'", string(chunks[0]))
	}
}

func TestReassembleTranscript(t *testing.T) {
	a := New()
	chunks := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	result, err := a.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatalf("ReassembleTranscript error: %v", err)
	}
	if string(result) != "a\nb\nc" {
		t.Errorf("ReassembleTranscript = %q, want 'a\\nb\\nc'", string(result))
	}
}

func TestExtractPrompts(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	records := []sidecarRecord{
		{V: 1, Agent: AgentName, Event: "SessionStart", SessionID: "p-001", TS: "2026-08-01T12:00:00Z"},
		{V: 1, Agent: AgentName, Event: "BeforeAgent", SessionID: "p-001", TS: "2026-08-01T12:01:00Z", Prompt: "first prompt"},
		{V: 1, Agent: AgentName, Event: "AfterAgent", SessionID: "p-001", TS: "2026-08-01T12:02:00Z"},
		{V: 1, Agent: AgentName, Event: "BeforeAgent", SessionID: "p-001", TS: "2026-08-01T12:03:00Z", Prompt: "second prompt"},
	}
	path := writeTestSidecar(t, a, tmp, "p-001", records)

	prompts, err := a.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts error: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(prompts))
	}
	if prompts[0] != "first prompt" {
		t.Errorf("prompts[0] = %q, want 'first prompt'", prompts[0])
	}
	if prompts[1] != "second prompt" {
		t.Errorf("prompts[1] = %q, want 'second prompt'", prompts[1])
	}
}

func TestExtractSummary(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	records := []sidecarRecord{
		{V: 1, Agent: AgentName, Event: "SessionStart", SessionID: "s-001", TS: "2026-08-01T12:00:00Z"},
		{V: 1, Agent: AgentName, Event: "BeforeAgent", SessionID: "s-001", TS: "2026-08-01T12:01:00Z", Prompt: "do something"},
		{V: 1, Agent: AgentName, Event: "PreCompress", SessionID: "s-001", TS: "2026-08-01T12:02:00Z", CompactSummary: "Summary of conversation"},
	}
	path := writeTestSidecar(t, a, tmp, "s-001", records)

	summary, found, err := a.ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary error: %v", err)
	}
	if !found {
		t.Fatal("expected summary to be found")
	}
	if summary != "Summary of conversation" {
		t.Errorf("summary = %q, want 'Summary of conversation'", summary)
	}
}

func TestExtractSummaryNotFound(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	records := []sidecarRecord{
		{V: 1, Agent: AgentName, Event: "SessionStart", SessionID: "s-002", TS: "2026-08-01T12:00:00Z"},
	}
	path := writeTestSidecar(t, a, tmp, "s-002", records)

	summary, found, err := a.ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary error: %v", err)
	}
	if found {
		t.Error("should not find summary when none exists")
	}
	if summary != "" {
		t.Errorf("summary = %q, want empty", summary)
	}
}

func TestExtractModifiedFiles(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	toolInput, _ := json.Marshal(map[string]string{"file_path": "/src/main.go"})
	records := []sidecarRecord{
		{V: 1, Agent: AgentName, Event: "BeforeTool", SessionID: "m-001", TS: "2026-08-01T12:01:00Z", ToolName: "write_file", ToolInput: toolInput},
		{V: 1, Agent: AgentName, Event: "AfterTool", SessionID: "m-001", TS: "2026-08-01T12:01:05Z", ToolName: "write_file", ToolInput: toolInput},
		{V: 1, Agent: AgentName, Event: "BeforeTool", SessionID: "m-001", TS: "2026-08-01T12:02:00Z", ToolName: "read_file", ToolInput: toolInput},
	}
	path := writeTestSidecar(t, a, tmp, "m-001", records)

	files, _, err := a.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 modified file, got %d", len(files))
	}
	if files[0] != "/src/main.go" {
		t.Errorf("files[0] = %q, want /src/main.go", files[0])
	}
}

func TestCompactTranscript(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	records := []sidecarRecord{
		{V: 1, Agent: AgentName, Event: "SessionStart", SessionID: "c-001", TS: "2026-08-01T12:00:00Z", Model: "gemini-2.5-pro"},
		{V: 1, Agent: AgentName, Event: "BeforeAgent", SessionID: "c-001", TS: "2026-08-01T12:01:00Z", Prompt: "test prompt"},
		{V: 1, Agent: AgentName, Event: "AfterAgent", SessionID: "c-001", TS: "2026-08-01T12:02:00Z", LastAssistantMessage: "response"},
	}
	path := writeTestSidecar(t, a, tmp, "c-001", records)

	result, err := a.CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript error: %v", err)
	}
	if result.Transcript == "" {
		t.Fatal("CompactTranscript returned empty transcript")
	}

	// Verify it's valid JSON
	var entries []map[string]interface{}
	if err := json.Unmarshal([]byte(result.Transcript), &entries); err != nil {
		t.Fatalf("compact transcript is not valid JSON: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestGetTranscriptPosition(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	records := []sidecarRecord{
		{V: 1, Agent: AgentName, Event: "SessionStart", SessionID: "pos-001", TS: "2026-08-01T12:00:00Z"},
	}
	path := writeTestSidecar(t, a, tmp, "pos-001", records)

	pos, err := a.GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition error: %v", err)
	}
	if pos <= 0 {
		t.Errorf("position = %d, should be > 0", pos)
	}
}

func TestExtractFilePathFromInput(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"file_path", `{"file_path":"/src/main.go"}`, "/src/main.go"},
		{"filePath", `{"filePath":"/src/utils.ts"}`, "/src/utils.ts"},
		{"path", `{"path":"README.md"}`, "README.md"},
		{"empty", `{}`, ""},
		{"invalid_json", `not json`, ""},
	}
	for _, tc := range tests {
		got := extractFilePathFromInput(json.RawMessage(tc.json))
		if got != tc.want {
			t.Errorf("extractFilePathFromInput(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLooksLikeFilePath(t *testing.T) {
	if !looksLikeFilePath("/src/main.go") {
		t.Error("/src/main.go should look like a file path")
	}
	if !looksLikeFilePath("main.go") {
		t.Error("main.go should look like a file path")
	}
	if looksLikeFilePath("") {
		t.Error("empty string should not look like a file path")
	}
	if looksLikeFilePath("just text without path") {
		t.Error("plain text should not look like a file path")
	}
}

func TestWriteAndReadSession(t *testing.T) {
	a := New()
	tmp := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", tmp)

	// Write sidecar first
	records := []sidecarRecord{
		{V: 1, Agent: AgentName, Event: "SessionStart", SessionID: "wr-001", TS: "2026-08-01T12:00:00Z"},
	}
	sidecarPath := writeTestSidecar(t, a, tmp, "wr-001", records)

	// Read it back
	data, err := a.ReadTranscript(sidecarPath)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("no data read")
	}

	// Clean up
	_ = filepath.Clean(sidecarPath)
}
