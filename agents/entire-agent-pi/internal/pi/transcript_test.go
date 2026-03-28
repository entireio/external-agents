package pi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
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
	info, _ := os.Stat(path)
	if pos != int(info.Size()) {
		t.Errorf("position = %d, want %d", pos, info.Size())
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
	if session.AgentName != "pi" {
		t.Errorf("AgentName = %q, want %q", session.AgentName, "pi")
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

// ---------------------------------------------------------------------------
// Deep tree: branch off a branch
// ---------------------------------------------------------------------------
// Trailing non-message entry: model_change after last message
// ---------------------------------------------------------------------------

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

// testDeepBranchingJSONL:
//
//	mc1 → m1(user) → m2(asst, write a.txt) → m3(result) → m4(asst "done a")
//	                                           └→ m5(user "again") → m6(asst, write b.txt) → m7(result) → m8(asst "done b")
//	                                                                   └→ m9(asst, write c.txt) → m10(result) → m11(asst "done c")
//
// Active branch: mc1 → m1 → m2 → m3 → m5 → m6 → m7 → m8 is NOT active
// Wait, let me think again. The last message is m11. Walk up: m11→m10→m9→m5→m1→mc1.
// So m6,m7,m8 are abandoned (branched from m5), and m2,m3,m4 are on the path (m3→m2→m1).
// Wait no: m5.parentId=m3, so the chain is m11→m10→m9→m5... but m9.parentId=m5? No, I need
// m9 to fork from m5 to create a deep branch. Let me design this more carefully.
//
// Tree:
//
//	mc1(root)
//	  └→ m1(user, parentId=mc1)
//	       └→ m2(asst write a.txt, parentId=m1)
//	            └→ m3(toolResult, parentId=m2)
//	                 ├→ m4(asst "done a", parentId=m3) [abandoned]
//	                 └→ m5(user "try b", parentId=m3)
//	                      ├→ m6(asst write b.txt, parentId=m5) [abandoned]
//	                      │    └→ m7(toolResult, parentId=m6) [abandoned]
//	                      │         └→ m8(asst "done b", parentId=m7) [abandoned]
//	                      └→ m9(asst write c.txt, parentId=m5)  [active]
//	                           └→ m10(toolResult, parentId=m9)
//	                                └→ m11(asst "done c", parentId=m10)
//
// Active path: m11→m10→m9→m5→m3→m2→m1→mc1
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

func TestDeepBranching_ExtractPrompts(t *testing.T) {
	path := writeJSONL(t, "deep.jsonl", testDeepBranchingJSONL)
	agent := New()

	prompts, err := agent.ExtractPrompts(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	// m1 and m5 are both on the active branch.
	if len(prompts) != 2 {
		t.Fatalf("prompts = %v, want 2 entries", prompts)
	}
	if prompts[0] != "make files" || prompts[1] != "try b instead" {
		t.Errorf("prompts = %v", prompts)
	}
}

func TestDeepBranching_Summary(t *testing.T) {
	path := writeJSONL(t, "deep.jsonl", testDeepBranchingJSONL)
	agent := New()

	summary, has, err := agent.ExtractSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if !has || summary != "done c" {
		t.Errorf("summary = %q, want %q", summary, "done c")
	}
}

func TestDeepBranching_Tokens(t *testing.T) {
	agent := New()
	usage, err := agent.CalculateTokens([]byte(testDeepBranchingJSONL), 0)
	if err != nil {
		t.Fatal(err)
	}
	// Active assistant messages: m2(10,5), m9(20,10), m11(20,10) = input:50, output:25
	// Abandoned: m4(10,5), m6(10,5), m8(10,5)
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

// ---------------------------------------------------------------------------
// Multi-fork: 3 branches from the same parent
// ---------------------------------------------------------------------------

const testMultiForkJSONL = `{"type":"session","id":"multi-123"}
{"type":"message","id":"root","parentId":"x","message":{"role":"user","content":[{"type":"text","text":"go"}]}}
{"type":"message","id":"a","parentId":"root","message":{"role":"assistant","content":[{"type":"text","text":"branch A"}],"usage":{"input":10,"output":1,"cacheRead":0,"cacheWrite":0}}}
{"type":"message","id":"b","parentId":"root","message":{"role":"assistant","content":[{"type":"text","text":"branch B"}],"usage":{"input":20,"output":2,"cacheRead":0,"cacheWrite":0}}}
{"type":"message","id":"c","parentId":"root","message":{"role":"assistant","content":[{"type":"text","text":"branch C"}],"usage":{"input":30,"output":3,"cacheRead":0,"cacheWrite":0}}}
`

func TestMultiFork_DefaultsToLastBranch(t *testing.T) {
	path := writeJSONL(t, "multi.jsonl", testMultiForkJSONL)
	agent := New()

	summary, _, _ := agent.ExtractSummary(path)
	if summary != "branch C" {
		t.Errorf("summary = %q, want %q", summary, "branch C")
	}

	usage, _ := agent.CalculateTokens([]byte(testMultiForkJSONL), 0)

	// Only "c" branch: input=30, output=3
	if usage.InputTokens != 30 || usage.OutputTokens != 3 {
		t.Errorf("tokens = %d/%d, want 30/3", usage.InputTokens, usage.OutputTokens)
	}
}

// ---------------------------------------------------------------------------
// Branching + offset: only entries after offset AND on active branch
// ---------------------------------------------------------------------------

func TestBranching_WithOffset(t *testing.T) {
	// Use the branching JSONL. Set offset past the abandoned branch entries
	// (m2-m4) but before the active branch entries (m5-m7).
	path := writeBranchingSession(t)

	data, _ := os.ReadFile(path)
	// Find byte offset just before m5's line.
	// m5 is the 7th line (0-indexed: session, mc1, m1, m2, m3, m4, m5).
	lines := 0
	offset := 0
	for i, b := range data {
		if b == '\n' {
			lines++
			if lines == 6 { // after m4's line
				offset = i + 1
				break
			}
		}
	}

	agent := New()
	files, _, err := agent.ExtractModifiedFiles(path, offset)
	if err != nil {
		t.Fatal(err)
	}
	// After offset, only m5-m7 are scanned. m5 is on active branch → new.txt.
	if len(files) != 1 || files[0] != "new.txt" {
		t.Errorf("files = %v, want [new.txt]", files)
	}
}

func TestBranching_OffsetSkipsActiveEntries(t *testing.T) {
	// Offset past everything — should return nothing even though active branch exists.
	path := writeBranchingSession(t)
	info, _ := os.Stat(path)
	agent := New()

	files, _, err := agent.ExtractModifiedFiles(path, int(info.Size()))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want empty", files)
	}
}

func TestBranching_OffsetWithAbandonedEntriesAfter(t *testing.T) {
	// Set offset to just before m2 (first abandoned entry).
	// Entries after offset: m2(abandoned), m3(abandoned), m4(abandoned), m5(active), m6(active), m7(active).
	// Only m5 should yield a file (new.txt), m2 should be filtered out.
	path := writeBranchingSession(t)

	data, _ := os.ReadFile(path)
	lines := 0
	offset := 0
	for i, b := range data {
		if b == '\n' {
			lines++
			if lines == 3 { // after m1's line (session, mc1, m1)
				offset = i + 1
				break
			}
		}
	}

	agent := New()
	files, _, err := agent.ExtractModifiedFiles(path, offset)
	if err != nil {
		t.Fatal(err)
	}
	// m2 (write old.txt) is abandoned, m5 (write new.txt) is active.
	if len(files) != 1 || files[0] != "new.txt" {
		t.Errorf("files = %v, want [new.txt]", files)
	}
}

// testFlatSessionJSONL has no parentId references (flat log, not a tree).
const testFlatSessionJSONL = `{"type":"session","id":"flat-123"}
{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}
{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0}}}
`

func TestFlatTranscript_NoTreeFiltering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flat.jsonl")
	if err := os.WriteFile(path, []byte(testFlatSessionJSONL), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := New()

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
	if !has || summary != "hi" {
		t.Errorf("summary = %q, want %q", summary, "hi")
	}

	usage, err := agent.CalculateTokens([]byte(testFlatSessionJSONL), 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 5 {
		t.Errorf("tokens = input:%d output:%d, want input:10 output:5",
			usage.InputTokens, usage.OutputTokens)
	}
}
