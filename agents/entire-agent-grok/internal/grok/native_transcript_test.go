package grok

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

// realTranscript mirrors the shape Grok 1.0.5 actually writes: a system line, a
// <user_info> preamble, synthetic <system-reminder> injections carrying
// synthetic_reason, real prompts tagged with prompt_index, reasoning lines with
// an opaque encrypted_content, and tool results on their own lines.
const realTranscript = `{"type":"system","content":"You are Grok."}
{"type":"user","content":[{"type":"text","text":"<user_info>\nOS Version: macos\n</user_info>"}]}
{"type":"user","synthetic_reason":"system_reminder","content":[{"type":"text","text":"<system-reminder>\nMCP server connected: pw\n</system-reminder>"}]}
{"type":"user","prompt_index":0,"content":[{"type":"text","text":"<user_query>\nCreate hello.txt\n</user_query>"}]}
{"type":"reasoning","id":"r1","model_id":"grok-4.6","summary":[{"type":"summary_text","text":"The user wants a file."}],"encrypted_content":"8k0KExCUBzdOBM4cHXfgVQ=="}
{"type":"assistant","model_id":"grok-4.6","content":"","tool_calls":[{"id":"t1","name":"Write","arguments":"{\"path\":\"hello.txt\"}"}]}
{"type":"tool_result","tool_call_id":"t1","content":"wrote hello.txt"}
{"type":"assistant","model_id":"grok-4.6","content":"Created hello.txt"}
`

func compactLines(t *testing.T, data []byte) []compactLine {
	t.Helper()
	out, err := compactTranscriptBytes(data)
	if err != nil {
		t.Fatalf("compactTranscriptBytes: %v", err)
	}
	var lines []compactLine
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var line compactLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("unmarshal compact line %q: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines
}

// Grok injects context as user-role messages. Only prompt_index-tagged lines are
// things the human typed; treating the rest as prompts floods the summarizer.
func TestCompactKeepsOnlyRealUserPrompts(t *testing.T) {
	var prompts []string
	for _, line := range compactLines(t, []byte(realTranscript)) {
		if line.Type != "user" {
			continue
		}
		blocks, ok := line.Content.([]any)
		if !ok || len(blocks) == 0 {
			t.Fatalf("unexpected user content: %#v", line.Content)
		}
		block, _ := blocks[0].(map[string]any)
		text, _ := block["text"].(string)
		prompts = append(prompts, text)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected 1 real prompt, got %d: %q", len(prompts), prompts)
	}
	if prompts[0] != "Create hello.txt" {
		t.Fatalf("prompt not unwrapped from <user_query>: %q", prompts[0])
	}
}

// Grok writes tool output on a separate tool_result line keyed by tool_call_id.
// Without joining them every tool call compacts with no result at all.
func TestCompactAttachesToolResults(t *testing.T) {
	var found int
	for _, line := range compactLines(t, []byte(realTranscript)) {
		blocks, ok := line.Content.([]any)
		if !ok {
			continue
		}
		for _, raw := range blocks {
			block, _ := raw.(map[string]any)
			if block["type"] != "tool_use" {
				continue
			}
			found++
			result, ok := block["result"].(map[string]any)
			if !ok {
				t.Fatalf("tool_use %v has no result", block["id"])
			}
			if got := result["output"]; got != "wrote hello.txt" {
				t.Fatalf("unexpected tool output: %v", got)
			}
		}
	}
	if found != 1 {
		t.Fatalf("expected 1 tool_use block, got %d", found)
	}
}

// The reasoning summary is useful context; encrypted_content is opaque and is
// the field redaction corrupts, so it must never reach the compacted output.
func TestCompactKeepsReasoningSummaryButNotEncryptedContent(t *testing.T) {
	out, err := compactTranscriptBytes([]byte(realTranscript))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "The user wants a file.") {
		t.Fatal("reasoning summary was dropped")
	}
	if strings.Contains(string(out), "encrypted_content") || strings.Contains(string(out), "8k0KExCUBzdOBM4cHXfgVQ==") {
		t.Fatal("encrypted_content leaked into the compacted transcript")
	}
}

// Entire scopes a checkpoint by slicing from a mid-session offset and reads the
// file while Grok is still appending. Neither should fail the whole parse: an
// error here surfaces as "transcript has no content to summarize".
func TestCompactToleratesTruncatedLines(t *testing.T) {
	full := []byte(realTranscript)
	cases := map[string][]byte{
		"slice starting mid-line": full[len(full)/2:],
		"half-flushed final line": append(append([]byte{}, full...), []byte(`{"type":"assistant","cont`)...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := compactTranscriptBytes(data); err != nil {
				t.Fatalf("expected tolerant parse, got %v", err)
			}
		})
	}
}

