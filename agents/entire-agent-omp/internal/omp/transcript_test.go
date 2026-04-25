package omp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

const testSessionJSONL = `{"type":"session","version":3,"id":"test-uuid-123","timestamp":"2026-03-27T21:00:00.000Z","cwd":"/tmp/test"}
{"type":"model_change","id":"mc1","parentId":null,"timestamp":"2026-03-27T21:00:00.001Z","provider":"anthropic","modelId":"claude-sonnet-4-6"}
{"type":"message","id":"m1","parentId":"mc1","timestamp":"2026-03-27T21:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"Create hello.txt"}],"timestamp":1774646400000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-27T21:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"write","arguments":{"path":"hello.txt","content":"hello world\n"}}],"usage":{"input":100,"output":50,"cacheRead":10,"cacheWrite":5},"stopReason":"toolUse","timestamp":1774646401000}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-03-27T21:00:03.000Z","message":{"role":"toolResult","toolCallId":"tc1","toolName":"write","content":[{"type":"text","text":"Written 12 bytes"}],"isError":false,"timestamp":1774646402000}}
{"type":"message","id":"m4","parentId":"m3","timestamp":"2026-03-27T21:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Created hello.txt with the content hello world."}],"usage":{"input":200,"output":30,"cacheRead":0,"cacheWrite":0},"stopReason":"stop","timestamp":1774646403000}}
`

// testBranchingSessionJSONL has two branches from m1:
//   - abandoned: m2 (write old.txt) → m3 → m4 ("Created old.txt")
//   - active:    m5 (write new.txt) → m6 → m7 ("Created new.txt")
const testBranchingSessionJSONL = `{"type":"session","version":3,"id":"test-branch-123","timestamp":"2026-03-27T22:00:00.000Z","cwd":"/tmp/test"}
{"type":"model_change","id":"mc1","parentId":null,"timestamp":"2026-03-27T22:00:00.001Z","provider":"anthropic","modelId":"claude-sonnet-4-6"}
{"type":"message","id":"m1","parentId":"mc1","timestamp":"2026-03-27T22:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"Create a file"}],"timestamp":1774650000000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-27T22:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"write","arguments":{"path":"old.txt","content":"old\n"}}],"usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0},"stopReason":"toolUse","timestamp":1774650001000}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-03-27T22:00:03.000Z","message":{"role":"toolResult","toolCallId":"tc1","toolName":"write","content":[{"type":"text","text":"Written 4 bytes"}],"isError":false,"timestamp":1774650002000}}
{"type":"message","id":"m4","parentId":"m3","timestamp":"2026-03-27T22:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Created old.txt"}],"usage":{"input":200,"output":30,"cacheRead":0,"cacheWrite":0},"stopReason":"stop","timestamp":1774650003000}}
{"type":"message","id":"m5","parentId":"m1","timestamp":"2026-03-27T22:00:05.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc2","name":"write","arguments":{"path":"new.txt","content":"new\n"}}],"usage":{"input":150,"output":60,"cacheRead":5,"cacheWrite":3},"stopReason":"toolUse","timestamp":1774650004000}}
{"type":"message","id":"m6","parentId":"m5","timestamp":"2026-03-27T22:00:06.000Z","message":{"role":"toolResult","toolCallId":"tc2","toolName":"write","content":[{"type":"text","text":"Written 4 bytes"}],"isError":false,"timestamp":1774650005000}}
{"type":"message","id":"m7","parentId":"m6","timestamp":"2026-03-27T22:00:07.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Created new.txt"}],"usage":{"input":250,"output":40,"cacheRead":0,"cacheWrite":0},"stopReason":"stop","timestamp":1774650006000}}
`

func writeTestSession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-03-27T21-00-00-000Z_test-uuid-123.jsonl")
	if err := os.WriteFile(path, []byte(testSessionJSONL), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractModifiedFiles(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	files, pos, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "hello.txt" {
		t.Errorf("files = %v, want [hello.txt]", files)
	}
	if pos == 0 {
		t.Error("position should be > 0")
	}
}

func TestExtractModifiedFiles_WithOffset(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	// Use a large offset to skip all content.
	info, _ := os.Stat(path)
	files, _, err := agent.ExtractModifiedFiles(path, int(info.Size()))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want empty", files)
	}
}

