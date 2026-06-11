package goose

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// exportFixture is a trimmed `goose session export --format json` document
// captured from goose 1.37.0 during research verification.
const exportFixture = `{
  "id": "20260611_1",
  "working_dir": "/tmp/workspace",
  "name": "File verification request",
  "user_set_name": false,
  "session_type": "user",
  "created_at": "2026-06-11T11:02:34Z",
  "updated_at": "2026-06-11T11:02:55Z",
  "total_tokens": 2928,
  "input_tokens": 2918,
  "output_tokens": 10,
  "accumulated_total_tokens": 5774,
  "accumulated_input_tokens": 5650,
  "accumulated_output_tokens": 124,
  "accumulated_cost": 0.03135,
  "message_count": 4,
  "provider_name": "openrouter",
  "model_config": {"model_name": "anthropic/claude-opus-4.6", "context_limit": 1000000},
  "conversation": [
    {
      "id": "msg_1",
      "role": "user",
      "created": 1781175769,
      "content": [{"type": "text", "text": "Create a file named verify.txt"}],
      "metadata": {"userVisible": true, "agentVisible": true}
    },
    {
      "id": "msg_2",
      "role": "assistant",
      "created": 1781175772,
      "content": [{
        "type": "toolRequest",
        "id": "toolu_1",
        "toolCall": {
          "status": "success",
          "value": {"name": "write", "arguments": {"path": "/tmp/workspace/verify.txt", "content": "ENTIRE_VERIFY_FILE"}}
        },
        "_meta": {"goose_extension": "developer"}
      }]
    },
    {
      "id": "msg_3",
      "role": "user",
      "created": 1781175773,
      "content": [{
        "type": "toolResponse",
        "id": "toolu_1",
        "toolResult": {"status": "success"}
      }]
    },
    {
      "id": "msg_4",
      "role": "assistant",
      "created": 1781175775,
      "content": [{"type": "text", "text": "ENTIRE_VERIFY_OK"}]
    }
  ]
}`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "20260611_1.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGetTranscriptPosition(t *testing.T) {
	a := &Agent{}
	path := writeFixture(t, exportFixture)

	pos, err := a.GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition: %v", err)
	}
	if pos != 4 {
		t.Errorf("position = %d, want 4 (message count)", pos)
	}

	pos, err = a.GetTranscriptPosition(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || pos != 0 {
		t.Errorf("missing file = (%d, %v), want (0, nil)", pos, err)
	}
}

func TestExtractModifiedFiles(t *testing.T) {
	a := &Agent{}
	path := writeFixture(t, exportFixture)

	files, pos, err := a.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles: %v", err)
	}
	if len(files) != 1 || files[0] != "/tmp/workspace/verify.txt" {
		t.Errorf("files = %v", files)
	}
	if pos != 4 {
		t.Errorf("position = %d, want 4", pos)
	}

	// Offset past the write tool call sees no modifications.
	files, _, err = a.ExtractModifiedFiles(path, 2)
	if err != nil || len(files) != 0 {
		t.Errorf("offset 2: files = %v, err = %v", files, err)
	}
}

func TestExtractPrompts(t *testing.T) {
	a := &Agent{}
	path := writeFixture(t, exportFixture)

	prompts, err := a.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts: %v", err)
	}
	// The toolResponse user message must not count as a prompt.
	if len(prompts) != 1 || prompts[0] != "Create a file named verify.txt" {
		t.Errorf("prompts = %v", prompts)
	}
}

func TestExtractSummary(t *testing.T) {
	a := &Agent{}
	path := writeFixture(t, exportFixture)

	summary, ok, err := a.ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary: %v", err)
	}
	if !ok || summary != "File verification request" {
		t.Errorf("summary = (%q, %v)", summary, ok)
	}
}

func TestCalculateTokens(t *testing.T) {
	a := &Agent{}

	usage, err := a.CalculateTokens([]byte(exportFixture), 0)
	if err != nil {
		t.Fatalf("CalculateTokens: %v", err)
	}
	if usage.InputTokens != 5650 || usage.OutputTokens != 124 {
		t.Errorf("tokens = %+v", usage)
	}
	if usage.APICallCount != 2 {
		t.Errorf("api calls = %d, want 2 (assistant messages)", usage.APICallCount)
	}

	if _, err := a.CalculateTokens([]byte("not json"), 0); err == nil {
		t.Error("invalid transcript must error")
	}
}