// A checkpoint slice can begin after the assistant line that issued a call,
// leaving its result orphaned. The output must still carry the tool output.
func TestCompactEmitsOrphanedToolResult(t *testing.T) {
	orphan := []byte(`{"type":"tool_result","tool_call_id":"t1","content":"wrote hello.txt"}` + "\n")
	out, err := compactTranscriptBytes(orphan)
	if err != nil {
		t.Fatalf("orphaned tool_result should still compact: %v", err)
	}
	if !strings.Contains(string(out), "wrote hello.txt") {
		t.Fatal("orphaned tool result output was lost")
	}
}

// A byte-level match on `"type":"user"` misses a serializer that emits
// `"type": "user"`, silently routing a chat history into the sidecar parser.
func TestCompactDispatchIgnoresJSONSpacing(t *testing.T) {
	spaced := strings.ReplaceAll(realTranscript, `"type":"`, `"type": "`)
	if !looksLikeChatHistory([]byte(spaced)) {
		t.Fatal("spaced chat history not recognized")
	}
	if _, err := compactTranscriptBytes([]byte(spaced)); err != nil {
		t.Fatalf("spaced chat history failed to compact: %v", err)
	}
}

// Entire sidecar records carry "event" and must not be mistaken for a chat history.
func TestCompactDispatchRejectsSidecarRecords(t *testing.T) {
	sidecar := []byte(`{"v":1,"agent":"grok","event":"UserPromptSubmit","prompt":"hi"}` + "\n")
	if looksLikeChatHistory(sidecar) {
		t.Fatal("sidecar records must not route to the chat-history parser")
	}
}

