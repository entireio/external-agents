package dsh

import (
	"path/filepath"
	"sort"
	"testing"
)

func analysisSession(t *testing.T) testSession {
	t.Helper()
	cwd := t.TempDir()
	events := []map[string]any{
		eventRecord(t, "turn/start", 0, 1700000001000, map[string]any{"turn": 1}),
		eventRecord(t, "user/message", 1, 1700000001100, userMessage(t, "u1", "first prompt", "user", "")),
		eventRecord(t, "user/message", 2, 1700000001200, userMessage(t, "u2", "injected context", "runtime-context", "snapshot")),
		eventRecord(t, "assistant/message", 3, 1700000001300, assistantMessage(t, "a1", "model-a", "working", []contentBlock{
			{Type: "reasoning", Text: "hidden"},
			{Type: "tool-call", ID: "call_w", Name: "write", Arguments: `{"file_path":"src/a.go","content":"x"}`},
		}, &tokenUsage{InputTokens: 100, OutputTokens: 10, CacheReadTokens: 5, CacheWriteTokens: 2}, 1, 1)),
		eventRecord(t, "tool/call", 4, 1700000001400, toolCallData{Turn: 1, Step: 1, CallID: "call_w", Name: "write", Arguments: `{"file_path":"src/a.go","content":"x"}`}),
		eventRecord(t, "tool/result", 5, 1700000001500, toolResultData{
			Turn: 1, Step: 1,
			Message: chatMessage{
				ID:         "r1",
				Role:       "tool",
				Content:    []contentBlock{{Type: "text", Text: "ok"}},
				Source:     messageSource{Kind: "tool", CallID: "call_w"},
				ToolCallID: "call_w",
			},
		}),
		eventRecord(t, "assistant/message", 6, 1700000001600, assistantMessage(t, "a2", "model-b", "", []contentBlock{
			{Type: "tool-call", ID: "call_e", Name: "edit", Arguments: `{"file_path":"/abs/b.go","old_string":"a","new_string":"b"}`},
			{Type: "tool-call", ID: "call_v", Name: "str_replace_editor", Arguments: `{"command":"view","path":"/abs/viewed.go"}`},
		}, &tokenUsage{InputTokens: 200, OutputTokens: 20, CacheReadTokens: 7, CacheWriteTokens: 3}, 1, 2)),
		eventRecord(t, "tool/call", 7, 1700000001700, toolCallData{Turn: 1, Step: 2, CallID: "call_e", Name: "edit", Arguments: `{"file_path":"/abs/b.go","old_string":"a","new_string":"b"}`}),
		eventRecord(t, "assistant/message", 8, 1700000001800, assistantMessage(t, "a3", "model-b", "", []contentBlock{
			{Type: "tool-call", ID: "call_p", Name: "apply_patch", Arguments: `{"input":"*** Begin Patch\n*** Add File: new.txt\n+x\n*** Delete File: gone.txt\n*** Update File: old.txt\n*** End Patch"}`},
		}, nil, 1, 3)),
		eventRecord(t, "tool/call", 9, 1700000001900, toolCallData{Turn: 1, Step: 3, CallID: "call_p", Name: "apply_patch", Arguments: `{"input":"*** Begin Patch\n*** Add File: new.txt\n+x\n*** Delete File: gone.txt\n*** Update File: old.txt\n*** End Patch"}`}),
		eventRecord(t, "user/message", 10, 1700000002000, userMessage(t, "u3", "second prompt", "user", "")),
		eventRecord(t, "assistant/message", 11, 1700000002100, assistantMessage(t, "a4", "model-b", "done", nil, &tokenUsage{InputTokens: 300, OutputTokens: 30}, 1, 4)),
	}
	session := newTestSession(t, "session-analysis", cwd, events)
	t.Setenv("ENTIRE_REPO_ROOT", cwd)
	if _, _, err := (&Agent{}).materializeTranscript(session.SessionID, cwd, "", materializeOptions{}); err != nil {
		t.Fatalf("materialize analysis session: %v", err)
	}
	return session
}

