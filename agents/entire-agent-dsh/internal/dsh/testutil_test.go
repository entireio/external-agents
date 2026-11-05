package dsh

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type testSession struct {
	NativePath string
	CacheRef   string
	SessionID  string
	CWD        string
	DSHHome    string
}

func newTestSession(t *testing.T, sessionID, cwd string, events []map[string]any) testSession {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	writeNativeSession(t, home, sessionID, cwd, events)
	native := filepath.Join(home, "sessions", projectKey(cwd), mustEncode(t, sessionID), "session.v4.jsonl.zstd")
	dir, err := (&Agent{}).GetSessionDir(cwd)
	if err != nil {
		t.Fatalf("GetSessionDir: %v", err)
	}
	return testSession{
		NativePath: native,
		CacheRef:   (&Agent{}).ResolveSessionFile(dir, sessionID),
		SessionID:  sessionID,
		CWD:        cwd,
		DSHHome:    home,
	}
}

func writeNativeSession(t *testing.T, home, sessionID, cwd string, events []map[string]any) string {
	t.Helper()
	header := map[string]any{
		"type":            "session",
		"version":         4,
		"id":              sessionID,
		"createdAt":       int64(1700000000000),
		"cwd":             cwd,
		"isSeeded":        false,
		"delegationDepth": 0,
	}
	lines := make([]string, 0, len(events)+1)
	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	lines = append(lines, string(headerBytes))
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		lines = append(lines, string(data))
	}

	var frames bytes.Buffer
	for _, line := range lines {
		var frame bytes.Buffer
		encoder, err := zstd.NewWriter(&frame)
		if err != nil {
			t.Fatalf("zstd writer: %v", err)
		}
		if _, err := encoder.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("zstd write: %v", err)
		}
		if err := encoder.Close(); err != nil {
			t.Fatalf("zstd close: %v", err)
		}
		frames.Write(frame.Bytes())
	}

	dir := filepath.Join(home, "sessions", projectKey(cwd), mustEncode(t, sessionID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create session dir: %v", err)
	}
	path := filepath.Join(dir, "session.v4.jsonl.zstd")
	if err := os.WriteFile(path, frames.Bytes(), 0o644); err != nil {
		t.Fatalf("write native session: %v", err)
	}
	return path
}

func mustEncode(t *testing.T, value string) string {
	t.Helper()
	encoded, err := encodeSegment(value)
	if err != nil {
		t.Fatalf("encodeSegment(%q): %v", value, err)
	}
	return encoded
}

func eventRecord(t *testing.T, eventType string, seq int, timeMS int64, data any) map[string]any {
	t.Helper()
	record := map[string]any{
		"type": eventType,
		"seq":  seq,
		"time": timeMS,
	}
	if data != nil {
		record["data"] = data
	}
	return record
}

func userMessage(t *testing.T, id, text, kind, form string) map[string]any {
	t.Helper()
	source := map[string]any{"kind": kind}
	if form != "" {
		source["form"] = form
	}
	return map[string]any{
		"id":      id,
		"role":    "user",
		"content": []map[string]any{{"type": "text", "text": text}},
		"source":  source,
	}
}

func assistantMessage(t *testing.T, id, model, text string, blocks []contentBlock, usage *tokenUsage, turn, step int) map[string]any {
	t.Helper()
	content := make([]map[string]any, 0, len(blocks)+1)
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	for _, block := range blocks {
		switch block.Type {
		case "tool-call":
			content = append(content, map[string]any{
				"type":      "tool-call",
				"id":        block.ID,
				"name":      block.Name,
				"arguments": block.Arguments,
			})
		case "reasoning":
			content = append(content, map[string]any{"type": "reasoning", "text": block.Text})
		}
	}
	data := map[string]any{
		"turn": turn,
		"step": step,
		"message": map[string]any{
			"id":      id,
			"role":    "assistant",
			"content": content,
			"source":  map[string]any{"kind": "model", "provider": "test", "model": model},
		},
	}
	if usage != nil {
		data["usage"] = map[string]any{
			"inputTokens":      usage.InputTokens,
			"outputTokens":     usage.OutputTokens,
			"cacheReadTokens":  usage.CacheReadTokens,
			"cacheWriteTokens": usage.CacheWriteTokens,
		}
	}
	return data
}