func TestExtractPrompts(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "Create hello.txt" {
		t.Errorf("prompts = %v, want [Create hello.txt]", prompts)
	}
}

func TestExtractSummary(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	summary, has, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("expected has_summary = true")
	}
	if summary != "Created hello.txt with the content hello world." {
		t.Errorf("summary = %q", summary)
	}
}

func TestGetTranscriptPosition(t *testing.T) {
	path := writeTestSession(t)
	agent := New()

	pos, err := agent.GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	// testSessionJSONL has 6 lines (session, model_change, m1, m2, m3, m4)
	if pos != 6 {
		t.Errorf("position = %d, want 6", pos)
	}
}

func TestGetTranscriptPosition_Missing(t *testing.T) {
	agent := New()
	pos, err := agent.GetTranscriptPosition("/nonexistent/file.jsonl")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if pos != 0 {
		t.Errorf("position = %d, want 0 for missing file", pos)
	}
}

func TestCalculateTokens(t *testing.T) {
	path := writeTestSession(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	agent := New()
	usage, err := agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatal(err)
	}

	if usage.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", usage.InputTokens)
	}
	if usage.OutputTokens != 80 {
		t.Errorf("OutputTokens = %d, want 80", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 10 {
		t.Errorf("CacheReadTokens = %d, want 10", usage.CacheReadTokens)
	}
	if usage.CacheCreationTokens != 5 {
		t.Errorf("CacheCreationTokens = %d, want 5", usage.CacheCreationTokens)
	}
	if usage.APICallCount != 2 {
		t.Errorf("APICallCount = %d, want 2", usage.APICallCount)
	}
}

func TestCalculateTokens_OffsetAtEnd(t *testing.T) {
	path := writeTestSession(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	agent := New()

	// offset == len(data): should return zero tokens
	usage, err := agent.CalculateTokens(data, len(data))
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.APICallCount != 0 {
		t.Errorf("offset=len(data): got input=%d output=%d calls=%d, want all zero",
			usage.InputTokens, usage.OutputTokens, usage.APICallCount)
	}

	// offset > len(data): should also return zero tokens
	usage, err = agent.CalculateTokens(data, len(data)+100)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.APICallCount != 0 {
		t.Errorf("offset>len(data): got input=%d output=%d calls=%d, want all zero",
			usage.InputTokens, usage.OutputTokens, usage.APICallCount)
	}
}

func TestExtractPrompts_OffsetAtEnd(t *testing.T) {
	path := writeTestSession(t)
	data, _ := os.ReadFile(path)
	agent := New()

	// offset > len(data): should return no prompts
	prompts, err := agent.ExtractPrompts(path, len(data)+100)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 0 {
		t.Errorf("offset>len(data): got %d prompts, want 0", len(prompts))
	}
}

func TestChunkAndReassemble(t *testing.T) {
	agent := New()
	data := []byte("hello world, this is a test")

	chunks, err := agent.ChunkTranscript(data, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Errorf("chunks = %d, want 3", len(chunks))
	}

	reassembled, err := agent.ReassembleTranscript(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if string(reassembled) != string(data) {
		t.Errorf("reassembled = %q, want %q", reassembled, data)
	}
}

func TestChunkTranscript_InvalidMaxSize(t *testing.T) {
	agent := New()
	_, err := agent.ChunkTranscript([]byte("test"), 0)
	if err == nil {
		t.Error("expected error for max-size=0")
	}
}

func TestReadSession(t *testing.T) {
	path := writeTestSession(t)
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())

	agent := New()
	session, err := agent.ReadSession(&protocol.HookInputJSON{
		SessionRef: path,
	})
	if err != nil {
		t.Fatal(err)
	}

	if session.SessionID != "test-uuid-123" {
		t.Errorf("SessionID = %q, want %q", session.SessionID, "test-uuid-123")
	}
	if session.AgentName != "omp" {
		t.Errorf("AgentName = %q, want %q", session.AgentName, "omp")
	}
	if session.StartTime != "2026-03-27T21:00:00.000Z" {
		t.Errorf("StartTime = %q", session.StartTime)
	}
	if len(session.NativeData) == 0 {
		t.Error("NativeData should not be empty")
	}
	if session.ModifiedFiles == nil {
		t.Error("ModifiedFiles must be initialized (not nil)")
	}
	if session.NewFiles == nil {
		t.Error("NewFiles must be initialized (not nil)")
	}
	if session.DeletedFiles == nil {
		t.Error("DeletedFiles must be initialized (not nil)")
	}
}

func TestWriteSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	agent := New()
	data := []byte(`{"test":"data"}`)

	err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionRef: path,
		NativeData: data,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func writeJSONL(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBranchingSession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-03-27T22-00-00-000Z_test-branch-123.jsonl")
	if err := os.WriteFile(path, []byte(testBranchingSessionJSONL), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractModifiedFiles_Branching(t *testing.T) {
	path := writeBranchingSession(t)
	agent := New()

	files, _, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Only new.txt (active branch), not old.txt (abandoned branch).
	if len(files) != 1 || files[0] != "new.txt" {
		t.Errorf("files = %v, want [new.txt]", files)
	}
}

func TestExtractPrompts_Branching(t *testing.T) {
	path := writeBranchingSession(t)
	agent := New()

	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	// m1 is on the active branch.
	if len(prompts) != 1 || prompts[0] != "Create a file" {
		t.Errorf("prompts = %v, want [Create a file]", prompts)
	}
}

func TestExtractSummary_Branching(t *testing.T) {
	path := writeBranchingSession(t)
	agent := New()

	summary, has, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected has_summary = true")
	}
	// Active branch leaf, not abandoned branch.
	if summary != "Created new.txt" {
		t.Errorf("summary = %q, want %q", summary, "Created new.txt")
	}
}

func TestCalculateTokens_Branching(t *testing.T) {
	data := []byte(testBranchingSessionJSONL)
	agent := New()

	usage, err := agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Only m5 (input=150,output=60) and m7 (input=250,output=40) on active branch.
	if usage.InputTokens != 400 {
		t.Errorf("InputTokens = %d, want 400", usage.InputTokens)
	}
	if usage.OutputTokens != 100 {
		t.Errorf("OutputTokens = %d, want 100", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 5 {
		t.Errorf("CacheReadTokens = %d, want 5", usage.CacheReadTokens)
	}
	if usage.CacheCreationTokens != 3 {
		t.Errorf("CacheCreationTokens = %d, want 3", usage.CacheCreationTokens)
	}
	if usage.APICallCount != 2 {
		t.Errorf("APICallCount = %d, want 2", usage.APICallCount)
	}
}

// ---------------------------------------------------------------------------
// resolveActiveBranch unit tests
// ---------------------------------------------------------------------------

func TestResolveActiveBranch_LinearChain(t *testing.T) {
	data := []byte(`{"type":"session","id":"s1"}
{"type":"model_change","id":"mc1","parentId":null}
{"type":"message","id":"m1","parentId":"mc1"}
{"type":"message","id":"m2","parentId":"m1"}
{"type":"message","id":"m3","parentId":"m2"}
`)
	active := resolveActiveBranch(data)
	for _, id := range []string{"m3", "m2", "m1", "mc1"} {
		if !active[id] {
			t.Errorf("expected %q in active set", id)
		}
	}
	// session entry is not in the tree chain
	if active["s1"] {
		t.Error("session entry should not be in active set")
	}
}

func TestResolveActiveBranch_EmptyData(t *testing.T) {
	active := resolveActiveBranch(nil)
	if active != nil {
		t.Errorf("expected nil for empty data, got %v", active)
	}
}

func TestResolveActiveBranch_SessionOnly(t *testing.T) {
	data := []byte(`{"type":"session","id":"s1"}
`)
	active := resolveActiveBranch(data)
	if active != nil {
		t.Errorf("expected nil when no message entries, got %v", active)
	}
}

func TestResolveActiveBranch_CycleProtection(t *testing.T) {
	// a → b → a (cycle). Should terminate, not infinite loop.
	data := []byte(`{"type":"message","id":"a","parentId":"b"}
{"type":"message","id":"b","parentId":"a"}
`)
	active := resolveActiveBranch(data)
	// Should contain both but not hang.
	if !active["a"] || !active["b"] {
		t.Errorf("active = %v, want both a and b", active)
	}
}

func TestResolveActiveBranch_SelfReferentialParent(t *testing.T) {
	data := []byte(`{"type":"message","id":"m1","parentId":"m1"}
`)
	active := resolveActiveBranch(data)
	if !active["m1"] {
		t.Error("expected m1 in active set")
	}
	if len(active) != 1 {
		t.Errorf("expected 1 entry in active set, got %d", len(active))
	}
}

func TestResolveActiveBranch_TwoBranches_PicksLast(t *testing.T) {
	// Two branches from a: b (earlier) and c (later, last in file).
	// Active branch should be c's path.
	data := []byte(`{"type":"message","id":"a","parentId":"root"}
{"type":"message","id":"root","parentId":null}
{"type":"message","id":"b","parentId":"a"}
{"type":"message","id":"c","parentId":"a"}
`)

	active := resolveActiveBranch(data)
	if !active["c"] || !active["a"] {
		t.Errorf("expected c and a in active set, got %v", active)
	}
	if active["b"] {
		t.Error("b should not be in active set")
	}
}

func TestResolveActiveBranch_FlatReturnsNil(t *testing.T) {
	// No parentId at all — should skip tree resolution.
	data := []byte(`{"type":"message","id":"m1"}
{"type":"message","id":"m2"}
`)
	active := resolveActiveBranch(data)
	if active != nil {
		t.Errorf("expected nil for flat transcript, got %v", active)
	}
}

func TestResolveActiveBranch_NullParentIDOnly(t *testing.T) {
	// All entries have parentId:null — no real tree.
	data := []byte(`{"type":"message","id":"m1","parentId":null}
{"type":"message","id":"m2","parentId":null}
`)
	active := resolveActiveBranch(data)
	if active != nil {
		t.Errorf("expected nil when only null parentIds, got %v", active)
	}
}

func TestResolveActiveBranch_TrailingModelChange(t *testing.T) {
	// The last entry is a model_change, not a message.
	// resolveActiveBranch should use the last *message* as the leaf.
	data := []byte(`{"type":"message","id":"m1","parentId":"mc1","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}
{"type":"message","id":"mc1","parentId":null}
{"type":"message","id":"m2","parentId":"m1","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}
{"type":"model_change","id":"mc2","parentId":"m2","provider":"anthropic","modelId":"claude-opus-4-6"}
`)
	active := resolveActiveBranch(data)
	// Active branch should be m2→m1→mc1, resolved from last message (m2), not from mc2.
	if !active["m2"] || !active["m1"] {
		t.Errorf("expected m2 and m1 in active set, got %v", active)
	}
}

func TestTrailingModelChange_SummaryStillWorks(t *testing.T) {
	jsonl := `{"type":"session","id":"s1"}
{"type":"model_change","id":"mc1","parentId":null}
{"type":"message","id":"m1","parentId":"mc1","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}
{"type":"message","id":"m2","parentId":"m1","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0}}}
{"type":"model_change","id":"mc2","parentId":"m2"}
`
	path := writeJSONL(t, "trailing.jsonl", jsonl)
	agent := New()

	summary, has, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if !has || summary != "hello" {
		t.Errorf("summary = %q, want %q", summary, "hello")
	}

	usage, err := agent.CalculateTokens([]byte(jsonl), 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 5 {
		t.Errorf("tokens = %d/%d, want 10/5", usage.InputTokens, usage.OutputTokens)
	}
}

// ---------------------------------------------------------------------------
//
// testDeepBranchingJSONL:
//
//	mc1 → m1(user) → m2(asst, write a.txt) → m3(result) → m4(asst "done a")
//	                                           └→ m5(user "again") → m6(asst, write b.txt) → m7(result) → m8(asst "done b")
//	                                                                   └→ m9(asst, write c.txt) → m10(result) → m11(asst "done c")
//
// Active branch: mc1 → m1 → m2 → m3 → m5 → m9 → m10 → m11
// Abandoned: m4, m6, m7, m8
const testDeepBranchingJSONL = `{"type":"session","version":3,"id":"deep-123","timestamp":"2026-03-28T00:00:00.000Z","cwd":"/tmp"}
{"type":"model_change","id":"mc1","parentId":null}
{"type":"message","id":"m1","parentId":"mc1","message":{"role":"user","content":[{"type":"text","text":"make files"}]}}
{"type":"message","id":"m2","parentId":"m1","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"write","arguments":{"path":"a.txt","content":"a"}}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0}}}
{"type":"message","id":"m3","parentId":"m2","message":{"role":"toolResult","toolCallId":"tc1","toolName":"write","content":[{"type":"text","text":"ok"}]}}
{"type":"message","id":"m4","parentId":"m3","message":{"role":"assistant","content":[{"type":"text","text":"done a"}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0}}}
{"type":"message","id":"m5","parentId":"m3","message":{"role":"user","content":[{"type":"text","text":"try b instead"}]}}
{"type":"message","id":"m6","parentId":"m5","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc2","name":"write","arguments":{"path":"b.txt","content":"b"}}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0}}}
{"type":"message","id":"m7","parentId":"m6","message":{"role":"toolResult","toolCallId":"tc2","toolName":"write","content":[{"type":"text","text":"ok"}]}}
{"type":"message","id":"m8","parentId":"m7","message":{"role":"assistant","content":[{"type":"text","text":"done b"}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0}}}
{"type":"message","id":"m9","parentId":"m5","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc3","name":"write","arguments":{"path":"c.txt","content":"c"}}],"usage":{"input":20,"output":10,"cacheRead":0,"cacheWrite":0}}}
{"type":"message","id":"m10","parentId":"m9","message":{"role":"toolResult","toolCallId":"tc3","toolName":"write","content":[{"type":"text","text":"ok"}]}}
{"type":"message","id":"m11","parentId":"m10","message":{"role":"assistant","content":[{"type":"text","text":"done c"}],"usage":{"input":20,"output":10,"cacheRead":0,"cacheWrite":0}}}
`

func TestDeepBranching_ExtractModifiedFiles(t *testing.T) {
	path := writeJSONL(t, "deep.jsonl", testDeepBranchingJSONL)
	agent := New()

	files, _, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Active path includes m2 (write a.txt) and m9 (write c.txt).
	// m6 (write b.txt) is on abandoned second-level branch.
	want := map[string]bool{"a.txt": true, "c.txt": true}
	got := map[string]bool{}
	for _, f := range files {
		got[f] = true
	}
	if len(got) != len(want) {
		t.Errorf("files = %v, want %v", files, want)
	}
	for f := range want {
		if !got[f] {
			t.Errorf("missing expected file %q in %v", f, files)
		}
	}
}

func TestDeepBranching_CalculateTokens(t *testing.T) {
	data := []byte(testDeepBranchingJSONL)
	agent := New()

	usage, err := agent.CalculateTokens(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Active: m2(10/5), m9(20/10), m11(20/10). Abandoned: m4, m6, m8.
	if usage.InputTokens != 50 {
		t.Errorf("InputTokens = %d, want 50", usage.InputTokens)
	}
	if usage.OutputTokens != 25 {
		t.Errorf("OutputTokens = %d, want 25", usage.OutputTokens)
	}
	if usage.APICallCount != 3 {
		t.Errorf("APICallCount = %d, want 3", usage.APICallCount)
	}
}

// omp-specific: additional entry types (ttsr_injection, session_init, mode_change)
// must be ignored — they are not "message" type entries.
func TestOmpAdditionalEntryTypes_Ignored(t *testing.T) {
	jsonl := `{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp"}
{"type":"session_init","id":"si1","parentId":null}
{"type":"ttsr_injection","id":"ti1","parentId":"si1"}
{"type":"mode_change","id":"mode1","parentId":"ti1"}
{"type":"message","id":"m1","parentId":"mode1","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}
{"type":"message","id":"m2","parentId":"m1","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"write","arguments":{"path":"out.txt","content":"x"}}],"usage":{"input":5,"output":3,"cacheRead":0,"cacheWrite":0}}}
{"type":"ttsr_injection","id":"ti2","parentId":"m2"}
{"type":"message","id":"m3","parentId":"ti2","message":{"role":"toolResult","toolCallId":"tc1","toolName":"write","content":[{"type":"text","text":"ok"}]}}
{"type":"message","id":"m4","parentId":"m3","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0}}}
`
	path := writeJSONL(t, "omp-extra.jsonl", jsonl)
	agent := New()

	files, _, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "out.txt" {
		t.Errorf("files = %v, want [out.txt]", files)
	}

	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "hello" {
		t.Errorf("prompts = %v, want [hello]", prompts)
	}

	summary, has, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if !has || summary != "done" {
		t.Errorf("summary = %q has=%v, want done/true", summary, has)
	}
}
