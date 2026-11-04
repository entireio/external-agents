package kilo

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

// sliceFromLine mirrors Entire's transcript.SliceFromLine: return the bytes
// starting after the Nth newline (empty if there aren't that many lines).
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
	msgs := []SessionMessage{
		makeTextMessage("m1", MessageRoleUser, "first prompt"),
		makeTextMessage("m2", MessageRoleAssistant, "first answer"),
		makeTextMessage("m3", MessageRoleUser, "second prompt"),
		makeTextMessage("m4", MessageRoleAssistant, "second answer"),
	}
	data, err := encodeMessagesJSONL(msgs)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
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
	if len(msgs) != 4 || msgs[0].Info.ID != "m1" || msgs[3].Info.ID != "m4" {
		t.Fatalf("round-trip messages = %+v", msgs)
	}
}

// The regression this whole change exists for: Entire scopes checkpoint
// transcripts by line offset before calling compact-transcript. With
// one-message-per-line JSONL a mid-session slice is still valid and compacts;
// the old single-blob layout produced empty/invalid bytes and summaries failed.
func TestCompactToleratesLineScopedSlice(t *testing.T) {
	full := fourMessageTranscript(t)

	// Offset 2 == start of the second turn (messages m3, m4).
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
}

func TestGetTranscriptPositionMatchesLineCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
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

func TestTurnEndWritesJSONL(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)

	messages := json.RawMessage(`[` +
		`{"info":{"id":"m1","role":"user","sessionID":"S-1"},"parts":[{"type":"text","text":"hi"}]},` +
		`{"info":{"id":"m2","role":"assistant","modelID":"claude"},"parts":[{"type":"text","text":"yo"}]}` +
		`]`)
	body, _ := json.Marshal(turnEndRaw{
		SessionID: "S-1",
		Session:   json.RawMessage(`{"id":"S-1"}`),
		Messages:  messages,
	})

	event, err := New().ParseHook(HookNameTurnEnd, body)
	if err != nil {
		t.Fatalf("ParseHook: %v", err)
	}
	data, err := os.ReadFile(event.SessionRef)
	if err != nil {
		t.Fatalf("read session ref: %v", err)
	}
	// Exactly one line per message, each independently valid JSON.
	if lines := bytes.Count(data, []byte{'\n'}); lines != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d: %s", lines, data)
	}
	for line := range bytes.SplitSeq(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		var msg SessionMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			t.Fatalf("line not valid message JSON: %v (%s)", err, line)
		}
	}
	// Model recovered from the assistant message when the payload omits it.
	if event.Model != "claude" {
		t.Fatalf("model = %q, want claude", event.Model)
	}
}

