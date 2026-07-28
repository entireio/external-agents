package kilo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

func writeSession(t *testing.T, dir string, session Session) string {
	t.Helper()
	path := filepath.Join(dir, "session.json")
	data, err := encodeMessagesJSONL(session.Messages)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func makeTextMessage(id string, role MessageRole, text string) SessionMessage {
	return SessionMessage{
		Info: MessageInfo{ID: id, Role: role, ModelID: "claude-sonnet-4"},
		Parts: []MessagePart{
			{ID: id + "-p1", Type: PartText, Text: text},
		},
	}
}

func TestDecodeTranscriptEdgeCases(t *testing.T) {
	// The decoder is intentionally tolerant: whitespace, non-JSON, arrays, and
	// non-message objects yield no messages without error, so header-less scoped
	// slices and partially-corrupt transcripts decode cleanly.
	for _, in := range []string{"  ", "not json", "[1,2,3]", `{"foo":"bar"}`} {
		msgs, err := decodeTranscript([]byte(in))
		if err != nil {
			t.Fatalf("decodeTranscript(%q) error = %v", in, err)
		}
		if len(msgs) != 0 {
			t.Fatalf("decodeTranscript(%q) = %d messages, want 0", in, len(msgs))
		}
	}
}

func TestExtractPromptsAndSummary(t *testing.T) {
	dir := t.TempDir()
	session := Session{
		ID: "S-1",
		Messages: []SessionMessage{
			makeTextMessage("m1", MessageRoleUser, "hello"),
			makeTextMessage("m2", MessageRoleAssistant, "hi there"),
			makeTextMessage("m3", MessageRoleUser, "do a thing"),
			makeTextMessage("m4", MessageRoleAssistant, "thing done"),
		},
	}
	path := writeSession(t, dir, session)

	a := New()
	prompts, err := a.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts: %v", err)
	}
	if want := []string{"hello", "do a thing"}; !equalSlice(prompts, want) {
		t.Fatalf("prompts = %v, want %v", prompts, want)
	}

	prompts, err = a.ExtractPrompts(path, 2)
	if err != nil {
		t.Fatalf("ExtractPrompts offset: %v", err)
	}
	if want := []string{"do a thing"}; !equalSlice(prompts, want) {
		t.Fatalf("offset prompts = %v, want %v", prompts, want)
	}

	summary, ok, err := a.ExtractSummary(path)
	if err != nil {
		t.Fatalf("ExtractSummary: %v", err)
	}
	if !ok || summary != "thing done" {
		t.Fatalf("summary = (%q, %v), want (%q, true)", summary, ok, "thing done")
	}
}

func TestExtractModifiedFiles(t *testing.T) {
	dir := t.TempDir()
	toolInput := json.RawMessage(`{"filePath":"/tmp/repo/foo.go"}`)
	session := Session{
		ID: "S-1",
		Messages: []SessionMessage{
			{
				Info: MessageInfo{ID: "m1", Role: MessageRoleAssistant},
				Parts: []MessagePart{
					{
						Type:   PartTool,
						Tool:   "write",
						CallID: "c1",
						State: &ToolState{
							Status: "completed",
							Input:  toolInput,
							Output: "wrote",
						},
					},
					{
						Type:   PartTool,
						Tool:   "read",
						CallID: "c2",
						State: &ToolState{
							Status: "completed",
							Input:  json.RawMessage(`{"filePath":"/tmp/repo/bar.go"}`),
							Output: "read",
						},
					},
				},
			},
		},
	}
	path := writeSession(t, dir, session)

	a := New()
	files, pos, err := a.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles: %v", err)
	}
	if pos != 1 {
		t.Fatalf("position = %d, want 1", pos)
	}
	if want := []string{"/tmp/repo/foo.go"}; !equalSlice(files, want) {
		t.Fatalf("files = %v, want %v (read tool should be filtered out)", files, want)
	}
}

func TestGetTranscriptPosition(t *testing.T) {
	dir := t.TempDir()
	session := Session{
		ID: "S-1",
		Messages: []SessionMessage{
			makeTextMessage("m1", MessageRoleUser, "a"),
			makeTextMessage("m2", MessageRoleAssistant, "b"),
		},
	}
	path := writeSession(t, dir, session)

	a := New()
	pos, err := a.GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition: %v", err)
	}
	if pos != 2 {
		t.Fatalf("position = %d, want 2", pos)
	}

	// Missing file returns position 0
	pos, err = a.GetTranscriptPosition(filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("GetTranscriptPosition missing: %v", err)
	}
	if pos != 0 {
		t.Fatalf("missing position = %d, want 0", pos)
	}
}

func TestCalculateTokens(t *testing.T) {
	session := Session{
		ID: "S-1",
		Messages: []SessionMessage{
			{Info: MessageInfo{ID: "m1", Role: MessageRoleUser}},
			{
				Info: MessageInfo{
					ID:   "m2",
					Role: MessageRoleAssistant,
					Tokens: &Tokens{
						Input:  100,
						Output: 50,
						Cache:  &CacheTokens{Read: 10, Write: 5},
					},
				},
			},
			{
				Info: MessageInfo{
					ID:   "m3",
					Role: MessageRoleAssistant,
					Tokens: &Tokens{
						Input:  200,
						Output: 75,
						Cache:  &CacheTokens{Read: 20, Write: 0},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(session)

	a := New()
	usage, err := a.CalculateTokens(data, 0)
	if err != nil {
		t.Fatalf("CalculateTokens: %v", err)
	}
	want := protocol.TokenUsageResponse{
		InputTokens:         300,
		OutputTokens:        125,
		CacheReadTokens:     30,
		CacheCreationTokens: 5,
		APICallCount:        2,
	}
	if usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

func TestChunkAndReassemble(t *testing.T) {
	a := New()
	original := []byte("the quick brown fox jumps over the lazy dog")
	chunks, err := a.ChunkTranscript(original, 5)
	if err != nil {
		t.Fatalf("ChunkTranscript: %v", err)
	}
	reassembled, err := a.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatalf("ReassembleTranscript: %v", err)
	}
	if string(reassembled) != string(original) {
		t.Fatalf("reassembled = %q, want %q", reassembled, original)
	}

	if _, err := a.ChunkTranscript(original, 0); err == nil {
		t.Fatal("expected error for max-size 0")
	}
}

func TestReadSession(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	dir := t.TempDir()
	session := Session{
		ID:    "S-1",
		Title: "test",
		Time:  &SessionTime{Created: 1700000000000},
		Messages: []SessionMessage{
			makeTextMessage("m1", MessageRoleUser, "hello"),
			makeTextMessage("m2", MessageRoleAssistant, "world"),
		},
	}
	path := writeSession(t, dir, session)

	a := New()
	got, err := a.ReadSession(&protocol.HookInputJSON{SessionID: "S-1", SessionRef: path})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got.SessionID != "S-1" {
		t.Fatalf("SessionID = %q", got.SessionID)
	}
	if got.AgentName != "kilo" {
		t.Fatalf("AgentName = %q", got.AgentName)
	}
	if got.SessionRef != path {
		t.Fatalf("SessionRef = %q", got.SessionRef)
	}
	if len(got.NativeData) == 0 {
		t.Fatal("NativeData empty")
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