func TestExtractPrompts(t *testing.T) {
	session := analysisSession(t)
	agent := &Agent{}
	prompts, err := agent.ExtractPrompts(session.CacheRef, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts: %v", err)
	}
	want := []string{"first prompt", "second prompt"}
	if len(prompts) != len(want) || prompts[0] != want[0] || prompts[1] != want[1] {
		t.Errorf("prompts = %v, want %v", prompts, want)
	}

	prompts, err = agent.ExtractPrompts(session.CacheRef, 11)
	if err != nil {
		t.Fatalf("ExtractPrompts with offset: %v", err)
	}
	if len(prompts) != 1 || prompts[0] != "second prompt" {
		t.Errorf("offset prompts = %v, want [second prompt]", prompts)
	}
}

func TestExtractModifiedFiles(t *testing.T) {
	session := analysisSession(t)
	agent := &Agent{}
	files, position, err := agent.ExtractModifiedFiles(session.CacheRef, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles: %v", err)
	}
	if position != 13 {
		t.Errorf("position = %d, want 13", position)
	}
	want := []string{
		filepath.Join(session.CWD, "src", "a.go"),
		"/abs/b.go",
		filepath.Join(session.CWD, "new.txt"),
		filepath.Join(session.CWD, "gone.txt"),
		filepath.Join(session.CWD, "old.txt"),
	}
	got := append([]string(nil), files...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("files[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
	for _, file := range files {
		if file == "/abs/viewed.go" {
			t.Error("str_replace_editor view must not be reported as a modification")
		}
	}

	files, _, err = agent.ExtractModifiedFiles(session.CacheRef, 7)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles offset: %v", err)
	}
	for _, file := range files {
		if file == filepath.Join(session.CWD, "src", "a.go") {
			t.Error("offset should exclude the first mutating call")
		}
	}
}

func TestExtractFileLists(t *testing.T) {
	session := analysisSession(t)
	data, err := readTranscriptFile(session.CacheRef)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	modified, created, deleted := extractFileLists(data, session.CWD)
	if len(created) != 1 || created[0] != filepath.Join(session.CWD, "new.txt") {
		t.Errorf("created = %v", created)
	}
	if len(deleted) != 1 || deleted[0] != filepath.Join(session.CWD, "gone.txt") {
		t.Errorf("deleted = %v", deleted)
	}
	found := map[string]bool{}
	for _, file := range modified {
		found[file] = true
	}
	for _, want := range []string{
		filepath.Join(session.CWD, "src", "a.go"),
		"/abs/b.go",
		filepath.Join(session.CWD, "old.txt"),
	} {
		if !found[want] {
			t.Errorf("modified files missing %q: %v", want, modified)
		}
	}
}

func TestExtractSummary(t *testing.T) {
	session := analysisSession(t)
	summary, has, err := (&Agent{}).ExtractSummary(session.CacheRef)
	if err != nil {
		t.Fatalf("ExtractSummary: %v", err)
	}
	if !has || summary != "done" {
		t.Errorf("summary = %q, has = %v; want done, true", summary, has)
	}
}

func TestCalculateTokens(t *testing.T) {
	session := analysisSession(t)
	data, err := readTranscriptFile(session.CacheRef)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	agent := &Agent{}
	usage, err := agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatalf("CalculateTokens: %v", err)
	}
	if usage.InputTokens != 600 || usage.OutputTokens != 60 || usage.CacheReadTokens != 12 || usage.CacheCreationTokens != 5 {
		t.Errorf("usage = %+v", usage)
	}
	if usage.APICallCount != 3 {
		t.Errorf("api call count = %d, want 3", usage.APICallCount)
	}

	usage, err = agent.CalculateTokens(data, 7)
	if err != nil {
		t.Fatalf("CalculateTokens offset: %v", err)
	}
	if usage.InputTokens != 500 || usage.OutputTokens != 50 || usage.APICallCount != 2 {
		t.Errorf("offset usage = %+v", usage)
	}
}

func TestGetTranscriptPosition(t *testing.T) {
	session := analysisSession(t)
	agent := &Agent{}
	position, err := agent.GetTranscriptPosition(session.CacheRef)
	if err != nil {
		t.Fatalf("GetTranscriptPosition: %v", err)
	}
	if position != 13 {
		t.Errorf("position = %d, want 13", position)
	}
	position, err = agent.GetTranscriptPosition(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || position != 0 {
		t.Errorf("missing position = %d, %v; want 0, nil", position, err)
	}
}
