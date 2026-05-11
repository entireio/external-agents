package amp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

const testTranscriptJSONL = `{"type":"agent.start","thread_id":"T-123","message":"Create hello.txt","timestamp":"2026-05-07T12:00:00Z"}
{"type":"agent.end","thread_id":"T-123","status":"done","modified_files":["hello.txt"],"timestamp":"2026-05-07T12:00:03Z","messages":[{"role":"user","id":1,"content":[{"type":"text","text":"Create hello.txt"}]},{"role":"assistant","id":"a1","content":[{"type":"tool_use","id":"tool1","name":"edit_file","input":{"path":"hello.txt"}},{"type":"text","text":"Created hello.txt"}]},{"role":"user","id":2,"content":[{"type":"tool_result","toolUseID":"tool1","status":"done","output":"ok"}]}]}
`

const testPreparedTranscriptJSON = `{"v":1,"id":"T-123","created":1778155200000,"updatedAt":"2026-05-07T12:00:05Z","messages":[{"role":"user","messageId":1,"meta":{"sentAt":1778155200000},"content":[{"type":"text","text":"Create hello.txt"}]},{"role":"assistant","messageId":"a1","meta":{"sentAt":1778155201000},"usage":{"model":"claude","timestamp":"2026-05-07T12:00:01Z","inputTokens":100,"outputTokens":25,"cacheReadInputTokens":7,"cacheCreationInputTokens":3},"content":[{"type":"tool_use","id":"tool1","name":"edit_file","input":{"path":"hello.txt"}},{"type":"text","text":"Created hello.txt"}]},{"role":"user","messageId":2,"meta":{"sentAt":1778155202000},"content":[{"type":"tool_result","toolUseID":"tool1","run":{"status":"done","trackFiles":["hello.txt"],"result":{"output":"ok"}}}]}]}`

type fakeCommandRunner struct {
	threadID   string
	outputPath string
	err        error
}

func (r *fakeCommandRunner) ExportThread(_ context.Context, threadID string, outputPath string) (string, error) {
	r.threadID = threadID
	r.outputPath = outputPath
	if r.err != nil {
		return "", r.err
	}
	if err := os.WriteFile(outputPath, []byte(testPreparedTranscriptJSON), 0o600); err != nil {
		return "", err
	}
	return outputPath, nil
}

