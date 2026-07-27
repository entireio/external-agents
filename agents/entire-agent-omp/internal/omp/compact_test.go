package omp

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func ompCompactFixture() []byte {
	return []byte(ompTitleSlot() + ompHeader() +
		`{"type":"model_change","id":"model","parentId":null,"timestamp":"2026-07-26T03:14:07Z","model":"openai/gpt"}` + "\n" +
		`{"type":"thinking_level_change","id":"thinking-level","parentId":"model","timestamp":"2026-07-26T03:14:08Z","thinkingLevel":"medium"}` + "\n" +
		`{"type":"custom_message","customType":"notice","content":"hidden","id":"custom","parentId":"thinking-level","timestamp":"2026-07-26T03:14:09Z"}` + "\n" +
		`{"type":"message","id":"user","parentId":"custom","timestamp":"2026-07-26T03:14:10Z","message":{"role":"user","content":[{"type":"text","text":"first"},42,{"type":"image","data":"ignored"},{"type":"text","text":"second"}]}}` + "\n" +
		`{"type":"message","id":"assistant-tool","parentId":"user","timestamp":"2026-07-26T03:14:11Z","message":{"role":"assistant","model":"openai/gpt-5","content":[{"type":"thinking","thinking":"hidden"},{"type":"toolCall","id":"tool-1","name":"Write","arguments":{"path":"x.go"}},{"type":"toolCall","id":"tool-2","name":"mcp:read","arguments":{"path":"x.go"}},{"type":"image","data":"ignored"}],"usage":{"input":101,"output":23}}}` + "\n" +
		`{"type":"message","id":"result-1","parentId":"assistant-tool","timestamp":"2026-07-26T03:14:12Z","message":{"role":"toolResult","toolCallId":"tool-1","toolName":"Write","content":[{"type":"text","text":"written"}],"isError":false}}` + "\n" +
		`{"type":"message","id":"result-2","parentId":"result-1","timestamp":"2026-07-26T03:14:13Z","message":{"role":"toolResult","toolCallId":"tool-2","toolName":"read","content":"permission denied","isError":true}}` + "\n" +
		`{"type":"message","id":"assistant-final","parentId":"result-2","timestamp":"2026-07-26T03:14:14Z","message":{"role":"assistant","model":"openai/gpt-5","content":[{"type":"fallback","text":"ignored"},"bad block",{"type":"text","text":"done"}]}}` + "\n")
}

type decodedCompactLine struct {
	V            int               `json:"v"`
	Agent        string            `json:"agent"`
	CLIVersion   string            `json:"cli_version"`
	Type         string            `json:"type"`
	ID           string            `json:"id"`
	InputTokens  int               `json:"input_tokens"`
	OutputTokens int               `json:"output_tokens"`
	Content      []json.RawMessage `json:"content"`
}

func decodeCompactLines(t *testing.T, data []byte) []decodedCompactLine {
	t.Helper()
	var lines []decodedCompactLine
	for _, encoded := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var line decodedCompactLine
		if err := json.Unmarshal([]byte(encoded), &line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	return lines
}

func TestCompactActiveMessagesToolsResultsAndTokens(t *testing.T) {
	data, err := compactTranscriptBytes(ompCompactFixture())
	if err != nil {
		t.Fatal(err)
	}
	lines := decodeCompactLines(t, data)
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3: %s", len(lines), data)
	}
	if lines[0].V != 1 || lines[0].Agent != "omp" || lines[0].CLIVersion == "" || lines[0].Type != "user" || len(lines[0].Content) != 2 {
		t.Fatalf("unexpected user line: %+v", lines[0])
	}
	var first, second compactUserBlock
	if err := json.Unmarshal(lines[0].Content[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[0].Content[1], &second); err != nil {
		t.Fatal(err)
	}
	if first.Text != "first" || second.Text != "second" {
		t.Fatalf("user blocks = %+v, %+v", first, second)
	}
	if lines[1].Type != "assistant" || lines[1].ID != "assistant-tool" || lines[1].InputTokens != 101 || lines[1].OutputTokens != 23 || len(lines[1].Content) != 2 {
		t.Fatalf("unexpected tool line: %+v", lines[1])
	}
	var writeTool, readTool compactToolBlock
	if err := json.Unmarshal(lines[1].Content[0], &writeTool); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1].Content[1], &readTool); err != nil {
		t.Fatal(err)
	}
	if writeTool.Name != "write" || writeTool.Result == nil || writeTool.Result.Status != "success" || writeTool.Result.Output != "written" {
		t.Fatalf("write tool = %+v", writeTool)
	}
	if readTool.Name != "read" || readTool.Result == nil || readTool.Result.Status != "error" || readTool.Result.Output != "permission denied" {
		t.Fatalf("read tool = %+v", readTool)
	}
	if lines[2].Type != "assistant" || lines[2].ID != "assistant-final" || len(lines[2].Content) != 1 {
		t.Fatalf("unexpected final line: %+v", lines[2])
	}
	var text compactTextBlock
	if err := json.Unmarshal(lines[2].Content[0], &text); err != nil {
		t.Fatal(err)
	}
	if text.Type != "text" || text.Text != "done" {
		t.Fatalf("text block = %+v", text)
	}
}