func TestChunkAndReassemble(t *testing.T) {
	a := &Agent{}
	content := []byte("0123456789")

	if _, err := a.ChunkTranscript(content, 0); err == nil {
		t.Error("max-size 0 must error")
	}

	chunks, err := a.ChunkTranscript(content, 3)
	if err != nil {
		t.Fatalf("ChunkTranscript: %v", err)
	}
	if len(chunks) != 4 {
		t.Errorf("chunks = %d, want 4", len(chunks))
	}
	round, err := a.ReassembleTranscript(chunks)
	if err != nil || !bytes.Equal(round, content) {
		t.Errorf("round trip = %q, err = %v", round, err)
	}
}

func TestReadSessionFromExport(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	a := &Agent{}
	path := writeFixture(t, exportFixture)

	session, err := a.ReadSession(&protocol.HookInputJSON{SessionID: "20260611_1", SessionRef: path})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if session.SessionID != "20260611_1" {
		t.Errorf("session id = %q", session.SessionID)
	}
	if session.AgentName != "goose" {
		t.Errorf("agent name = %q", session.AgentName)
	}
	if session.StartTime != "2026-06-11T11:02:34Z" {
		t.Errorf("start time = %q", session.StartTime)
	}
	if len(session.ModifiedFiles) != 1 {
		t.Errorf("modified files = %v", session.ModifiedFiles)
	}
	if session.NewFiles == nil || session.DeletedFiles == nil {
		t.Error("file slices must be initialized")
	}
}

func TestWriteThenReadSessionRoundTripsOpaqueData(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	a := &Agent{}
	ref := filepath.Join(t.TempDir(), "opaque.json")
	native := []byte(`{"anything":"goes"}`)

	err := a.WriteSession(protocol.AgentSessionJSON{
		SessionID:  "opaque",
		SessionRef: ref,
		NativeData: native,
	})
	if err != nil {
		t.Fatalf("WriteSession: %v", err)
	}

	session, err := a.ReadSession(&protocol.HookInputJSON{SessionID: "opaque", SessionRef: ref})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if !bytes.Equal(session.NativeData, native) {
		t.Errorf("native data = %s", session.NativeData)
	}
}

func TestPrepareTranscript(t *testing.T) {
	runner := &stubRunner{export: []byte(exportFixture)}
	a := &Agent{CommandRunner: runner}

	// Goose-style session id: export is invoked.
	ref := filepath.Join(t.TempDir(), "20260611_1.json")
	if err := a.PrepareTranscript(ref); err != nil {
		t.Fatalf("PrepareTranscript: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "20260611_1" {
		t.Errorf("export calls = %v", runner.calls)
	}

	// Existing non-goose file: left untouched, no export.
	opaque := writeFixture(t, `{"opaque":true}`)
	opaqueRenamed := filepath.Join(filepath.Dir(opaque), "not-a-session.json")
	if err := os.Rename(opaque, opaqueRenamed); err != nil {
		t.Fatal(err)
	}
	if err := a.PrepareTranscript(opaqueRenamed); err != nil {
		t.Errorf("existing non-goose file must be a no-op, got %v", err)
	}
	if len(runner.calls) != 1 {
		t.Errorf("no extra export expected, calls = %v", runner.calls)
	}

	// Missing file with a non-goose name cannot be prepared.
	if err := a.PrepareTranscript(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("missing non-goose file must error")
	}

	if err := a.PrepareTranscript(""); err == nil {
		t.Error("empty session ref must error")
	}
}

func TestResolveSessionFileAndDir(t *testing.T) {
	a := &Agent{}

	root := t.TempDir()
	t.Setenv("GOOSE_PATH_ROOT", root)
	dir, err := a.GetSessionDir("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "data", "sessions") {
		t.Errorf("session dir = %q", dir)
	}

	t.Setenv("GOOSE_PATH_ROOT", "")
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir, err = a.GetSessionDir("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(dataHome, "goose", "sessions") {
		t.Errorf("session dir = %q", dir)
	}

	file := a.ResolveSessionFile(dir, "20260611_1")
	if file != filepath.Join(dir, "20260611_1.json") {
		t.Errorf("session file = %q", file)
	}
}
