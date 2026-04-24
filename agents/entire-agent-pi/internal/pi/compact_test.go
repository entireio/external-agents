package pi

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestCompactTranscriptFromJSONLTranscript(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "9.9.9")

	path := writeCompactTranscriptFixture(t, testSessionJSONL)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}

	want := strings.Join([]string{
		`{"v":1,"agent":"pi","cli_version":"9.9.9","type":"user","ts":"2026-03-27T21:00:01.000Z","content":[{"text":"Create hello.txt"}]}`,
		`{"v":1,"agent":"pi","cli_version":"9.9.9","type":"assistant","ts":"2026-03-27T21:00:02.000Z","id":"m2","input_tokens":100,"output_tokens":50,"content":[{"type":"tool_use","id":"tc1","name":"Write","input":{"content":"hello world\n","path":"hello.txt"},"result":{"output":"Written 12 bytes","status":"success"}}]}`,
		`{"v":1,"agent":"pi","cli_version":"9.9.9","type":"assistant","ts":"2026-03-27T21:00:04.000Z","id":"m4","input_tokens":200,"output_tokens":30,"content":[{"type":"text","text":"Created hello.txt with the content hello world."}]}`,
		"",
	}, "\n")

	if string(data) != want {
		t.Fatalf("compact transcript = %q, want %q", string(data), want)
	}
}

func TestCompactTranscriptUsesUnknownCLIVersionFallback(t *testing.T) {
	path := writeCompactTranscriptFixture(t, testFlatSessionJSONL)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	if !strings.Contains(string(data), `"cli_version":"unknown"`) {
		t.Fatalf("compact transcript should include unknown cli_version fallback, got %q", string(data))
	}
}

func TestCompactTranscriptFiltersAbandonedBranches(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "1.2.3")

	path := writeCompactTranscriptFixture(t, testBranchingSessionJSONL)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "old.txt") || strings.Contains(got, "Created old.txt") {
		t.Fatalf("compact transcript should exclude abandoned branch, got %q", got)
	}
	if !strings.Contains(got, "new.txt") || !strings.Contains(got, "Created new.txt") {
		t.Fatalf("compact transcript should include active branch, got %q", got)
	}
}

func TestCompactTranscriptMarksToolResultErrors(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "1.2.3")

	jsonl := `{"type":"session","id":"s1"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-27T21:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"Edit app.go"}]}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-27T21:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"edit","arguments":{"path":"app.go"}}]}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-03-27T21:00:03.000Z","message":{"role":"toolResult","toolCallId":"tc1","toolName":"edit","content":[{"type":"text","text":"file not found"}],"isError":true}}
`
	path := writeCompactTranscriptFixture(t, jsonl)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"name":"Edit"`) || !strings.Contains(got, `"status":"error"`) {
		t.Fatalf("compact transcript should normalize edit error result, got %q", got)
	}
}

func TestCompactTranscriptRejectsEmptyTranscript(t *testing.T) {
	path := writeCompactTranscriptFixture(t, `{}`)

	_, err := New().CompactTranscript(path)
	if err == nil {
		t.Fatal("expected error for empty transcript, got nil")
	}
	if !strings.Contains(err.Error(), "no output") {
		t.Fatalf("error = %v, want no output", err)
	}
}

func writeCompactTranscriptFixture(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/transcript.jsonl"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write transcript fixture: %v", err)
	}
	return path
}
