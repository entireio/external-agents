package kilo

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
