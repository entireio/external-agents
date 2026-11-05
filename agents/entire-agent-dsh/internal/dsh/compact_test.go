package dsh

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactTranscript(t *testing.T) {
	session := analysisSession(t)
	agent := &Agent{}
	if err := agent.PrepareTranscript(session.CacheRef); err != nil {
		t.Fatalf("PrepareTranscript: %v", err)
	}
	compacted, err := agent.CompactTranscript(session.CacheRef)
	if err != nil {
		t.Fatalf("CompactTranscript: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(compacted.Transcript)
	if err != nil {
		t.Fatalf("decode compact transcript: %v", err)
	}

	lines := make([]compactLine, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var parsed compactLine
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("parse compact line: %v", err)
		}
		lines = append(lines, parsed)
	}
	if len(lines) != 6 {
		t.Fatalf("compact line count = %d, want 6", len(lines))
	}
	for _, line := range lines {
		if line.V != 1 || line.Agent != "dsh" || line.CLIVersion != "unknown" {
			t.Errorf("unexpected compact metadata: %+v", line)
		}
		if line.Content == nil {
			t.Error("compact content must be an array, not null")
		}
	}

	first := lines[0]
	if first.Type != "user" || len(first.Content) != 1 || first.Content[0].Text != "first prompt" || first.Content[0].Type != "" {
		t.Errorf("first user line = %+v", first)
	}

	a1 := lines[1]
	if a1.Type != "assistant" || a1.ID != "a1" || a1.Model != "model-a" {
		t.Errorf("a1 = %+v", a1)
	}
	if a1.InputTokens != 100 || a1.OutputTokens != 10 || a1.CacheReadTokens != 5 || a1.CacheWriteTokens != 2 {
		t.Errorf("a1 usage = %+v", a1)
	}
	if len(a1.Content) != 2 {
		t.Fatalf("a1 content = %+v, want text + tool_use (reasoning dropped)", a1.Content)
	}
	if a1.Content[0].Type != "text" || a1.Content[0].Text != "working" {
		t.Errorf("a1 text block = %+v", a1.Content[0])
	}
	toolUse := a1.Content[1]
	if toolUse.Type != "tool_use" || toolUse.Name != "write" || toolUse.ID != "call_w" {
		t.Errorf("a1 tool block = %+v", toolUse)
	}
	input, ok := toolUse.Input.(map[string]any)
	if !ok || input["file_path"] != "src/a.go" {
		t.Errorf("a1 tool input = %#v", toolUse.Input)
	}
	if toolUse.Result == nil || toolUse.Result.Status != "success" || toolUse.Result.Output != "ok" {
		t.Errorf("a1 tool result = %+v", toolUse.Result)
	}

	a4 := lines[5]
	if a4.Type != "assistant" || a4.Content[0].Text != "done" || a4.InputTokens != 300 {
		t.Errorf("a4 = %+v", a4)
	}
}

func TestCompactTranscriptRequiresMessages(t *testing.T) {
	data := []byte(`{"type":"session","version":4,"id":"session-empty"}` + "\n" +
		`{"type":"turn/start","seq":0,"time":1700000001000,"data":{"turn":1}}` + "\n")
	if _, err := compactTranscriptBytes(data); err == nil {
		t.Fatal("expected an error for a transcript with no messages")
	}
}

func TestCompactToolCallBeforeAssistant(t *testing.T) {
	session := newTestSession(t, "session-order", "/repo/order", []map[string]any{
		eventRecord(t, "user/message", 0, 1700000001000, userMessage(t, "u1", "write it", "user", "")),
		eventRecord(t, "tool/call", 1, 1700000001100, toolCallData{Turn: 1, Step: 1, CallID: "call_x", Name: "write", Arguments: `{"file_path":"x.txt"}`}),
		eventRecord(t, "assistant/message", 2, 1700000001200, assistantMessage(t, "a1", "model", "done", []contentBlock{
			{Type: "tool-call", ID: "call_x", Name: "write", Arguments: `{"file_path":"x.txt"}`},
		}, nil, 1, 1)),
	})
	t.Setenv("ENTIRE_REPO_ROOT", session.CWD)
	agent := &Agent{}
	if err := agent.PrepareTranscript(session.CacheRef); err != nil {
		t.Fatalf("PrepareTranscript: %v", err)
	}
	compacted, err := agent.CompactTranscript(session.CacheRef)
	if err != nil {
		t.Fatalf("CompactTranscript: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(compacted.Transcript)
	if err != nil {
		t.Fatalf("decode compact: %v", err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var parsed compactLine
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("parse line: %v", err)
		}
		for _, block := range parsed.Content {
			if block.Type == "tool_use" && block.ID == "call_x" {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("tool_use call_x appears %d times, want 1", count)
	}
}

func TestCompactImageOnlyUserMessage(t *testing.T) {
	imageUser := map[string]any{
		"id":   "u1",
		"role": "user",
		"content": []map[string]any{{
			"type":       "image",
			"attachment": map[string]any{"id": "att-1"},
		}},
		"source": map[string]any{"kind": "user"},
	}
	session := newTestSession(t, "session-image", "/repo/image", []map[string]any{
		eventRecord(t, "user/message", 0, 1700000001000, imageUser),
		eventRecord(t, "assistant/message", 1, 1700000001100, assistantMessage(t, "a1", "model", "ok", nil, nil, 1, 1)),
	})
	t.Setenv("ENTIRE_REPO_ROOT", session.CWD)
	agent := &Agent{}
	if err := agent.PrepareTranscript(session.CacheRef); err != nil {
		t.Fatalf("PrepareTranscript: %v", err)
	}
	compacted, err := agent.CompactTranscript(session.CacheRef)
	if err != nil {
		t.Fatalf("CompactTranscript: %v", err)
	}
	raw, _ := base64.StdEncoding.DecodeString(compacted.Transcript)
	first := strings.Split(strings.TrimSpace(string(raw)), "\n")[0]
	var parsed compactLine
	if err := json.Unmarshal([]byte(first), &parsed); err != nil {
		t.Fatalf("parse first line: %v", err)
	}
	if parsed.Type != "user" || len(parsed.Content) != 1 || parsed.Content[0].Text != "[image]" {
		t.Errorf("image-only user line = %+v", parsed)
	}
}
