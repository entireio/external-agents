package kiro

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestCompactTranscriptFromCLITranscript(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "9.9.9")

	path := writeCompactTranscriptFixture(t, testCLIAnalyzerTranscript)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}

	want := strings.Join([]string{
		`{"v":1,"agent":"kiro","cli_version":"9.9.9","type":"user","ts":"2026-01-01T00:00:00Z","content":[{"text":"Create a hello.go file"}]}`,
		`{"v":1,"agent":"kiro","cli_version":"9.9.9","type":"assistant","ts":"2026-01-01T00:00:00Z","id":"msg-1","content":[{"type":"text","text":"I'll create that file for you."}]}`,
		`{"v":1,"agent":"kiro","cli_version":"9.9.9","type":"user","ts":"2026-01-01T00:01:00Z","content":[{"text":"Now add a test"}]}`,
		`{"v":1,"agent":"kiro","cli_version":"9.9.9","type":"assistant","ts":"2026-01-01T00:01:00Z","id":"msg-2","content":[{"type":"tool_use","id":"tu-1","name":"Write","input":{"content":"package main","path":"/repo/hello.go"},"result":{"output":"ok","status":"success"}}]}`,
		`{"v":1,"agent":"kiro","cli_version":"9.9.9","type":"assistant","id":"msg-3","content":[{"type":"tool_use","id":"tu-2","name":"Write","input":{"content":"package main","path":"/repo/hello_test.go"},"result":{"output":"ok","status":"success"}}]}`,
		`{"v":1,"agent":"kiro","cli_version":"9.9.9","type":"assistant","id":"msg-4","content":[{"type":"text","text":"Done! I created both files."}]}`,
		"",
	}, "\n")

	if string(data) != want {
		t.Fatalf("compact transcript = %q, want %q", string(data), want)
	}
}

func TestCompactTranscriptFromIDETranscript(t *testing.T) {
	t.Setenv("ENTIRE_CLI_VERSION", "1.2.3")

	path := writeCompactTranscriptFixture(t, testIDEAnalyzerTranscript)

	resp, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript() error = %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript: %v", err)
	}

	want := strings.Join([]string{
		`{"v":1,"agent":"kiro","cli_version":"1.2.3","type":"user","content":[{"text":"Open the workspace"}]}`,
		`{"v":1,"agent":"kiro","cli_version":"1.2.3","type":"assistant","content":[{"type":"text","text":"I opened the workspace."}]}`,
		`{"v":1,"agent":"kiro","cli_version":"1.2.3","type":"user","content":[{"text":"Create app.js"}]}`,
		`{"v":1,"agent":"kiro","cli_version":"1.2.3","type":"assistant","content":[{"type":"text","text":"Created app.js."}]}`,
		"",
	}, "\n")

	if string(data) != want {
		t.Fatalf("compact transcript = %q, want %q", string(data), want)
	}
}

func TestCompactTranscriptRejectsEmptyTranscript(t *testing.T) {
	path := writeCompactTranscriptFixture(t, `{}`)

	_, err := New().CompactTranscript(path)
	if err == nil {
		t.Fatal("expected error for empty transcript, got nil")
	}
	if !strings.Contains(err.Error(), "no history entries") {
		t.Fatalf("error = %v, want no history entries", err)
	}
}

func writeCompactTranscriptFixture(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/transcript.json"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write transcript fixture: %v", err)
	}
	return path
}
