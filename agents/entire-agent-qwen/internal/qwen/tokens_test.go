package qwen

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-qwen/internal/protocol"
)

// qwenSession drives hooks for one session against a native chat log the test
// writes, the way Qwen Code writes both.
type qwenSession struct {
	t          *testing.T
	agent      *Agent
	id         string
	nativePath string
}

func newQwenSession(t *testing.T) *qwenSession {
	t.Helper()
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	id := "6f1c2d3e-0000-4000-8000-000000000075"
	native := filepath.Join(t.TempDir(), ".qwen", "projects", "-repo", "chats", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(native), 0o700); err != nil {
		t.Fatal(err)
	}
	return &qwenSession{t: t, agent: New(), id: id, nativePath: native}
}

// hook runs one hook with the fields every Qwen hook payload carries.
func (s *qwenSession) hook(name, timestamp string, extra map[string]any) {
	s.t.Helper()
	payload := map[string]any{
		"session_id":      s.id,
		"transcript_path": s.nativePath,
		"cwd":             "/repo",
		"timestamp":       timestamp,
	}
	for key, value := range extra {
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.agent.ParseHook(name, data); err != nil {
		s.t.Fatalf("ParseHook(%s): %v", name, err)
	}
}

// writeNative appends raw text to the native log; a line without its newline
// is a record Qwen is still writing.
func (s *qwenSession) writeNative(text string) {
	s.t.Helper()
	if err := appendFile(s.nativePath, text); err != nil {
		s.t.Fatal(err)
	}
}

func appendFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(text)
	return errors.Join(err, f.Close())
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (s *qwenSession) sidecar() []byte {
	s.t.Helper()
	data, err := os.ReadFile(s.agent.sidecarPath(s.id))
	if err != nil {
		s.t.Fatal(err)
	}
	return data
}

// position is what Entire stores as a turn's start: get-transcript-position.
func (s *qwenSession) position() int {
	s.t.Helper()
	pos, err := s.agent.GetTranscriptPosition(s.agent.sidecarPath(s.id))
	if err != nil {
		s.t.Fatal(err)
	}
	return pos
}

func (s *qwenSession) tokens(offset int) protocol.TokenUsageResponse {
	s.t.Helper()
	usage, err := s.agent.CalculateTokens(s.sidecar(), offset)
	if err != nil {
		s.t.Fatal(err)
	}
	return usage
}

// nativeLine renders a Qwen chat-log record as ChatRecordingService writes it.
func nativeLine(uuid, recordType, timestamp, usage string) string {
	record := fmt.Sprintf(`{"uuid":%q,"parentUuid":null,"sessionId":"6f1c2d3e-0000-4000-8000-000000000075",`+
		`"timestamp":%q,"type":%q,"cwd":"/repo","version":"0.26.9","gitBranch":"main"`, uuid, timestamp, recordType)
	switch recordType {
	case "assistant":
		record += `,"provenance":"assistant_output","model":"qwen3-coder-plus","message":{"role":"model","parts":[{"text":"done"}]}`
		if usage != "" {
			record += `,"usageMetadata":` + usage
		}
		record += `,"contextWindowSize":1000000`
	case "user":
		record += `,"provenance":"real_user","message":{"role":"user","parts":[{"text":"go"}]}`
	default:
		record += `,"provenance":"tool_result","message":{"role":"user","parts":[{"functionResponse":{"name":"write_file","response":{"output":"ok"}}}]}`
	}
	return record + "}\n"
}

// openAIUsage is what Qwen records from an OpenAI-compatible provider:
// prompt_tokens includes the cached share and completion_tokens the reasoning.
func openAIUsage(prompt, completion, cached, reasoning int) string {
	return fmt.Sprintf(`{"promptTokenCount":%d,"candidatesTokenCount":%d,"thoughtsTokenCount":%d,"totalTokenCount":%d,"cachedContentTokenCount":%d}`,
		prompt, completion, reasoning, prompt+completion, cached)
}

func wantUsage(t *testing.T, got protocol.TokenUsageResponse, want protocol.TokenUsageResponse) {
	t.Helper()
	if got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

// The regression in #75: a Qwen session reported no token usage at all. Each
// assistant record's usage is copied at the hooks and summed per turn from the
// turn's start position.
func TestTurnTokenUsageComesFromNativeChatLog(t *testing.T) {
	s := newQwenSession(t)
	s.hook(HookNameSessionStart, "2026-11-06T10:00:00.000Z", map[string]any{"source": "startup"})

	turnStart := s.position()
	s.hook(HookNameUserPromptSubmit, "2026-11-06T10:00:01.000Z", map[string]any{"prompt": "create hello.txt"})
	s.writeNative(nativeLine("u-1", "user", "2026-11-06T10:00:01.100Z", ""))
	s.writeNative(nativeLine("a-1", "assistant", "2026-11-06T10:00:03.000Z", openAIUsage(1200, 80, 1000, 20)))
	s.hook(HookNamePreToolUse, "2026-11-06T10:00:03.100Z", map[string]any{"tool_name": "write_file", "tool_use_id": "call-1", "tool_input": map[string]any{"file_path": "hello.txt"}})
	s.hook(HookNamePostToolUse, "2026-11-06T10:00:03.200Z", map[string]any{"tool_name": "write_file", "tool_use_id": "call-1", "tool_input": map[string]any{"file_path": "hello.txt"}, "tool_response": "ok"})
	s.writeNative(nativeLine("t-1", "tool_result", "2026-11-06T10:00:03.300Z", ""))
	s.writeNative(nativeLine("a-2", "assistant", "2026-11-06T10:00:05.000Z", openAIUsage(1400, 30, 1200, 0)))
	s.hook(HookNameStop, "2026-11-06T10:00:05.100Z", map[string]any{"last_assistant_message": "Created hello.txt"})

	wantUsage(t, s.tokens(turnStart), protocol.TokenUsageResponse{
		InputTokens:     (1200 - 1000) + (1400 - 1200),
		CacheReadTokens: 1000 + 1200,
		OutputTokens:    80 + 30,
		APICallCount:    2,
	})

	// The copies are sidecar lines Entire's compactor drops (no top-level
	// type), so the compact transcript and the summary are unchanged.
	var copies int
	for _, line := range decodeLines(t, s.sidecar()) {
		var event string
		_ = json.Unmarshal(line["event"], &event)
		if event != eventAssistantUsage {
			continue
		}
		copies++
		if kind := compactKind(line); kind != "" {
			t.Fatalf("AssistantUsage line has compact kind %q; Entire would render it", kind)
		}
	}
	if copies != 2 {
		t.Fatalf("sidecar holds %d AssistantUsage records, want 2", copies)
	}
	summary, ok, err := s.agent.ExtractSummary(s.agent.sidecarPath(s.id))
	if err != nil || !ok || summary != "Created hello.txt" {
		t.Fatalf("summary = %q, %v, %v", summary, ok, err)
	}
	if _, err := compactTranscriptBytes(s.sidecar()); err != nil {
		t.Fatalf("compact transcript: %v", err)
	}
}

// Qwen fires Stop without waiting for its write queue, so the turn's final
// record can still be on its way. While the log ends with a record the model
// has yet to answer, Stop waits for it; the next test covers that. Here the
// log already ends with an assistant record, so Stop has nothing to wait for,
// and the late call is copied at the next sync and counted with the next turn.
// The session total is unaffected.
func TestUsageWrittenAfterStopIsCountedWithNextTurn(t *testing.T) {
	s := newQwenSession(t)
	s.hook(HookNameSessionStart, "2026-11-06T10:00:00Z", nil)

	turn1 := s.position()
	s.hook(HookNameUserPromptSubmit, "2026-11-06T10:00:01Z", map[string]any{"prompt": "one"})
	s.writeNative(nativeLine("a-1", "assistant", "2026-11-06T10:00:02Z", openAIUsage(100, 10, 0, 0)))
	s.hook(HookNameStop, "2026-11-06T10:00:03Z", map[string]any{"last_assistant_message": "one done"})
	// The final record of turn 1 reaches the log only now, and the hook it
	// missed is not run again.
	s.writeNative(nativeLine("a-2", "assistant", "2026-11-06T10:00:03.5Z", openAIUsage(200, 20, 0, 0)))
	wantUsage(t, s.tokens(turn1), protocol.TokenUsageResponse{InputTokens: 100, OutputTokens: 10, APICallCount: 1})

	turn2 := s.position()
	s.hook(HookNameUserPromptSubmit, "2026-11-06T10:01:00Z", map[string]any{"prompt": "two"})
	s.writeNative(nativeLine("a-3", "assistant", "2026-11-06T10:01:02Z", openAIUsage(300, 30, 0, 0)))
	s.hook(HookNameStop, "2026-11-06T10:01:03Z", map[string]any{"last_assistant_message": "two done"})

	wantUsage(t, s.tokens(turn2), protocol.TokenUsageResponse{InputTokens: 500, OutputTokens: 50, APICallCount: 2})
	wantUsage(t, s.tokens(0), protocol.TokenUsageResponse{InputTokens: 600, OutputTokens: 60, APICallCount: 3})
}

// Qwen queues the turn's final record before it fires Stop, but the write can
// still be in flight when the hook reads the log. Stop waits for it, so the
// turn that made the call counts it.
func TestStopWaitsForTheTurnsFinalRecord(t *testing.T) {
	s := newQwenSession(t)
	s.hook(HookNameSessionStart, "2026-11-06T10:00:00Z", nil)
	turn := s.position()
	s.hook(HookNameUserPromptSubmit, "2026-11-06T10:00:01Z", map[string]any{"prompt": "go"})
	s.writeNative(nativeLine("u-1", "user", "2026-11-06T10:00:01.1Z", ""))
	s.writeNative(nativeLine("a-1", "assistant", "2026-11-06T10:00:02Z", openAIUsage(100, 10, 0, 0)))
	s.hook(HookNamePreToolUse, "2026-11-06T10:00:02.1Z", map[string]any{"tool_name": "write_file", "tool_use_id": "call-1"})
	s.writeNative(nativeLine("t-1", "tool_result", "2026-11-06T10:00:02.5Z", "") +
		nativeLine("sys-1", "system", "2026-11-06T10:00:02.6Z", ""))

	// The final record is half written when Stop fires, and finished while
	// the hook waits.
	final := nativeLine("a-2", "assistant", "2026-11-06T10:00:04Z", openAIUsage(200, 20, 0, 0))
	s.writeNative(final[:len(final)/2])
	written := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		written <- appendFile(s.nativePath, final[len(final)/2:])
	}()
	s.hook(HookNameStop, "2026-11-06T10:00:04.1Z", map[string]any{"last_assistant_message": "done"})
	if err := <-written; err != nil {
		t.Fatal(err)
	}

	wantUsage(t, s.tokens(turn), protocol.TokenUsageResponse{InputTokens: 300, OutputTokens: 30, APICallCount: 2})
}

// A record Qwen is still writing has no newline yet. It must not be copied
// half-read or skipped: the next sync copies it whole.
func TestPartialNativeLineIsCopiedOnceComplete(t *testing.T) {
	s := newQwenSession(t)
	s.hook(HookNameSessionStart, "2026-11-06T10:00:00Z", nil)
	line := nativeLine("a-1", "assistant", "2026-11-06T10:00:02Z", openAIUsage(100, 10, 0, 0))
	s.writeNative(line[:len(line)/2])
	s.hook(HookNamePreToolUse, "2026-11-06T10:00:03Z", map[string]any{"tool_name": "read_file", "tool_use_id": "call-1"})
	wantUsage(t, s.tokens(0), protocol.TokenUsageResponse{})

	s.writeNative(line[len(line)/2:])
	s.hook(HookNameSessionEnd, "2026-11-06T10:00:04Z", map[string]any{"reason": "exit"})
	wantUsage(t, s.tokens(0), protocol.TokenUsageResponse{InputTokens: 100, OutputTokens: 10, APICallCount: 1})
}

// Qwen can rewrite its log in place, e.g. when prompts are deleted, so the
// remembered line may no longer hold the remembered record. The sync then
// rereads the log and copies only what it has not copied before.
func TestRewrittenNativeLogIsNotCountedTwice(t *testing.T) {
	s := newQwenSession(t)
	s.hook(HookNameSessionStart, "2026-11-06T10:00:00Z", nil)
	s.writeNative(nativeLine("u-1", "user", "2026-11-06T10:00:01Z", ""))
	s.writeNative(nativeLine("a-1", "assistant", "2026-11-06T10:00:02Z", openAIUsage(100, 10, 0, 0)))
	s.writeNative(nativeLine("a-2", "assistant", "2026-11-06T10:00:03Z", openAIUsage(200, 20, 0, 0)))
	s.hook(HookNameStop, "2026-11-06T10:00:04Z", nil)

	// Rewrite without the first prompt group, then append a new call.
	rewritten := nativeLine("a-2", "assistant", "2026-11-06T10:00:03Z", openAIUsage(200, 20, 0, 0)) +
		nativeLine("a-3", "assistant", "2026-11-06T10:00:05Z", openAIUsage(300, 30, 0, 0))
	if err := os.WriteFile(s.nativePath, []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}
	s.hook(HookNameStop, "2026-11-06T10:00:06Z", nil)

	wantUsage(t, s.tokens(0), protocol.TokenUsageResponse{InputTokens: 600, OutputTokens: 60, APICallCount: 3})

	// The copy made after the rewrite anchors later syncs in the rewritten
	// log, so they resume instead of rereading it.
	state, err := readUsageSyncState(s.agent.sidecarPath(s.id), s.nativePath, "")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(s.nativePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if got := resumeOffset(f, state.anchor); state.anchor == nil || state.anchor.NativeUUID != "a-3" || got != int64(len(rewritten)) {
		t.Fatalf("anchor = %+v, resumeOffset = %d; want a-3, %d", state.anchor, got, len(rewritten))
	}
	s.writeNative(nativeLine("a-4", "assistant", "2026-11-06T10:00:07Z", openAIUsage(400, 40, 0, 0)))
	s.hook(HookNameStop, "2026-11-06T10:00:08Z", nil)
	wantUsage(t, s.tokens(0), protocol.TokenUsageResponse{InputTokens: 1000, OutputTokens: 100, APICallCount: 4})
}

// The anchor is read from the end of the sidecar: the last AssistantUsage
// record copied from the given log, past long lines and a line still being
// written, without reading the whole session.
func TestLastUsageRecordReadsTheSidecarBackwards(t *testing.T) {
	t.Parallel()
	usage := func(uuid, native string) string {
		data, err := json.Marshal(sidecarRecord{
			V: sidecarVersion, Agent: AgentName, Event: eventAssistantUsage, NativeTranscriptPath: native,
			NativeUUID: uuid, Usage: &qwenUsageMetadata{PromptTokenCount: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(data) + "\n"
	}
	long := `{"v":2,"agent":"qwen","event":"PostToolUse","tool_response":"` + strings.Repeat("x", 200*1024) + `"}` + "\n"
	for _, tc := range []struct {
		name, sidecar, want string
	}{
		{"first line", usage("a-1", "/n.jsonl"), "a-1"},
		{"last of several", usage("a-1", "/n.jsonl") + usage("a-2", "/n.jsonl") + long, "a-2"},
		{"past long lines and other logs", usage("a-1", "/n.jsonl") + long + usage("b-1", "/other.jsonl") + long, "a-1"},
		{"partial trailing line", usage("a-1", "/n.jsonl") + strings.TrimSuffix(usage("a-2", "/n.jsonl"), "\n"), "a-1"},
		{"none", long + usage("b-1", "/other.jsonl"), ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "sidecar.jsonl")
			if err := os.WriteFile(path, []byte(tc.sidecar), 0o600); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			record, err := lastUsageRecord(f, "/n.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if record != nil {
				got = record.NativeUUID
			}
			if got != tc.want {
				t.Fatalf("last usage record = %q, want %q", got, tc.want)
			}
		})
	}
}

// A sync resumes after the last copied line instead of rereading the log, as
// long as that line still holds the record copied from it.
func TestResumeOffsetChecksTheAnchoredRecord(t *testing.T) {
	s := newQwenSession(t)
	s.hook(HookNameSessionStart, "2026-11-06T10:00:00Z", nil)
	first := nativeLine("u-1", "user", "2026-11-06T10:00:01Z", "")
	anchored := nativeLine("a-1", "assistant", "2026-11-06T10:00:02Z", openAIUsage(100, 10, 0, 0))
	s.writeNative(first + anchored)
	s.hook(HookNameStop, "2026-11-06T10:00:03Z", nil)

	state, err := readUsageSyncState(s.agent.sidecarPath(s.id), s.nativePath, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.anchor == nil || state.anchor.NativeUUID != "a-1" || state.anchor.NativeOffset != int64(len(first)) {
		t.Fatalf("anchor = %+v, want a-1 at offset %d", state.anchor, len(first))
	}
	f, err := os.Open(s.nativePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if got, want := resumeOffset(f, state.anchor), int64(len(first)+len(anchored)); got != want {
		t.Fatalf("resumeOffset = %d, want %d", got, want)
	}
	moved := *state.anchor
	moved.NativeUUID = "a-other"
	if got := resumeOffset(f, &moved); got != 0 {
		t.Fatalf("resumeOffset for a line holding another record = %d, want 0", got)
	}
}

// A resumed session whose sidecar was cleaned out of the temp directory must
// not count the whole earlier history into its first turn.
func TestNativeHistoryBeforeTheSidecarIsNotCopied(t *testing.T) {
	s := newQwenSession(t)
	s.writeNative(nativeLine("old-1", "assistant", "2026-11-01T09:00:00Z", openAIUsage(5000, 500, 0, 0)))
	s.writeNative(nativeLine("old-2", "assistant", "2026-11-01T09:05:00Z", openAIUsage(6000, 600, 0, 0)))

	s.hook(HookNameSessionStart, "2026-11-06T10:00:00Z", map[string]any{"source": "resume"})
	s.hook(HookNameUserPromptSubmit, "2026-11-06T10:00:01Z", map[string]any{"prompt": "continue"})
	s.writeNative(nativeLine("a-1", "assistant", "2026-11-06T10:00:02Z", openAIUsage(100, 10, 0, 0)))
	s.hook(HookNameStop, "2026-11-06T10:00:03Z", nil)

	wantUsage(t, s.tokens(0), protocol.TokenUsageResponse{InputTokens: 100, OutputTokens: 10, APICallCount: 1})
}

// A session whose sidecar an older adapter started has no usage copied yet.
// After the upgrade, counting starts with the first record the new adapter
// wrote, instead of the session's whole history landing in the next turn.
func TestSidecarFromAnOlderAdapterStartsAtTheUpgrade(t *testing.T) {
	s := newQwenSession(t)
	older := `{"v":1,"agent":"qwen","event":"SessionStart","session_id":"` + s.id + `","ts":"2026-11-01T09:00:00Z","source":"startup"}` + "\n" +
		`{"v":1,"agent":"qwen","event":"UserPromptSubmit","session_id":"` + s.id + `","ts":"2026-11-01T09:00:01Z","prompt":"first"}` + "\n" +
		`{"v":1,"agent":"qwen","event":"Stop","session_id":"` + s.id + `","ts":"2026-11-01T09:00:09Z"}` + "\n"
	if err := appendFile(s.agent.sidecarPath(s.id), older); err != nil {
		t.Fatal(err)
	}
	s.writeNative(nativeLine("old-1", "assistant", "2026-11-01T09:00:05Z", openAIUsage(5000, 500, 0, 0)))

	turn := s.position()
	s.hook(HookNameUserPromptSubmit, "2026-11-06T10:00:01Z", map[string]any{"prompt": "continue"})
	s.writeNative(nativeLine("a-1", "assistant", "2026-11-06T10:00:02Z", openAIUsage(100, 10, 0, 0)))
	s.hook(HookNameStop, "2026-11-06T10:00:03Z", nil)
	// A later reread of the whole log, after Qwen rewrites it, still leaves
	// the older history out.
	s.writeNative(nativeLine("a-2", "assistant", "2026-11-06T10:01:02Z", openAIUsage(200, 20, 0, 0)))
	if err := os.WriteFile(s.nativePath, append([]byte(nativeLine("u-0", "user", "2026-11-01T09:00:01Z", "")), mustRead(t, s.nativePath)...), 0o600); err != nil {
		t.Fatal(err)
	}
	s.hook(HookNameStop, "2026-11-06T10:01:03Z", nil)

	wantUsage(t, s.tokens(turn), protocol.TokenUsageResponse{InputTokens: 300, OutputTokens: 30, APICallCount: 2})
	wantUsage(t, s.tokens(0), protocol.TokenUsageResponse{InputTokens: 300, OutputTokens: 30, APICallCount: 2})
}

// Records /branch copied from a parent session, and subagent records, were
// counted where their API calls were made.
func TestForkedAndSidechainRecordsAreNotCopied(t *testing.T) {
	s := newQwenSession(t)
	s.hook(HookNameSessionStart, "2026-11-06T10:00:00Z", nil)
	forked := strings.TrimSuffix(nativeLine("f-1", "assistant", "2026-11-06T10:00:01Z", openAIUsage(900, 90, 0, 0)), "}\n") +
		`,"forkedFrom":{"sessionId":"parent","messageUuid":"f-1"}}` + "\n"
	sidechain := strings.TrimSuffix(nativeLine("s-1", "assistant", "2026-11-06T10:00:01Z", openAIUsage(800, 80, 0, 0)), "}\n") +
		`,"isSidechain":true,"agentId":"explore-7f3c"}` + "\n"
	s.writeNative(forked + sidechain + nativeLine("a-1", "assistant", "2026-11-06T10:00:02Z", openAIUsage(100, 10, 0, 0)))
	s.hook(HookNameStop, "2026-11-06T10:00:03Z", nil)

	wantUsage(t, s.tokens(0), protocol.TokenUsageResponse{InputTokens: 100, OutputTokens: 10, APICallCount: 1})
}

// Two hooks syncing at once can both copy the same call. It is counted once,
// also when the first copy lies before the offset and the second after it.
func TestDuplicateCopiesAreCountedOnce(t *testing.T) {
	record := func(uuid string, prompt int) string {
		data, err := json.Marshal(sidecarRecord{
			V: 1, Agent: AgentName, Event: eventAssistantUsage, SessionID: "s", NativeUUID: uuid,
			Usage: &qwenUsageMetadata{PromptTokenCount: prompt, CandidatesTokenCount: 1, TotalTokenCount: prompt + 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(data) + "\n"
	}
	sidecar := []byte(record("a-1", 100) + `{"v":1,"agent":"qwen","event":"UserPromptSubmit","prompt":"next"}` + "\n" +
		record("a-1", 100) + record("a-2", 200) + record("a-2", 200))

	usage, err := New().CalculateTokens(sidecar, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantUsage(t, usage, protocol.TokenUsageResponse{InputTokens: 300, OutputTokens: 2, APICallCount: 2})
	usage, err = New().CalculateTokens(sidecar, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantUsage(t, usage, protocol.TokenUsageResponse{InputTokens: 200, OutputTokens: 1, APICallCount: 1})
}

// Qwen normalizes every provider to Gemini's usageMetadata names, but the
// providers disagree on whether reasoning is inside the output count.
func TestUsageMetadataMapping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		usage qwenUsageMetadata
		want  protocol.TokenUsageResponse
	}{
		{
			name:  "openai compatible: completion includes reasoning",
			usage: qwenUsageMetadata{PromptTokenCount: 1000, CandidatesTokenCount: 300, ThoughtsTokenCount: 200, CachedContentTokenCount: 600, TotalTokenCount: 1300},
			want:  protocol.TokenUsageResponse{InputTokens: 400, CacheReadTokens: 600, OutputTokens: 300, APICallCount: 1},
		},
		{
			name:  "gemini: thoughts reported apart",
			usage: qwenUsageMetadata{PromptTokenCount: 1000, CandidatesTokenCount: 100, ThoughtsTokenCount: 200, CachedContentTokenCount: 0, TotalTokenCount: 1300},
			want:  protocol.TokenUsageResponse{InputTokens: 1000, OutputTokens: 300, APICallCount: 1},
		},
		{
			name:  "no total: openai reading",
			usage: qwenUsageMetadata{PromptTokenCount: 50, CandidatesTokenCount: 5, ThoughtsTokenCount: 3},
			want:  protocol.TokenUsageResponse{InputTokens: 50, OutputTokens: 5, APICallCount: 1},
		},
		{
			name:  "cache larger than prompt is clamped",
			usage: qwenUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 1, CachedContentTokenCount: 50, TotalTokenCount: 11},
			want:  protocol.TokenUsageResponse{CacheReadTokens: 10, OutputTokens: 1, APICallCount: 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got protocol.TokenUsageResponse
			tc.usage.addTo(&got)
			wantUsage(t, got, tc.want)
		})
	}
}

// The protocol compliance suite sends "{}" with --offset 0, and a sidecar
// without usage (written before this change) must report zeros, not fail.
func TestCalculateTokensWithoutUsage(t *testing.T) {
	t.Parallel()
	for _, input := range [][]byte{nil, []byte(`{}`), []byte("not json\n\n"), []byte(`{"v":1,"agent":"qwen","event":"Stop","last_assistant_message":"done"}` + "\n")} {
		usage, err := New().CalculateTokens(input, 0)
		if err != nil {
			t.Fatalf("CalculateTokens(%q): %v", input, err)
		}
		wantUsage(t, usage, protocol.TokenUsageResponse{})
	}
	if !New().Info().Capabilities.TokenCalculator {
		t.Fatal("token_calculator is not declared, so Entire never calls calculate-tokens")
	}
}

// A line longer than the read limit is skipped without losing the lines
// around it or the byte offsets after it.
func TestReadLineSkipsOversizedLines(t *testing.T) {
	t.Parallel()
	input := "short\n" + strings.Repeat("x", 100) + "\n" + "tail\n" + "partial"
	reader := bufio.NewReaderSize(strings.NewReader(input), 16)
	var lines []string
	var consumed int64
	for {
		line, n, complete, err := readLine(reader, 32)
		if err != nil {
			t.Fatal(err)
		}
		if !complete {
			break
		}
		consumed += n
		lines = append(lines, string(bytes.TrimSuffix(line, []byte("\n"))))
	}
	if got := strings.Join(lines, "|"); got != "short||tail" {
		t.Fatalf("lines = %q", got)
	}
	if want := int64(len(input) - len("partial")); consumed != want {
		t.Fatalf("consumed %d bytes, want %d", consumed, want)
	}
}
