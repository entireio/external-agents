package pi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

const testSessionJSONL = `{"type":"session","version":3,"id":"test-uuid-123","timestamp":"2026-03-27T21:00:00.000Z","cwd":"/tmp/test"}
{"type":"model_change","id":"mc1","parentId":null,"timestamp":"2026-03-27T21:00:00.001Z","provider":"anthropic","modelId":"claude-sonnet-4-6"}
{"type":"message","id":"m1","parentId":"mc1","timestamp":"2026-03-27T21:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"Create hello.txt"}],"timestamp":1774646400000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-27T21:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"write","arguments":{"path":"hello.txt","content":"hello world\n"}}],"usage":{"input":100,"output":50,"cacheRead":10,"cacheWrite":5},"stopReason":"toolUse","timestamp":1774646401000}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-03-27T21:00:03.000Z","message":{"role":"toolResult","toolCallId":"tc1","toolName":"write","content":[{"type":"text","text":"Written 12 bytes"}],"isError":false,"timestamp":1774646402000}}
{"type":"message","id":"m4","parentId":"m3","timestamp":"2026-03-27T21:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Created hello.txt with the content hello world."}],"usage":{"input":200,"output":30,"cacheRead":0,"cacheWrite":0},"stopReason":"stop","timestamp":1774646403000}}
`

func writeTestSession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-03-27T21-00-00-000Z_test-uuid-123.jsonl")
	if err := os.WriteFile(path, []byte(testSessionJSONL), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractModifiedFiles(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	files, pos, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Errorf("files = %v, want [hello.txt]", files)
	}
	if pos == 0 {
		t.Error("position should be > 0")
	}
}

func TestExtractModifiedFiles_WithOffset(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	// Use a large offset to skip all content.
	info, _ := os.Stat(path)
	files, _, err := agent.ExtractModifiedFiles(path, int(info.Size()))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want empty", files)
	}
}

func TestExtractPrompts(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "Create hello.txt" {
		t.Errorf("prompts = %v, want [Create hello.txt]", prompts)
	}
}

func TestExtractSummary(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	summary, has, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("expected has_summary = true")
	}
	if summary != "Created hello.txt with the content hello world." {
		t.Errorf("summary = %q", summary)
	}
}

func TestGetTranscriptPosition(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	pos, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if pos != int(info.Size()) {
		t.Errorf("position = %d, want %d", pos, info.Size())
	}
}

func TestGetTranscriptPosition_Missing(t *testing.T) {
	agent := New()
	pos, err := agent.GetTranscriptPosition("/nonexistent/file.jsonl")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if pos != 0 {
		t.Errorf("position = %d, want 0 for missing file", pos)
	}
}

func TestCalculateTokens(t *testing.T) {
	path := writeTestSession(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	agent := New()
	usage, err := agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatal(err)
	}

	if usage.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", usage.InputTokens)
	}
	if usage.OutputTokens != 80 {
		t.Errorf("OutputTokens = %d, want 80", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 10 {
		t.Errorf("CacheReadTokens = %d, want 10", usage.CacheReadTokens)
	}
	if usage.CacheCreationTokens != 5 {
		t.Errorf("CacheCreationTokens = %d, want 5", usage.CacheCreationTokens)
	}
	if usage.APICallCount != 2 {
		t.Errorf("APICallCount = %d, want 2", usage.APICallCount)
	}
}

func TestCalculateTokens_OffsetAtEnd(t *testing.T) {
	path := writeTestSession(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	agent := New()

	// offset == len(data): should return zero tokens
	usage, err := agent.CalculateTokens(data, len(data))
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.APICallCount != 0 {
		t.Errorf("offset=len(data): got input=%d output=%d calls=%d, want all zero",
			usage.InputTokens, usage.OutputTokens, usage.APICallCount)
	}

	// offset > len(data): should also return zero tokens
	usage, err = agent.CalculateTokens(data, len(data)+100)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.APICallCount != 0 {
		t.Errorf("offset>len(data): got input=%d output=%d calls=%d, want all zero",
			usage.InputTokens, usage.OutputTokens, usage.APICallCount)
	}
}

func TestExtractPrompts_OffsetAtEnd(t *testing.T) {
	path := writeTestSession(t)
	data, _ := os.ReadFile(path)
	agent := New()

	// offset > len(data): should return no prompts
	prompts, err := agent.ExtractPrompts(path, len(data)+100)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 0 {
		t.Errorf("offset>len(data): got %d prompts, want 0", len(prompts))
	}
}

func TestChunkAndReassemble(t *testing.T) {
	agent := New()
	data := []byte("hello world, this is a test")

	chunks, err := agent.ChunkTranscript(data, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Errorf("chunks = %d, want 3", len(chunks))
	}

	reassembled, err := agent.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if string(reassembled) != string(data) {
		t.Errorf("reassembled = %q, want %q", reassembled, data)
	}
}

func TestChunkTranscript_InvalidMaxSize(t *testing.T) {
	agent := New()
	_, err := agent.ChunkTranscript([]byte("test"), 0)
	if err == nil {
		t.Error("expected error for max-size=0")
	}
}

func TestReadSession(t *testing.T) {
	path := writeTestSession(t)
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())

	agent := New()
	session, err := agent.ReadSession(&protocol.HookInputJSON{
		SessionRef: path,
	})
	if err != nil {
		t.Fatal(err)
	}

	if session.SessionID != "test-uuid-123" {
		t.Errorf("SessionID = %q, want %q", session.SessionID, "test-uuid-123")
	}
	if session.AgentName != "pi" {
		t.Errorf("AgentName = %q, want %q", session.AgentName, "pi")
	}
	if session.StartTime != "2026-03-27T21:00:00.000Z" {
		t.Errorf("StartTime = %q", session.StartTime)
	}
	if len(session.NativeData) == 0 {
		t.Error("NativeData should not be empty")
	}
	if session.ModifiedFiles == nil {
		t.Error("ModifiedFiles must be initialized (not nil)")
	}
	if session.NewFiles == nil {
		t.Error("NewFiles must be initialized (not nil)")
	}
	if session.DeletedFiles == nil {
		t.Error("DeletedFiles must be initialized (not nil)")
	}
}

func TestWriteSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	agent := New()
	data := []byte(`{"test":"data"}`)

	err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionRef: path,
		NativeData: data,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}
