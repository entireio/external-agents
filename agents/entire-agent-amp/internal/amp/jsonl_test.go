package amp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

// sliceFromLine mirrors Entire's transcript.SliceFromLine
// (cmd/entire/cli/transcript/parse.go:114): return the bytes starting after the
// Nth newline, or nil when there aren't that many lines. This is what
// cmd/entire/cli/explain.go:1883 (scopeTranscriptForCheckpoint) and
// cmd/entire/cli/strategy/manual_commit_condensation.go:1006 apply to every
// agent type that is not one of Entire's built-ins — amp included.
func sliceFromLine(content []byte, startLine int) []byte {
	if len(content) == 0 || startLine <= 0 {
		return content
	}
	count, offset := 0, 0
	for i, b := range content {
		if b == '\n' {
			count++
			if count == startLine {
				offset = i + 1
				break
			}
		}
	}
	if count < startLine || offset >= len(content) {
		return nil
	}
	return content[offset:]
}

func fourMessageTranscript(t *testing.T) []byte {
	t.Helper()
	data, err := materializeThread(&Thread{
		ID: "T-4",
		Messages: []ThreadMessage{
			makeTextMessage("1", ThreadMessageRoleUser, "first prompt"),
			makeTextMessage("a1", ThreadMessageRoleAssistant, "first answer"),
			makeTextMessage("2", ThreadMessageRoleUser, "second prompt"),
			makeTextMessage("a2", ThreadMessageRoleAssistant, "second answer"),
		},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	return data
}

func makeTextMessage(id ThreadMessageID, role ThreadMessageRole, text string) ThreadMessage {
	return ThreadMessage{
		MessageID: id,
		Role:      role,
		Meta:      &ThreadMessageMeta{SentAt: 1778155200000},
		Content:   []ThreadContentBlock{{Type: ThreadContentText, Text: text}},
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	data := fourMessageTranscript(t)
	if lines := bytes.Count(data, []byte{'\n'}); lines != 4 {
		t.Fatalf("expected 4 newline-terminated lines, got %d", lines)
	}
	msgs, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 4 || msgs[0].MessageID != "1" || msgs[3].MessageID != "a2" {
		t.Fatalf("round-trip messages = %+v", msgs)
	}
	for _, msg := range msgs {
		if msg.ThreadID != "T-4" {
			t.Fatalf("thread id not stamped on message %q", msg.MessageID)
		}
	}
}

// The regression this whole change exists for: Entire scopes checkpoint
// transcripts by line offset before calling compact-transcript. With
// one-message-per-line JSONL a mid-session slice is still valid and compacts;
// the old single-blob layout produced empty/invalid bytes and summaries failed.
func TestCompactToleratesLineScopedSlice(t *testing.T) {
	full := fourMessageTranscript(t)

	// Offset 2 == start of the second turn (messages 2, a2).
	scoped := sliceFromLine(full, 2)
	if len(scoped) == 0 {
		t.Fatal("scoped slice unexpectedly empty")
	}
	compacted, err := compactTranscriptBytes(scoped)
	if err != nil {
		t.Fatalf("compact scoped slice: %v", err)
	}
	if got := bytes.Count(compacted, []byte{'\n'}); got != 2 {
		t.Fatalf("compact scoped slice produced %d lines, want 2", got)
	}
	if !bytes.Contains(compacted, []byte("second prompt")) {
		t.Fatalf("scoped compact missing expected content: %s", compacted)
	}
	if bytes.Contains(compacted, []byte("first prompt")) {
		t.Fatalf("scoped compact leaked pre-offset content: %s", compacted)
	}
}

// A scoped slice has no thread header, so every analyzer must still work on it
// and the thread id must still be recoverable.
func TestScopedSliceRemainsAnalyzable(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	full := fourMessageTranscript(t)
	path := filepath.Join(t.TempDir(), "amp.jsonl")
	if err := os.WriteFile(path, sliceFromLine(full, 2), 0o600); err != nil {
		t.Fatal(err)
	}

	agent := New()
	pos, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 2 {
		t.Fatalf("scoped position = %d, want 2", pos)
	}
	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "second prompt" {
		t.Fatalf("scoped prompts = %v", prompts)
	}
	session, err := agent.ReadSession(&protocol.HookInputJSON{SessionRef: path})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "T-4" {
		t.Fatalf("scoped session id = %q, want T-4 (recovered from a message)", session.SessionID)
	}
}

func TestGetTranscriptPositionMatchesLineCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amp.jsonl")
	data := fourMessageTranscript(t)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	pos, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition: %v", err)
	}
	if lines := bytes.Count(data, []byte{'\n'}); pos != lines {
		t.Fatalf("position %d != newline count %d (Entire slices by line)", pos, lines)
	}
}

// The native export is an ingestion format: it must be converted before it is
// stored, never written to session_ref as-is.
func TestExportThreadStoresJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amp.jsonl")
	runner := &fakeCommandRunner{}
	if err := (&Agent{CommandRunner: runner}).exportThread("T-123", path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(`{"v":`)) {
		t.Fatalf("native single-document export was stored verbatim: %s", data)
	}
	if lines := bytes.Count(data, []byte{'\n'}); lines != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d: %s", lines, data)
	}
	for line := range bytes.SplitSeq(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		var msg ThreadMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			t.Fatalf("line not valid message JSON: %v (%s)", err, line)
		}
		if msg.ThreadID != "T-123" {
			t.Fatalf("line missing thread id: %s", line)
		}
	}
}

// An export whose document omits the id still gets the requested thread id, so
// scoped slices stay identifiable.
func TestExportThreadStampsRequestedThreadID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amp.jsonl")
	runner := &stubExportRunner{out: []byte(`{"messages":[{"role":"user","messageId":1,"content":[{"type":"text","text":"hi"}]}]}`)}
	if err := (&Agent{CommandRunner: runner}).exportThread("T-fallback", path); err != nil {
		t.Fatal(err)
	}
	messages, err := decodeTranscript(readFileT(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ThreadID != "T-fallback" {
		t.Fatalf("messages = %+v", messages)
	}
}

// Session files written before amp materialized JSONL must keep reading; the
// next export rewrites them.
func TestDecodeTranscriptAcceptsLegacyThreadExport(t *testing.T) {
	messages, err := decodeTranscript([]byte(testPreparedTranscriptJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("legacy messages = %d, want 3", len(messages))
	}
	if messages[0].ThreadID != "T-123" {
		t.Fatalf("legacy thread id = %q, want T-123", messages[0].ThreadID)
	}
}

func TestDecodeTranscriptReportsUnpreparedHookPayloads(t *testing.T) {
	if _, err := decodeTranscript([]byte(testTranscriptJSONL)); err == nil {
		t.Fatal("expected unprepared transcript error")
	}
}

type stubExportRunner struct {
	out []byte
}

func (r *stubExportRunner) ExportThread(_ context.Context, _ string) ([]byte, error) {
	return r.out, nil
}

func readFileT(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