func TestCompactTranscriptReturnsBase64V1JSONL(t *testing.T) {
	path := writeOMPFixture(t, ompCompactFixture())
	response, err := (&Agent{}).CompactTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(response.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	lines := decodeCompactLines(t, decoded)
	if len(lines) != 3 || lines[0].V != 1 {
		t.Fatalf("decoded compact transcript = %s", decoded)
	}
}

func TestCompactStringUserAndMalformedBlocks(t *testing.T) {
	data := []byte(ompHeader() +
		`{"type":"message","id":"user","parentId":null,"message":{"role":"user","content":"plain user"}}` + "\n" +
		`{"type":"message","id":"assistant","parentId":"user","message":{"role":"assistant","content":[null,7,{"type":"toolCall","id":"missing-name","arguments":"bad"},{"type":"text","text":"valid"}]}}` + "\n")
	compacted, err := compactTranscriptBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	lines := decodeCompactLines(t, compacted)
	if len(lines) != 2 || len(lines[0].Content) != 1 || len(lines[1].Content) != 1 {
		t.Fatalf("unexpected lines: %s", compacted)
	}
	var user compactUserBlock
	if err := json.Unmarshal(lines[0].Content[0], &user); err != nil || user.Text != "plain user" {
		t.Fatalf("user = %+v, err = %v", user, err)
	}
}

func TestCompactToleratesMissingSessionHeader(t *testing.T) {
	// Entire scopes checkpoint transcripts from a mid-session offset before
	// calling compact-transcript, so the title/session header is dropped.
	// Compaction must still succeed on the header-less slice, matching the
	// tolerant behavior of the transcript_analyzer methods.
	parts := strings.SplitN(string(ompCompactFixture()), "\n", 3)
	if len(parts) < 3 {
		t.Fatalf("fixture too short to strip header")
	}
	headerless := []byte(parts[2])

	data, err := compactTranscriptBytes(headerless)
	if err != nil {
		t.Fatalf("compact header-less slice: %v", err)
	}
	if lines := decodeCompactLines(t, data); len(lines) != 3 {
		t.Fatalf("lines = %d, want 3: %s", len(lines), data)
	}
}

func TestCompactRejectsNoOutputAndInvalidNative(t *testing.T) {
	noOutput := []byte(ompHeader() +
		`{"type":"message","id":"thinking","parentId":null,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"image","data":"x"}]}}` + "\n")
	if _, err := compactTranscriptBytes(noOutput); err == nil {
		t.Fatal("expected no-output error")
	}
	if _, err := compactTranscriptBytes([]byte("{}\n")); err == nil {
		t.Fatal("expected invalid native transcript error")
	}
	path := writeOMPFixture(t, noOutput)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Agent{}).CompactTranscript(path); err == nil {
		t.Fatal("expected compact method no-output error")
	}
}
