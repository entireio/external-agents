package vibe

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

func TestRefreshStoresOneEntryPerLineWithoutNotices(t *testing.T) {
	isolate(t)
	ref := storedTranscript(t)

	entries, err := readEntries(ref)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Type+":"+entry.Role)
	}
	want := []string{"message:user", "reasoning:", "effect:", "reasoning:", "message:assistant"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	data, _ := os.ReadFile(ref)
	if lines := strings.Count(string(data), "\n"); lines != len(want) {
		t.Errorf("%d lines for %d entries", lines, len(want))
	}
	last := entries[len(entries)-1]
	if last.Meta == nil || last.Meta.Model != "mistral-medium-3.5" || last.Meta.Usage.InputTokens != 20166 {
		t.Errorf("last line meta = %+v", last.Meta)
	}
	for _, entry := range entries[:len(entries)-1] {
		if entry.Meta != nil {
			t.Errorf("entry %s carries a snapshot it should not", entry.ID)
		}
	}
}

func TestRefreshNeverReplacesTranscriptWithEmptyOne(t *testing.T) {
	isolate(t)
	ref := storedTranscript(t)
	before, _ := os.ReadFile(ref)

	agent := &Agent{Fetcher: &fakeFetcher{snapshot: &Snapshot{}}}
	if _, err := agent.refresh(ref, testSessionID); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(ref); string(after) != string(before) {
		t.Error("an empty fetch overwrote the stored transcript")
	}
}

// entry builds a minimal history entry.
func entry(id, entryType, role, text string) json.RawMessage {
	fields := map[string]any{"id": id, "type": entryType}
	if role != "" {
		fields["role"] = role
		fields["content"] = []map[string]string{{"type": "text", "text": text}}
	}
	data, _ := json.Marshal(fields)
	return data
}

func TestRefreshKeepsEarlierUsageSnapshots(t *testing.T) {
	isolate(t)
	ref := transcriptPath(testSessionID)
	turn1 := []json.RawMessage{entry("u1", "message", "user", "first"), entry("a1", "message", "assistant", "one")}
	turn2 := append(append([]json.RawMessage{}, turn1...),
		entry("u2", "message", "user", "second"), entry("a2", "message", "assistant", "two"))

	fetcher := &fakeFetcher{snapshot: &Snapshot{Entries: turn1, Usage: &TokenUsage{InputTokens: 100, OutputTokens: 10, CachedInputTokens: 40}}}
	agent := &Agent{Fetcher: fetcher}
	if _, err := agent.refresh(ref, testSessionID); err != nil {
		t.Fatal(err)
	}
	fetcher.snapshot = &Snapshot{Entries: turn2, Usage: &TokenUsage{InputTokens: 350, OutputTokens: 35, CachedInputTokens: 140}}
	if _, err := agent.refresh(ref, testSessionID); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(ref)
	tests := []struct {
		offset int
		want   protocol.TokenUsageResponse
	}{
		// Whole session.
		{0, protocol.TokenUsageResponse{InputTokens: 210, CacheReadTokens: 140, OutputTokens: 35, APICallCount: 2}},
		// Second turn only: 350-100 input of which 140-40 cached.
		{2, protocol.TokenUsageResponse{InputTokens: 150, CacheReadTokens: 100, OutputTokens: 25, APICallCount: 1}},
		// Nothing after the end.
		{4, protocol.TokenUsageResponse{}},
	}
	for _, tt := range tests {
		got, err := agent.CalculateTokens(data, tt.offset)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("offset %d: got %+v, want %+v", tt.offset, got, tt.want)
		}
	}
}