func writeTestTranscript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "amp.jsonl")
	if err := os.WriteFile(path, []byte(testTranscriptJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePreparedTranscript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "amp.json")
	if err := os.WriteFile(path, []byte(testPreparedTranscriptJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractModifiedFiles(t *testing.T) {
	agent := New()
	path := writeTestTranscript(t)
	if _, _, err := agent.ExtractModifiedFiles(path, 0); err == nil {
		t.Fatal("expected unprepared transcript error")
	}
}

func TestExtractModifiedFiles_PreparedJSON(t *testing.T) {
	agent := New()
	path := filepath.Join(t.TempDir(), "amp.json")
	if err := os.WriteFile(path, []byte(testPreparedTranscriptJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	files, pos, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Fatalf("files = %v", files)
	}
	if pos != 3 {
		t.Fatalf("position = %d, want 3", pos)
	}
	files, pos, err = agent.ExtractModifiedFiles(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || pos != 3 {
		t.Fatalf("offset files = %v position = %d, want no files at position 3", files, pos)
	}
}

func TestExtractPrompts(t *testing.T) {
	agent := New()
	if _, err := agent.ExtractPrompts(writeTestTranscript(t), 0); err == nil {
		t.Fatal("expected unprepared transcript error")
	}
}

func TestExtractPrompts_PreparedJSON(t *testing.T) {
	agent := New()
	path := filepath.Join(t.TempDir(), "amp.json")
	if err := os.WriteFile(path, []byte(testPreparedTranscriptJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "Create hello.txt" {
		t.Fatalf("prompts = %v", prompts)
	}
	prompts, err = agent.ExtractPrompts(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 0 {
		t.Fatalf("offset prompts = %v, want none", prompts)
	}
}

func TestExtractSummary(t *testing.T) {
	agent := New()
	if _, _, err := agent.ExtractSummary(writeTestTranscript(t)); err == nil {
		t.Fatal("expected unprepared transcript error")
	}
}

func TestExtractSummary_PreparedJSON(t *testing.T) {
	agent := New()
	path := filepath.Join(t.TempDir(), "amp.json")
	if err := os.WriteFile(path, []byte(testPreparedTranscriptJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	summary, has, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if !has || summary != "Created hello.txt" {
		t.Fatalf("summary = %q has=%v", summary, has)
	}
}

func TestReadSession(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	path := writeTestTranscript(t)
	agent := New()
	if _, err := agent.ReadSession(&protocol.HookInputJSON{SessionRef: path}); err == nil {
		t.Fatal("expected unprepared transcript error")
	}
}

func TestReadSession_PreparedJSON(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	path := filepath.Join(t.TempDir(), "amp.json")
	if err := os.WriteFile(path, []byte(testPreparedTranscriptJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := New()
	session, err := agent.ReadSession(&protocol.HookInputJSON{SessionRef: path})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "T-123" || session.AgentName != "amp" || len(session.NativeData) == 0 {
		t.Fatalf("session = %+v", session)
	}
	if len(session.ModifiedFiles) != 1 || session.ModifiedFiles[0] != "hello.txt" {
		t.Fatalf("modified files = %v", session.ModifiedFiles)
	}
}

func TestCalculateTokens_PreparedJSON(t *testing.T) {
	agent := New()
	usage, err := agent.CalculateTokens([]byte(testPreparedTranscriptJSON), 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 25 || usage.CacheReadTokens != 7 || usage.CacheCreationTokens != 3 || usage.APICallCount != 1 {
		t.Fatalf("usage = %+v", usage)
	}
	usage, err = agent.CalculateTokens([]byte(testPreparedTranscriptJSON), 2)
	if err != nil {
		t.Fatal(err)
	}
	if usage.APICallCount != 0 || usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.CacheReadTokens != 0 || usage.CacheCreationTokens != 0 {
		t.Fatalf("offset usage = %+v, want zero usage", usage)
	}
}

func TestGetTranscriptPosition_PreparedJSON(t *testing.T) {
	agent := New()
	path := writePreparedTranscript(t)
	pos, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 3 {
		t.Fatalf("position = %d, want 3", pos)
	}
}

func TestPrepareTranscript(t *testing.T) {
	path := writePreparedTranscript(t)
	runner := &fakeCommandRunner{}
	agent := &Agent{CommandRunner: runner}
	if err := agent.PrepareTranscript(path); err != nil {
		t.Fatal(err)
	}
	if runner.threadID != "T-123" {
		t.Fatalf("threadID = %q", runner.threadID)
	}
	if runner.outputPath != path {
		t.Fatalf("outputPath = %q", runner.outputPath)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != testPreparedTranscriptJSON {
		t.Fatalf("prepared data = %s", data)
	}
}

func TestPrepareTranscript_UnpreparedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amp.jsonl")
	if err := os.WriteFile(path, []byte(testTranscriptJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	err := (&Agent{CommandRunner: runner}).PrepareTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if runner.threadID != "T-123" {
		t.Fatalf("threadID = %q, want T-123", runner.threadID)
	}
}

func TestPrepareTranscript_UnpreparedFileMissingThreadID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amp.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"agent.start\",\"message\":\"hello\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	err := (&Agent{CommandRunner: runner}).PrepareTranscript(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if runner.threadID != "" {
		t.Fatalf("runner unexpectedly invoked with threadID = %q", runner.threadID)
	}
}

func TestPrepareTranscript_RunnerError(t *testing.T) {
	path := writePreparedTranscript(t)
	wantErr := errors.New("boom")
	err := (&Agent{CommandRunner: &fakeCommandRunner{err: wantErr}}).PrepareTranscript(path)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestChunkAndReassemble(t *testing.T) {
	agent := New()
	chunks, err := agent.ChunkTranscript([]byte("hello world"), 5)
	if err != nil {
		t.Fatal(err)
	}
	got, err := agent.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteSession(t *testing.T) {
	agent := New()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := agent.WriteSession(protocol.AgentSessionJSON{SessionRef: path, NativeData: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "x" {
		t.Fatalf("data = %q", data)
	}
}
