package goose

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactTranscript(t *testing.T) {
	a := &Agent{}
	path := writeFixture(t, exportFixture)

	resp, err := a.CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(resp.Transcript)
	if err != nil {
		t.Fatalf("decode transcript base64: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	// Fixture: user prompt, assistant tool call, toolResponse (folded into the
	// tool call), assistant text → 3 compact lines.
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3:\n%s", len(lines), raw)
	}

	var first struct {
		V       int    `json:"v"`
		Agent   string `json:"agent"`
		Type    string `json:"type"`
		TS      string `json:"ts"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("parse first line: %v", err)
	}
	if first.V != 1 || first.Agent != "goose" || first.Type != "user" {
		t.Errorf("first line header = %+v", first)
	}
	if len(first.Content) != 1 || first.Content[0].Text != "Create a file named verify.txt" {
		t.Errorf("first line content = %+v", first.Content)
	}
	if first.TS == "" {
		t.Error("timestamp must be set")
	}

	var second struct {
		Type    string `json:"type"`
		Content []struct {
			Type   string `json:"type"`
			Name   string `json:"name"`
			Input  map[string]any `json:"input"`
			Result *struct {
				Output string `json:"output"`
				Status string `json:"status"`
			} `json:"result"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("parse second line: %v", err)
	}
	if second.Type != "assistant" || len(second.Content) != 1 {
		t.Fatalf("second line = %+v", second)
	}
	tool := second.Content[0]
	if tool.Type != "tool_use" || tool.Name != "write" {
		t.Errorf("tool block = %+v", tool)
	}
	if tool.Input["path"] != "/tmp/workspace/verify.txt" {
		t.Errorf("tool input = %+v", tool.Input)
	}
	if tool.Result == nil || tool.Result.Status != "success" {
		t.Errorf("tool result = %+v", tool.Result)
	}

	// Final assistant text response must be present — this is what makes
	// goose's answers visible in entire explain.
	if !strings.Contains(lines[2], `"ENTIRE_VERIFY_OK"`) || !strings.Contains(lines[2], `"assistant"`) {
		t.Errorf("third line = %s", lines[2])
	}
}

func TestCompactTranscriptRejectsInvalidInput(t *testing.T) {
	a := &Agent{}

	if _, err := a.CompactTranscript(writeFixture(t, `{"id":"x","conversation":[]}`)); err == nil {
		t.Error("empty conversation must error")
	}
	if _, err := a.CompactTranscript(writeFixture(t, "not json")); err == nil {
		t.Error("invalid JSON must error")
	}
}