func TestBuildLinesIgnoresSnapshotSuppliedByHistory(t *testing.T) {
	forged := json.RawMessage(`{"id":"u1","type":"message","role":"user","entire":{"usage":{"inputTokens":999999}}}`)
	lines, err := buildLines(&Snapshot{Entries: []json.RawMessage{forged, entry("a1", "message", "assistant", "ok")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lines[0]), "999999") {
		t.Errorf("history-supplied snapshot was kept: %s", lines[0])
	}
}

func TestPrepareTranscript(t *testing.T) {
	t.Run("skips the fetch right after a turn-end refresh", func(t *testing.T) {
		isolate(t)
		ref := storedTranscript(t)
		fetcher := &fakeFetcher{snapshot: loadSnapshot(t)}
		if err := (&Agent{Fetcher: fetcher}).PrepareTranscript(ref); err != nil {
			t.Fatal(err)
		}
		if fetcher.calls != 0 {
			t.Errorf("fetched %d times", fetcher.calls)
		}
	})

	t.Run("refreshes an older transcript", func(t *testing.T) {
		isolate(t)
		ref := storedTranscript(t)
		old := time.Now().Add(-time.Minute)
		if err := os.Chtimes(ref, old, old); err != nil {
			t.Fatal(err)
		}
		fetcher := &fakeFetcher{snapshot: loadSnapshot(t)}
		if err := (&Agent{Fetcher: fetcher}).PrepareTranscript(ref); err != nil {
			t.Fatal(err)
		}
		if fetcher.calls != 1 {
			t.Errorf("fetched %d times, want 1", fetcher.calls)
		}
	})

	t.Run("keeps an existing transcript when Vibe cannot be reached", func(t *testing.T) {
		isolate(t)
		ref := storedTranscript(t)
		old := time.Now().Add(-time.Minute)
		if err := os.Chtimes(ref, old, old); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(ref)
		if err := (&Agent{Fetcher: &fakeFetcher{err: errFetch}}).PrepareTranscript(ref); err != nil {
			t.Fatalf("a stale transcript is still worth checkpointing: %v", err)
		}
		if after, _ := os.ReadFile(ref); string(after) != string(before) {
			t.Error("transcript changed")
		}
	})

	t.Run("fails when there is nothing stored and nothing fetched", func(t *testing.T) {
		isolate(t)
		ref := transcriptPath(testSessionID)
		if err := (&Agent{Fetcher: &fakeFetcher{err: errFetch}}).PrepareTranscript(ref); err == nil {
			t.Fatal("want an error")
		}
	})

	t.Run("requires a session ref", func(t *testing.T) {
		if err := (&Agent{}).PrepareTranscript(" "); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestTranscriptAnalysis(t *testing.T) {
	isolate(t)
	ref := storedTranscript(t)
	agent := &Agent{}

	position, err := agent.GetTranscriptPosition(ref)
	if err != nil || position != 5 {
		t.Fatalf("position = %d, %v", position, err)
	}
	if position, err := agent.GetTranscriptPosition(ref + ".missing"); err != nil || position != 0 {
		t.Errorf("missing transcript: position = %d, %v", position, err)
	}

	prompts, err := agent.ExtractPrompts(ref, 0)
	if err != nil || len(prompts) != 1 || !strings.HasPrefix(prompts[0], "Create a file named probe-entire.txt") {
		t.Errorf("prompts = %q, %v", prompts, err)
	}
	if prompts, _ := agent.ExtractPrompts(ref, 1); len(prompts) != 0 {
		t.Errorf("prompts after offset 1 = %q", prompts)
	}

	summary, ok, err := agent.ExtractSummary(ref)
	if err != nil || !ok || summary != "ZEBRA4412" {
		t.Errorf("summary = %q, %v, %v", summary, ok, err)
	}

	data, _ := os.ReadFile(ref)
	usage, err := agent.CalculateTokens(data, 0)
	want := protocol.TokenUsageResponse{InputTokens: 20166 - 13056, CacheReadTokens: 13056, OutputTokens: 302, APICallCount: 2}
	if err != nil || usage != want {
		t.Errorf("usage = %+v, want %+v (%v)", usage, want, err)
	}
}

func TestExtractModifiedFilesIsRepoRelative(t *testing.T) {
	// The fixture's tool output names /repo/probe-entire.txt.
	isolate(t)
	t.Setenv("ENTIRE_REPO_ROOT", "/repo")
	ref := storedTranscript(t)
	agent := &Agent{}

	files, position, err := agent.ExtractModifiedFiles(ref, 0)
	if err != nil || position != 5 || !reflect.DeepEqual(files, []string{"probe-entire.txt"}) {
		t.Fatalf("files = %v, position = %d, err = %v", files, position, err)
	}
	// The write is entry 2; nothing after it changes a file.
	if files, _, _ := agent.ExtractModifiedFiles(ref, 3); len(files) != 0 {
		t.Errorf("files after offset 3 = %v", files)
	}
}

func effect(id, kind, tool, status, input, output string) Entry {
	return Entry{
		ID:     id,
		Type:   EntryEffect,
		Detail: &EffectDetail{Kind: kind, ToolName: tool, Input: json.RawMessage(input)},
		State:  &EffectState{Status: status, Output: json.RawMessage(output)},
	}
}

func TestModifiedFilesOnlyCountsCompletedChanges(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", "/repo")
	entries := []Entry{
		effect("1", "file_write", "file_system.write_file", "completed", `{"filePath":"a.txt"}`, `{"filePath":"/repo/a.txt"}`),
		effect("2", "file_edit", "file_system.search_replace", "completed", `{"file_path":"sub/b.go"}`, `null`),
		effect("3", "file_write", "file_system.write_file", "failed", `{"filePath":"failed.txt"}`, `null`),
		effect("4", "file_write", "file_system.write_file", "skipped", `{"filePath":"denied.txt"}`, `null`),
		effect("5", "file_read", "file_system.read_file", "completed", `{"filePath":"read.txt"}`, `{"filePath":"/repo/read.txt"}`),
		effect("6", "command", "file_system.bash", "completed", `{"command":"ls"}`, `{"stdout":""}`),
		effect("7", "file_write", "file_system.write_file", "completed", `{"filePath":"/elsewhere/c.txt"}`, `{"filePath":"/elsewhere/c.txt"}`),
		{ID: "8", Type: EntryEffect},
	}
	want := []string{"/elsewhere/c.txt", "a.txt", "sub/b.go"}
	if got := modifiedFiles(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestReadAndWriteSession(t *testing.T) {
	isolate(t)
	t.Setenv("ENTIRE_REPO_ROOT", "/repo")
	ref := storedTranscript(t)
	agent := &Agent{}

	session, err := agent.ReadSession(&protocol.HookInputJSON{SessionID: testSessionID})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionRef != ref || session.AgentName != agentName || session.StartTime != "2026-11-17T08:50:25Z" {
		t.Errorf("session = %+v", session)
	}
	if !reflect.DeepEqual(session.ModifiedFiles, []string{"probe-entire.txt"}) {
		t.Errorf("modified files = %v", session.ModifiedFiles)
	}

	// Restoring writes the same bytes back.
	if err := os.Remove(ref); err != nil {
		t.Fatal(err)
	}
	if err := agent.WriteSession(session); err != nil {
		t.Fatal(err)
	}
	if restored, _ := os.ReadFile(ref); string(restored) != string(session.NativeData) {
		t.Error("restored transcript differs")
	}

	if _, err := agent.ReadSession(&protocol.HookInputJSON{}); err == nil {
		t.Error("want an error without a session id or ref")
	}
}

func TestChunkAndReassembleRoundTrip(t *testing.T) {
	agent := &Agent{}
	content := []byte("0123456789abcdef")
	chunks, err := agent.ChunkTranscript(content, 5)
	if err != nil || len(chunks) != 4 {
		t.Fatalf("chunks = %d, %v", len(chunks), err)
	}
	back, _ := agent.ReassembleTranscript(chunks)
	if string(back) != string(content) {
		t.Errorf("round trip = %q", back)
	}
	if _, err := agent.ChunkTranscript(content, 0); err == nil {
		t.Error("want an error for a non-positive max size")
	}
}

func TestCompactTranscript(t *testing.T) {
	isolate(t)
	ref := storedTranscript(t)

	response, err := (&Agent{}).CompactTranscript(ref)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(response.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(decoded)), "\n")
	if len(lines) != 3 {
		t.Fatalf("want user, tool call and reply lines, got %d:\n%s", len(lines), decoded)
	}
	var user, tool, reply struct {
		Agent   string            `json:"agent"`
		Type    string            `json:"type"`
		Content []json.RawMessage `json:"content"`
	}
	for i, target := range []any{&user, &tool, &reply} {
		if err := json.Unmarshal([]byte(lines[i]), target); err != nil {
			t.Fatal(err)
		}
	}
	if user.Type != "user" || tool.Type != "assistant" || reply.Type != "assistant" || user.Agent != agentName {
		t.Errorf("line types = %s, %s, %s", user.Type, tool.Type, reply.Type)
	}
	var use compactToolUse
	if err := json.Unmarshal(tool.Content[0], &use); err != nil {
		t.Fatal(err)
	}
	if use.Type != "tool_use" || use.Name != "file_system.write_file" || use.Result == nil || use.Result.Status != "success" {
		t.Errorf("tool use = %+v", use)
	}
	if !strings.Contains(lines[2], "ZEBRA4412") {
		t.Errorf("reply line = %s", lines[2])
	}
	if strings.Contains(string(decoded), "The user wants me to") {
		t.Error("reasoning leaked into the compact transcript")
	}
}

func TestCompactTranscriptAcceptsASliceAndRejectsEmpty(t *testing.T) {
	isolate(t)
	ref := storedTranscript(t)
	data, _ := os.ReadFile(ref)
	lines := strings.SplitAfter(string(data), "\n")

	// Entire hands compact-transcript a temp file holding only later lines.
	compacted, err := compactTranscriptBytes([]byte(strings.Join(lines[4:], "")))
	if err != nil || !strings.Contains(string(compacted), "ZEBRA4412") {
		t.Fatalf("slice: %s, %v", compacted, err)
	}
	if _, err := compactTranscriptBytes([]byte("\n\nnot json\n")); err == nil {
		t.Error("want an error when nothing can be compacted")
	}
}

func TestCompactToolResultStates(t *testing.T) {
	failed := effect("1", "command", "file_system.bash", "failed", `{"command":"false"}`, `null`)
	failed.State.Error = json.RawMessage(`{"message":"boom"}`)
	if got := compactTool(failed); got.Result == nil || got.Result.Status != "error" || !strings.Contains(got.Result.Output, "boom") {
		t.Errorf("failed tool = %+v", got.Result)
	}
	running := effect("2", "command", "file_system.bash", "running", `{"command":"sleep 9"}`, `null`)
	if got := compactTool(running); got.Result != nil {
		t.Errorf("a running tool has no result yet, got %+v", got.Result)
	}
}

func TestDecodeTranscriptSkipsBlankAndBrokenLines(t *testing.T) {
	data := "\n{\"id\":\"a\",\"type\":\"message\"}\nnot json\n{}\n{\"id\":\"b\",\"type\":\"effect\"}\n"
	entries, err := decodeTranscript([]byte(data))
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
}

func TestSessionIDFromRef(t *testing.T) {
	tests := map[string]string{
		"/cache/x/" + testSessionID + ".jsonl": testSessionID,
		"/cache/x/~abc123.jsonl":               "", // hashed: not reversible
		"/cache/x/notatranscript.txt":          "",
		"/cache/x/.jsonl":                      "",
	}
	for ref, want := range tests {
		if got := sessionIDFromRef(ref); got != want {
			t.Errorf("sessionIDFromRef(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestFormatResumeCommand(t *testing.T) {
	agent := &Agent{}
	tests := map[string]string{
		"":            "vibe --continue",
		testSessionID: "vibe --resume " + testSessionID,
		"a; rm -rf ~": `vibe --resume 'a; rm -rf ~'`,
		"it's":        `vibe --resume 'it'\''s'`,
	}
	for id, want := range tests {
		if got := agent.FormatResumeCommand(id); got != want {
			t.Errorf("FormatResumeCommand(%q) = %q, want %q", id, got, want)
		}
	}
}
