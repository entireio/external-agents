package kilo

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCompactTranscript(t *testing.T) {
	dir := t.TempDir()
	data, err := encodeMessagesJSONL([]SessionMessage{
		makeTextMessage("m1", MessageRoleUser, "do a thing"),
		{
			Info: MessageInfo{
				ID:     "m2",
				Role:   MessageRoleAssistant,
				Tokens: &Tokens{Input: 10, Output: 5},
			},
			Parts: []MessagePart{
				{Type: PartText, Text: "calling tool"},
				{
					Type:   PartTool,
					Tool:   "write",
					CallID: "c1",
					State: &ToolState{
						Status: "completed",
						Input:  json.RawMessage(`{"filePath":"/x"}`),
						Output: "ok",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	path := filepath.Join(dir, "s.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	a := New()
	resp, err := a.CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	var lines []compactTranscriptLine
	scanner := bufio.NewScanner(bytes.NewReader(decoded))
	for scanner.Scan() {
		var line compactTranscriptLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[0].Type != "user" || lines[1].Type != "assistant" {
		t.Fatalf("line types = %q, %q", lines[0].Type, lines[1].Type)
	}
	if lines[1].InputTokens != 10 || lines[1].OutputTokens != 5 {
		t.Fatalf("assistant tokens wrong: %+v", lines[1])
	}
	if lines[0].Agent != "kilo" {
		t.Fatalf("agent = %q, want kilo", lines[0].Agent)
	}
}

func TestCompactTranscriptEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New()
	if _, err := a.CompactTranscript(path); err == nil {
		t.Fatal("expected error for empty messages")
	}
}