func TestWriteSessionPayloadKeepsTranscriptOnEmpty(t *testing.T) {
	dir := t.TempDir()
	ref := filepath.Join(dir, "s.json")
	orig, err := encodeMessagesJSONL([]SessionMessage{makeTextMessage("m1", MessageRoleUser, "keep me")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	// A failed plugin snapshot sends messages:[]; it must NOT clobber the
	// existing transcript.
	if err := writeSessionPayload(ref, nil, json.RawMessage(`[]`)); err != nil {
		t.Fatalf("writeSessionPayload: %v", err)
	}
	got, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(orig) {
		t.Fatalf("transcript clobbered by empty payload: %q", got)
	}
}

func TestReadSessionRecoversIDFromMessages(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	data, err := encodeMessagesJSONL([]SessionMessage{
		{Info: MessageInfo{ID: "m1", Role: MessageRoleUser, SessionID: "S-42"}, Parts: []MessagePart{{Type: PartText, Text: "hi"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// No SessionID in the input: it must be recovered from the message info.
	got, err := New().ReadSession(&protocol.HookInputJSON{SessionRef: path})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got.SessionID != "S-42" {
		t.Fatalf("SessionID = %q, want S-42", got.SessionID)
	}
}

// compactKind mirrors normalizeKind in Entire's
// cmd/entire/cli/transcript/compact/compact.go:197 — the generic JSONL reader
// used for any agent Entire does not special-case, Kilo included. It takes the
// entry kind from the TOP-LEVEL "type", falling back to "role"; a line with
// neither is dropped outright.
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
// from a TOP-LEVEL "message" object, never from the line itself. A line that
// passes compactKind but has no "message" compacts to an empty content array.
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

// The regression PR #47 did not cover: the stored lines were valid JSONL and
// sliced correctly, but carried neither of the two things Entire's generic
// compactJSONL needs, so transcript.jsonl came out 0 bytes. Both halves are
// asserted here because either one alone still loses the content.
func TestStoredLinesSatisfyEntireCompactContract(t *testing.T) {
	data := fourMessageTranscript(t)
	lines := decodeLines(t, data)
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
		var text string
		if err := json.Unmarshal(blocks[0]["text"], &text); err != nil || text != wantText[i] {
			t.Fatalf("line %d: message content text = %q (err %v), want %q", i, text, err, wantText[i])
		}
	}
}

// A mid-session slice must keep satisfying the contract: Entire compacts the
// scoped bytes, not the whole file.
func TestScopedSliceSatisfiesEntireCompactContract(t *testing.T) {
	scoped := sliceFromLine(fourMessageTranscript(t), 2)
	lines := decodeLines(t, scoped)
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

// An assistant tool call becomes a tool_use block, the only tool shape
// stripAssistantContent (compact.go:704) keeps.
func TestToolPartProjectsToToolUseBlock(t *testing.T) {
	msg := SessionMessage{
		Info: MessageInfo{ID: "m1", Role: MessageRoleAssistant},
		Parts: []MessagePart{{
			Type:   PartTool,
			Tool:   "edit_file",
			CallID: "call-1",
			State:  &ToolState{Input: json.RawMessage(`{"path":"hello.txt"}`), Output: "ok"},
		}},
	}
	data, err := encodeMessagesJSONL([]SessionMessage{msg})
	if err != nil {
		t.Fatal(err)
	}
	blocks := compactMessageContent(decodeLines(t, data)[0])
	if len(blocks) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(blocks))
	}
	for key, want := range map[string]string{"type": "tool_use", "id": "call-1", "name": "edit_file"} {
		var got string
		if err := json.Unmarshal(blocks[0][key], &got); err != nil || got != want {
			t.Fatalf("tool block %q = %q (err %v), want %q", key, got, err, want)
		}
	}
	if got := string(blocks[0]["input"]); got != `{"path":"hello.txt"}` {
		t.Fatalf("tool block input = %s", got)
	}
}

// Usage must use Entire's snake_case names (compact.go:517) or the token
// counts silently read as zero.
func TestUsageProjectsToSnakeCaseTokens(t *testing.T) {
	data, err := encodeMessagesJSONL([]SessionMessage{{
		Info: MessageInfo{
			ID:     "m1",
			Role:   MessageRoleAssistant,
			Tokens: &Tokens{Input: 100, Output: 25},
		},
		Parts: []MessagePart{{Type: PartText, Text: "hi"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var msg struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(decodeLines(t, data)[0]["message"], &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Usage.InputTokens != 100 || msg.Usage.OutputTokens != 25 {
		t.Fatalf("usage = %+v, want 100/25", msg.Usage)
	}
}

// Back-compat: a transcript written before the projection existed carries only
// info/parts. It must still decode, and re-encoding must self-heal it.
func TestLegacyLinesWithoutProjectionStillDecode(t *testing.T) {
	legacy := []byte(`{"info":{"id":"m1","role":"user"},"parts":[{"type":"text","text":"old line"}]}` + "\n")
	msgs, err := decodeTranscript(legacy)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Info.ID != "m1" || msgs[0].Parts[0].Text != "old line" {
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
// message unchanged, so a round-trip is lossless.
func TestProjectionIsIgnoredOnDecode(t *testing.T) {
	in := []SessionMessage{
		makeTextMessage("m1", MessageRoleUser, "first prompt"),
		makeTextMessage("m2", MessageRoleAssistant, "first answer"),
	}
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
