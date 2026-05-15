package windsurf

import (
	"os"
	"path/filepath"
	"testing"
)

const testTranscript = `{"v":1,"type":"prompt","content":"Create hello.go","ts":"2026-01-01T00:00:00Z"}
{"v":1,"type":"file","path":"hello.go","ts":"2026-01-01T00:00:01Z"}
{"v":1,"type":"response","content":"Done! Created hello.go.","ts":"2026-01-01T00:00:02Z"}
{"v":1,"type":"prompt","content":"Add a test file","ts":"2026-01-01T00:01:00Z"}
{"v":1,"type":"file","path":"hello_test.go","ts":"2026-01-01T00:01:01Z"}
{"v":1,"type":"file","path":"hello.go","ts":"2026-01-01T00:01:02Z"}
{"v":1,"type":"response","content":"Done! Added hello_test.go.","ts":"2026-01-01T00:01:03Z"}
`

func writeTranscriptFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestGetTranscriptPosition(t *testing.T) {
	path := writeTranscriptFixture(t, testTranscript)

	pos, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition() error = %v", err)
	}
	if pos != 7 {
		t.Fatalf("position = %d, want 7", pos)
	}
}

func TestGetTranscriptPositionMissingFile(t *testing.T) {
	pos, err := New().GetTranscriptPosition(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("GetTranscriptPosition() error = %v (should return 0, nil for missing file)", err)
	}
	if pos != 0 {
		t.Fatalf("position = %d, want 0", pos)
	}
}

func TestExtractModifiedFilesFromOffset(t *testing.T) {
	path := writeTranscriptFixture(t, testTranscript)

	files, total, err := New().ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles() error = %v", err)
	}
	if total != 7 {
		t.Fatalf("total = %d, want 7", total)
	}
	// hello.go appears twice — should be deduplicated.
	wantFiles := []string{"hello.go", "hello_test.go"}
	if len(files) != len(wantFiles) {
		t.Fatalf("files = %v, want %v", files, wantFiles)
	}
	for i, want := range wantFiles {
		if files[i] != want {
			t.Fatalf("files[%d] = %q, want %q", i, files[i], want)
		}
	}
}

func TestExtractModifiedFilesWithOffset(t *testing.T) {
	path := writeTranscriptFixture(t, testTranscript)

	// Skip first 3 records (prompt, file, response of turn 1).
	files, total, err := New().ExtractModifiedFiles(path, 3)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles(offset=3) error = %v", err)
	}
	if total != 7 {
		t.Fatalf("total = %d, want 7", total)
	}
	wantFiles := []string{"hello_test.go", "hello.go"}
	if len(files) != len(wantFiles) {
		t.Fatalf("files = %v, want %v", files, wantFiles)
	}
}

func TestExtractModifiedFilesMissingFile(t *testing.T) {
	files, total, err := New().ExtractModifiedFiles(filepath.Join(t.TempDir(), "missing.json"), 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles() error = %v", err)
	}
	if files != nil || total != 0 {
		t.Fatalf("expected nil files and 0 total for missing file, got %v, %d", files, total)
	}
}

func TestExtractPromptsFromOffset(t *testing.T) {
	path := writeTranscriptFixture(t, testTranscript)

	prompts, err := New().ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts() error = %v", err)
	}
	wantPrompts := []string{"Create hello.go", "Add a test file"}
	if len(prompts) != len(wantPrompts) {
		t.Fatalf("prompts = %v, want %v", prompts, wantPrompts)
	}
	for i, want := range wantPrompts {
		if prompts[i] != want {
			t.Fatalf("prompts[%d] = %q, want %q", i, prompts[i], want)
		}
	}
}

func TestExtractPromptsWithOffset(t *testing.T) {
	path := writeTranscriptFixture(t, testTranscript)

	// Offset past turn 1 (records 0-2).
	prompts, err := New().ExtractPrompts(path, 3)
	if err != nil {
		t.Fatalf("ExtractPrompts(offset=3) error = %v", err)
	}
	if len(prompts) != 1 || prompts[0] != "Add a test file" {
		t.Fatalf("prompts = %v, want [Add a test file]", prompts)
	}
}

func TestExtractSummaryReturnsLastResponse(t *testing.T) {
	path := writeTranscriptFixture(t, testTranscript)

	summary, hasSummary, err := New().ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary() error = %v", err)
	}
	if !hasSummary {
		t.Fatal("hasSummary = false, want true")
	}
	want := "Done! Added hello_test.go."
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
}

func TestExtractSummaryMissingFile(t *testing.T) {
	summary, hasSummary, err := New().ExtractSummary(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("ExtractSummary() error = %v", err)
	}
	if hasSummary || summary != "" {
		t.Fatalf("expected empty summary for missing file, got %q, %v", summary, hasSummary)
	}
}

func TestExtractSummaryNoResponse(t *testing.T) {
	transcript := `{"v":1,"type":"prompt","content":"hello"}` + "\n"
	path := writeTranscriptFixture(t, transcript)

	summary, hasSummary, err := New().ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary() error = %v", err)
	}
	if hasSummary {
		t.Fatalf("expected no summary when no response records, got %q", summary)
	}
}

func TestChunkAndReassembleTranscript(t *testing.T) {
	content := []byte(testTranscript)
	chunks, err := New().ChunkTranscript(content, 50)
	if err != nil {
		t.Fatalf("ChunkTranscript() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	reassembled, err := New().ReassembleTranscript(chunks)
	if err != nil {
		t.Fatalf("ReassembleTranscript() error = %v", err)
	}
	if string(reassembled) != string(content) {
		t.Fatalf("reassembled != original")
	}
}

func TestParseTranscriptLargeRecord(t *testing.T) {
	// Build a response content that exceeds the default 64 KiB scanner buffer.
	large := make([]byte, 128*1024)
	for i := range large {
		large[i] = 'x'
	}
	rec := transcriptRecord{V: 1, Type: transcriptTypeResponse, Content: string(large)}
	path := writeTranscriptFixture(t, "")

	if err := appendTranscriptRecord(path, rec); err != nil {
		t.Fatalf("appendTranscriptRecord() error = %v", err)
	}

	records, err := readTranscriptRecords(path)
	if err != nil {
		t.Fatalf("readTranscriptRecords() error on large record: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if len(records[0].Content) != len(large) {
		t.Fatalf("content length = %d, want %d", len(records[0].Content), len(large))
	}
}

func TestParseTranscriptSkipsInvalidLines(t *testing.T) {
	transcript := `{"v":1,"type":"prompt","content":"ok"}
not-valid-json
{"v":1,"type":"response","content":"done"}
`
	path := writeTranscriptFixture(t, transcript)

	records, err := readTranscriptRecords(path)
	if err != nil {
		t.Fatalf("readTranscriptRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 valid records, got %d", len(records))
	}
}
