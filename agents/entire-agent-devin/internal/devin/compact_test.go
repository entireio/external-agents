package devin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"
)

// compactSample mirrors the live-captured ATIF shape including the
// observation block that carries tool results (see AGENT.md).
const compactSample = `{
  "schema_version": "ATIF-v1.7",
  "session_id": "almond-cylinder",
  "agent": {"name": "devin", "model_name": "SWE-1.7"},
  "steps": [
    {"step_id": 1, "timestamp": "2026-07-27T11:59:51Z", "source": "system", "message": "system prompt"},
    {"step_id": 2, "timestamp": "2026-07-27T11:59:52Z", "source": "user", "message": "Append a line to hello.txt"},
    {"step_id": 3, "timestamp": "2026-07-27T12:01:40Z", "source": "agent", "message": "", "model_name": "SWE-1.7",
     "tool_calls": [{"tool_call_id": "read_0", "function_name": "read", "arguments": {"file_path": "/repo/hello.txt"}}],
     "observation": {"results": [{"source_call_id": "read_0", "content": "1|hook test ok"}]},
     "metrics": {"prompt_tokens": 10, "completion_tokens": 5, "cached_tokens": 2}},
    {"step_id": 4, "timestamp": "2026-07-27T12:01:50Z", "source": "agent", "message": "Done.", "model_name": "SWE-1.7",
     "metrics": {"prompt_tokens": 12, "completion_tokens": 3, "cached_tokens": 10}}
  ]
}`

func TestCompactTranscriptBytes(t *testing.T) {
	t.Parallel()

	out, err := compactTranscriptBytes([]byte(compactSample))
	if err != nil {
		t.Fatalf("compactTranscriptBytes: %v", err)
	}

	var lines []compactLine
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		var line compactLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("invalid compact line %q: %v", scanner.Text(), err)
		}
		lines = append(lines, line)
	}

	// system step skipped; user + two agent steps remain.
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if lines[0].Type != "user" || lines[1].Type != "assistant" || lines[2].Type != "assistant" {
		t.Errorf("line types = %s/%s/%s", lines[0].Type, lines[1].Type, lines[2].Type)
	}
	for i, line := range lines {
		if line.V != 1 || line.Agent != AgentName {
			t.Errorf("line %d envelope = v%d agent %q", i, line.V, line.Agent)
		}
	}

	// The tool_use block must carry the joined observation result.
	blocks, ok := lines[1].Content.([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("assistant content = %#v, want 1 block", lines[1].Content)
	}
	block, ok := blocks[0].(map[string]any)
	if !ok {
		t.Fatalf("block = %#v", blocks[0])
	}
	if block["type"] != "tool_use" || block["name"] != "read" || block["id"] != "read_0" {
		t.Errorf("tool_use block = %v", block)
	}
	result, ok := block["result"].(map[string]any)
	if !ok || result["output"] != "1|hook test ok" || result["status"] != "success" {
		t.Errorf("tool result = %v", block["result"])
	}
}

func TestCompactTranscriptBytes_TextAndEmpty(t *testing.T) {
	t.Parallel()

	out, err := compactTranscriptBytes([]byte(compactSample))
	if err != nil {
		t.Fatalf("compactTranscriptBytes: %v", err)
	}
	if !bytes.Contains(out, []byte(`"text":"Done."`)) {
		t.Error("final assistant text block missing")
	}

	// Non-ATIF content errors rather than fabricating an empty transcript.
	if _, err := compactTranscriptBytes([]byte("not json")); err == nil {
		t.Error("expected error for non-ATIF content")
	}

	// Empty steps produce an empty (but valid) compact transcript.
	empty := `{"schema_version":"ATIF-v1.7","session_id":"x","steps":[]}`
	out, err = compactTranscriptBytes([]byte(empty))
	if err != nil {
		t.Fatalf("empty transcript: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("empty transcript output = %q, want empty", out)
	}
}
