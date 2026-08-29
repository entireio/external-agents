package amp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

// compactKind mirrors normalizeKind in Entire's
// cmd/entire/cli/transcript/compact/compact.go:197: the entry kind comes from
// the TOP-LEVEL "type", falling back to "role". Amp's message already carries
// "role", so this half held once the transcript became JSONL.
func compactKind(line map[string]json.RawMessage) string {
	var kind string
	if json.Unmarshal(line["type"], &kind) == nil && kind != "" {
		return kind
	}
	if json.Unmarshal(line["role"], &kind) == nil {
		return kind
	}
	return ""
}

// compactMessageContent mirrors parseMessage (compact.go:617): content is read
// from a TOP-LEVEL "message" object, never from the line's own "content" array.
// This is the half amp was missing — lines were kept, and every one of them
// compacted to an empty content array.
func compactMessageContent(line map[string]json.RawMessage) []map[string]json.RawMessage {
	var msg map[string]json.RawMessage
	if json.Unmarshal(line["message"], &msg) != nil {
		return nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(msg["content"], &blocks) != nil {
		return nil
	}
	return blocks
}

func decodeLines(t *testing.T, data []byte) []map[string]json.RawMessage {
	t.Helper()
	var out []map[string]json.RawMessage
	for line := range bytes.SplitSeq(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("line is not valid JSON: %v (%s)", err, line)
		}
		out = append(out, m)
	}
	return out
}

// preparedLines materializes the native export through the real write path
// (materializeThread -> encodeMessagesJSONL) and returns the stored lines, so
// these assertions exercise the encoder rather than a checked-in constant.
func preparedLines(t *testing.T) []map[string]json.RawMessage {
	t.Helper()
	export, ok := legacyThreadExport([]byte(testPreparedTranscriptJSON))
	if !ok {
		t.Fatal("testPreparedTranscriptJSON is not a thread export")
	}
	data, err := materializeThread(export)
	if err != nil {
		t.Fatalf("materializeThread: %v", err)
	}
	return decodeLines(t, data)
}

func blockString(t *testing.T, block map[string]json.RawMessage, key string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(block[key], &s); err != nil {
		t.Fatalf("block[%q] = %s: %v", key, block[key], err)
	}
	return s
}

// Storing JSONL made Entire keep the lines; it did not make them carry
// anything. Both halves of the contract are asserted because satisfying only
// the first yields a correctly-sized transcript.jsonl full of empty content.
func TestStoredLinesSatisfyEntireCompactContract(t *testing.T) {
	lines := decodeLines(t, fourMessageTranscript(t))
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}

	wantKind := []string{"user", "assistant", "user", "assistant"}
	wantText := []string{"first prompt", "first answer", "second prompt", "second answer"}

	for i, line := range lines {
		if got := compactKind(line); got != wantKind[i] {
			t.Fatalf("line %d: compactKind = %q, want %q — Entire drops this line", i, got, wantKind[i])
		}
		blocks := compactMessageContent(line)
		if len(blocks) == 0 {
			t.Fatalf("line %d: no content under the \"message\" wrapper — Entire emits an empty line", i)
		}
		if got := blockString(t, blocks[0], "text"); got != wantText[i] {
			t.Fatalf("line %d: message content text = %q, want %q", i, got, wantText[i])
		}
	}
}

// A mid-session slice must keep satisfying the contract: Entire compacts the
// scoped bytes, not the whole file.
func TestScopedSliceSatisfiesEntireCompactContract(t *testing.T) {
	lines := decodeLines(t, sliceFromLine(fourMessageTranscript(t), 2))
	if len(lines) != 2 {
		t.Fatalf("scoped slice has %d lines, want 2", len(lines))
	}
	for i, line := range lines {
		if compactKind(line) == "" {
			t.Fatalf("scoped line %d has no top-level type/role", i)
		}
		if len(compactMessageContent(line)) == 0 {
			t.Fatalf("scoped line %d carries no message content", i)
		}
	}
}

// Tool calls and their results must land in the shapes Entire matches on:
// tool_use keeps type/id/name/input (compact.go:704), and a user tool_result
// uses snake_case tool_use_id with a string "content" (compact.go:665) so
// Entire can inline the output into the preceding assistant's tool_use.
func TestToolBlocksProjectToEntireShapes(t *testing.T) {
	lines := preparedLines(t)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	toolUse := compactMessageContent(lines[1])[0]
	for key, want := range map[string]string{"type": "tool_use", "id": "tool1", "name": "edit_file"} {
		if got := blockString(t, toolUse, key); got != want {
			t.Fatalf("tool_use %q = %q, want %q", key, got, want)
		}
	}
	if got := string(toolUse["input"]); got != `{"path":"hello.txt"}` {
		t.Fatalf("tool_use input = %s", got)
	}

	toolResult := compactMessageContent(lines[2])[0]
	for key, want := range map[string]string{"type": "tool_result", "tool_use_id": "tool1", "content": "ok"} {
		if got := blockString(t, toolResult, key); got != want {
			t.Fatalf("tool_result %q = %q, want %q", key, got, want)
		}
	}
}

// Usage must use Entire's snake_case names (compact.go:517). Amp's native
// camelCase inputTokens/outputTokens read as zero there.
func TestUsageProjectsToSnakeCaseTokens(t *testing.T) {
	lines := preparedLines(t)
	var msg struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(lines[1]["message"], &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Usage.InputTokens != 100 || msg.Usage.OutputTokens != 25 {
		t.Fatalf("usage = %+v, want 100/25", msg.Usage)
	}
}

// Back-compat: a JSONL transcript written before the projection existed carries
// only the native fields. It must still decode, and re-encoding self-heals it.
func TestLegacyJSONLWithoutProjectionStillDecodes(t *testing.T) {
	legacy := []byte(`{"threadID":"T-9","role":"user","content":[{"type":"text","text":"old line"}],"messageId":1}` + "\n")
	msgs, err := decodeTranscript(legacy)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ThreadID != "T-9" || msgs[0].Content[0].Text != "old line" {
		t.Fatalf("legacy decode = %+v", msgs)
	}

	healed, err := encodeMessagesJSONL(msgs)
	if err != nil {
		t.Fatal(err)
	}
	line := decodeLines(t, healed)[0]
	if compactKind(line) != "user" || len(compactMessageContent(line)) == 0 {
		t.Fatalf("re-encoded legacy line did not self-heal: %s", healed)
	}
}

// The projection is derived data: decoding ignores it and yields the native
// message unchanged, so a round-trip through the stored layout is lossless.
func TestProjectionIsIgnoredOnDecode(t *testing.T) {
	in := stampThreadID([]ThreadMessage{
		makeTextMessage("1", ThreadMessageRoleUser, "first prompt"),
		makeTextMessage("2", ThreadMessageRoleAssistant, "first answer"),
	}, "T-4")
	data, err := encodeMessagesJSONL(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := decodeTranscript(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip lost data:\n in = %+v\nout = %+v", in, out)
	}
}

// An info message has no compact representation, so it stays unprojected and
// Entire drops it — same as compactTranscriptBytes does.
func TestInfoMessagesAreNotProjected(t *testing.T) {
	data, err := encodeMessagesJSONL([]ThreadMessage{
		makeTextMessage("1", ThreadMessageRoleInfo, "connected"),
	})
	if err != nil {
		t.Fatal(err)
	}
	line := decodeLines(t, data)[0]
	if _, ok := line["message"]; ok {
		t.Fatalf("info message was projected: %s", data)
	}
	if kind := compactKind(line); kind != "info" {
		t.Fatalf("compactKind = %q, want info (which Entire drops)", kind)
	}
}