// Restoring chat_history.jsonl alone leaves Grok unable to find the session; it
// reports "Session not found locally" and `grok --continue` silently resumes a
// different session instead. summary.json is what makes the directory real.
func TestWriteSessionCreatesResumableSummary(t *testing.T) {
	repo := t.TempDir()
	testGrokHome(t)
	agent := New()

	sessionID := "01a0428c-8bcf-7000-8a5b-13ba3773a892"
	ref := nativeTranscriptPath(repo, sessionID)
	session := protocol.AgentSessionJSON{
		SessionID:  sessionID,
		AgentName:  AgentName,
		RepoPath:   repo,
		SessionRef: ref,
		// Entire sends an unset start time as a formatted zero time.
		StartTime:  "0001-01-01T00:00:00Z",
		NativeData: []byte(realTranscript),
	}
	if err := agent.WriteSession(session); err != nil {
		t.Fatal(err)
	}

	var summary grokSessionSummary
	data, err := os.ReadFile(filepath.Join(filepath.Dir(ref), "summary.json"))
	if err != nil {
		t.Fatalf("summary.json not written: %v", err)
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Info.ID != sessionID {
		t.Fatalf("session id = %q", summary.Info.ID)
	}
	// Grok rejects a summary with no model and one with a year-1 timestamp.
	if summary.CurrentModelID != "grok-4.6" {
		t.Fatalf("current_model_id = %q, want it carried over from the transcript", summary.CurrentModelID)
	}
	if strings.HasPrefix(summary.CreatedAt, "0001-") {
		t.Fatalf("created_at kept the zero time: %q", summary.CreatedAt)
	}
	if !strings.HasSuffix(summary.GitRootDir, "/") {
		t.Fatalf("git_root_dir = %q, want a trailing separator", summary.GitRootDir)
	}
	if summary.ChatFormatVersion != 1 {
		t.Fatalf("chat_format_version = %d", summary.ChatFormatVersion)
	}
}

// A restore over a live session must not clobber Grok's own metadata.
func TestWriteSessionPreservesExistingSummary(t *testing.T) {
	repo := t.TempDir()
	testGrokHome(t)
	agent := New()

	sessionID := "01a0428c-8bcf-7000-8a5b-13ba3773a892"
	ref := nativeTranscriptPath(repo, sessionID)
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID: sessionID, RepoPath: repo, SessionRef: ref,
		NativeData: []byte(realTranscript),
	}); err != nil {
		t.Fatal(err)
	}
	summaryPath := filepath.Join(filepath.Dir(ref), "summary.json")
	if err := os.WriteFile(summaryPath, []byte(`{"session_summary":"grok's own"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.WriteSession(protocol.AgentSessionJSON{
		SessionID: sessionID, RepoPath: repo, SessionRef: ref,
		NativeData: []byte(realTranscript),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "grok's own") {
		t.Fatal("existing summary.json was overwritten")
	}
}

// Grok fires StopCancelled instead of Stop on interrupts, declined permissions
// and --max-turns. Without it those turns produce no end-of-turn checkpoint.
func TestParseHookHandlesStopCancelled(t *testing.T) {
	repo := t.TempDir()
	testGrokHome(t)
	agent := New()

	payload := `{"session_id":"s1","hook_event_name":"StopCancelled","last_assistant_message":"partial answer","cwd":"` + repo + `"}`
	event, err := agent.ParseHook(HookNameStopCancelled, []byte(payload))
	if err != nil {
		t.Fatalf("ParseHook(stop-cancelled): %v", err)
	}
	if event == nil {
		t.Fatal("stop-cancelled produced no event; interrupted turns would go uncaptured")
	}
	if event.ResponseMessage != "partial answer" {
		t.Fatalf("response message = %q", event.ResponseMessage)
	}
}

func TestInstallHooksRegistersStopCancelled(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	testGrokHome(t)
	agent := New()

	if _, err := agent.InstallHooks(false, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(hooksFilePath(repo))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "StopCancelled") {
		t.Fatal("StopCancelled hook was not installed")
	}
}

func TestCompactTranscriptResponseIsBase64(t *testing.T) {
	repo := t.TempDir()
	ref := writeNativeTranscript(t, repo, "roundtrip", realTranscript)
	resp, err := New().CompactTranscript(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base64.StdEncoding.DecodeString(resp.Transcript); err != nil {
		t.Fatalf("transcript is not valid base64: %v", err)
	}
}

// Grok's encrypted_content is large and, because its name does not match the
// CLI redactor's "*signature" rule, the entropy scanner corrupts it — which makes
// the restored session unreplayable. It must never reach Entire's stored copy.
func TestReadPathsStripEncryptedContent(t *testing.T) {
	repo := t.TempDir()
	ref := writeNativeTranscript(t, repo, "strip", realTranscript)
	agent := New()

	transcript, err := agent.ReadTranscript(ref)
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.ReadSession(&protocol.HookInputJSON{SessionID: "strip", SessionRef: ref})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"ReadTranscript": transcript,
		"ReadSession":    session.NativeData,
	} {
		if bytes.Contains(data, []byte("encrypted_content")) {
			t.Errorf("%s: encrypted_content survived", name)
		}
		if bytes.Contains(data, []byte("8k0KExCUBzdOBM4cHXfgVQ==")) {
			t.Errorf("%s: blob value survived", name)
		}
	}
}

// Only the key goes. The reasoning line and its plaintext summary are useful
// context and must stay.
func TestSanitizeKeepsReasoningLineAndSummary(t *testing.T) {
	out := sanitizeTranscriptForStorage([]byte(realTranscript))
	if !bytes.Contains(out, []byte("The user wants a file.")) {
		t.Fatal("reasoning summary was dropped")
	}
	var found bool
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte("\n")) {
		var m map[string]any
		if json.Unmarshal(line, &m) != nil || m["type"] != "reasoning" {
			continue
		}
		found = true
		if _, ok := m["encrypted_content"]; ok {
			t.Fatal("encrypted_content still present on reasoning line")
		}
		if m["id"] != "r1" {
			t.Fatalf("reasoning line lost its id: %v", m["id"])
		}
	}
	if !found {
		t.Fatal("reasoning line was removed entirely")
	}
}

// Entire counts checkpoint offsets in messages, so the sanitizer must emit one
// line per input line. Dropping any would shift every offset after it.
func TestSanitizePreservesLineCount(t *testing.T) {
	in := []byte(realTranscript)
	out := sanitizeTranscriptForStorage(in)
	want := len(bytes.Split(bytes.TrimSpace(in), []byte("\n")))
	got := len(bytes.Split(bytes.TrimSpace(out), []byte("\n")))
	if got != want {
		t.Fatalf("line count changed: %d -> %d", want, got)
	}
}

// A transcript with nothing to strip must come back untouched, byte for byte.
func TestSanitizeNoMarkerReturnsInputUnchanged(t *testing.T) {
	clean := []byte(`{"type":"user","prompt_index":0,"content":[{"type":"text","text":"hi"}]}` + "\n")
	out := sanitizeTranscriptForStorage(clean)
	if !bytes.Equal(out, clean) {
		t.Fatalf("clean transcript was rewritten:\n got %q\nwant %q", out, clean)
	}
}

// A checkpoint slice can start mid-line, and Grok may be mid-append. Such a line
// is not corruption, so keep it verbatim rather than dropping or repairing it.
func TestSanitizeKeepsUnparseableLinesVerbatim(t *testing.T) {
	partial := `{"type":"reasoning","encrypted_content":"abc`
	in := []byte(realTranscript + partial + "\n")
	out := sanitizeTranscriptForStorage(in)
	if !bytes.Contains(out, []byte(partial)) {
		t.Fatal("truncated line was not preserved verbatim")
	}
}

func TestSanitizeIsIdempotent(t *testing.T) {
	once := sanitizeTranscriptForStorage([]byte(realTranscript))
	twice := sanitizeTranscriptForStorage(once)
	if !bytes.Equal(once, twice) {
		t.Fatal("sanitizing twice differs from sanitizing once")
	}
}

// "grok --continue" resumes the most recent session in the directory, which can
// be a different one — it silently answered from the wrong history in testing.
func TestFormatResumeCommandNamesTheSession(t *testing.T) {
	got := New().FormatResumeCommand("01a0428c-8bcf-7000-8a5b-13ba3773a892")
	if !strings.Contains(got, "01a0428c-8bcf-7000-8a5b-13ba3773a892") {
		t.Fatalf("resume command omits the session id: %q", got)
	}
	if strings.Contains(got, "--continue") {
		t.Fatalf("resume command still uses --continue: %q", got)
	}
}
