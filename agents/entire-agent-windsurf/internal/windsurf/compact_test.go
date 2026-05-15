package windsurf

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCompactTranscriptEmitsUserAndAssistantLines(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "2.3.4")

	path := writeTranscriptFixture(t, testTranscript)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 compact lines (2 user + 2 assistant), got %d:\n%s", len(lines), string(data))
	}
	if !strings.Contains(lines[0], `"type":"user"`) || !strings.Contains(lines[0], "Create hello.go") {
		t.Fatalf("line[0] = %q, want user line with prompt", lines[0])
	}
	if !strings.Contains(lines[1], `"type":"assistant"`) || !strings.Contains(lines[1], "Created hello.go") {
		t.Fatalf("line[1] = %q, want assistant line with response", lines[1])
	}
	if !strings.Contains(lines[2], `"type":"user"`) || !strings.Contains(lines[2], "Add a test file") {
		t.Fatalf("line[2] = %q, want user line with second prompt", lines[2])
	}
	if !strings.Contains(lines[3], `"type":"assistant"`) || !strings.Contains(lines[3], "Added hello_test.go") {
		t.Fatalf("line[3] = %q, want assistant line with second response", lines[3])
	}
}

func TestCompactTranscriptIncludesCLIVersion(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "5.6.7")

	path := writeTranscriptFixture(t, `{"v":1,"type":"prompt","content":"hi"}
{"v":1,"type":"response","content":"hello"}
`)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	if !strings.Contains(string(data), `"cli_version":"5.6.7"`) {
		t.Fatalf("compact transcript missing cli_version; got %q", string(data))
	}
}

func TestCompactTranscriptDefaultsCLIVersion(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "")

	path := writeTranscriptFixture(t, `{"v":1,"type":"prompt","content":"hi"}
{"v":1,"type":"response","content":"hello"}
`)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	if !strings.Contains(string(data), `"cli_version":"unknown"`) {
		t.Fatalf("compact transcript should use 'unknown' as default cli_version; got %q", string(data))
	}
}

func TestCompactTranscriptSkipsFileRecords(t *testing.T) {
	path := writeTranscriptFixture(t, `{"v":1,"type":"file","path":"main.go"}
{"v":1,"type":"prompt","content":"hi"}
{"v":1,"type":"response","content":"hello"}
`)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	// File records should not appear in compact output.
	if strings.Contains(string(data), `"file"`) {
		t.Fatalf("compact transcript should not contain file records; got %q", string(data))
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (user + assistant), got %d", len(lines))
	}
}

func TestCompactTranscriptErrorsOnEmptyTranscript(t *testing.T) {
	path := writeTranscriptFixture(t, "")

	_, err := New().CompactTranscript(path)
	if err == nil {
		t.Fatal("expected error for empty transcript")
	}
}

func TestCompactTranscriptErrorsOnMissingFile(t *testing.T) {
	_, err := New().CompactTranscript("/nonexistent/path/transcript.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
