package goose

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// sliceFromLine mirrors Entire's transcript.SliceFromLine
// (cmd/entire/cli/transcript/parse.go:130): return the bytes starting after the
// Nth newline, or nil when there aren't that many lines. This is what
// cmd/entire/cli/explain.go:1883 (scopeTranscriptForCheckpoint) and
// cmd/entire/cli/strategy/manual_commit_condensation.go:1006 apply to every
// agent that is not one of Entire's built-ins — goose included.
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

// storedTranscript materializes the committed native export through the real
// write path (materializeExport -> encodeSessionJSONL), so every assertion
// below exercises the encoder rather than a checked-in expected constant.
func storedTranscript(t *testing.T) []byte {
	t.Helper()
	data, ok := materializeExport([]byte(exportFixture))
	if !ok {
		t.Fatal("exportFixture did not materialize")
	}
	return data
}

func storedTranscriptFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "20260611_1.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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

// compactKind mirrors normalizeKind in Entire's
// cmd/entire/cli/transcript/compact/compact.go:197: the entry kind comes from
// the TOP-LEVEL "type", falling back to "role".
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
// This is the half goose was missing — its lines carried content where Entire
// does not look, so transcript.jsonl came out empty.
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

func blockString(t *testing.T, block map[string]json.RawMessage, key string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(block[key], &s); err != nil {
		t.Fatalf("block[%q] = %s: %v", key, block[key], err)
	}
	return s
}

// The regression this change exists for: a single JSON document cannot survive
// Entire's line-offset scoping, so the transcript must be one message per line.
func TestStoredTranscriptIsOneMessagePerLine(t *testing.T) {
	data := storedTranscript(t)
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(`{"id": "20260611_1"`)) {
		t.Fatalf("native single-document export stored verbatim: %s", data)
	}
	if lines := bytes.Count(data, []byte{'\n'}); lines != 4 {
		t.Fatalf("expected 4 newline-terminated lines, got %d: %s", lines, data)
	}
	export, err := parseGooseExport(data)
	if err != nil {
		t.Fatalf("parseGooseExport: %v", err)
	}
	if len(export.Conversation) != 4 || export.Conversation[0].ID != "msg_1" || export.Conversation[3].ID != "msg_4" {
		t.Fatalf("round-trip conversation = %+v", export.Conversation)
	}
}

// Storing JSONL makes Entire keep the lines; it does not make them carry
// anything. Both halves of the contract are asserted because satisfying only
// the first yields a correctly-sized transcript.jsonl full of empty content.
func TestStoredLinesSatisfyEntireCompactContract(t *testing.T) {
	lines := decodeLines(t, storedTranscript(t))
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	wantKind := []string{"user", "assistant", "user", "assistant"}
	for i, line := range lines {
		if got := compactKind(line); got != wantKind[i] {
			t.Fatalf("line %d: compactKind = %q, want %q — Entire drops this line", i, got, wantKind[i])
		}
		if len(compactMessageContent(line)) == 0 {
			t.Fatalf("line %d: no content under the %q wrapper — Entire emits an empty line", i, "message")
		}
	}
	if got := blockString(t, compactMessageContent(lines[0])[0], "text"); got != "Create a file named verify.txt" {
		t.Fatalf("user text = %q", got)
	}
	if got := blockString(t, compactMessageContent(lines[3])[0], "text"); got != "ENTIRE_VERIFY_OK" {
		t.Fatalf("assistant text = %q", got)
	}
}

// A mid-session slice must keep satisfying the contract: Entire compacts the
// scoped bytes, not the whole file.
func TestScopedSliceSatisfiesEntireCompactContract(t *testing.T) {
	scoped := sliceFromLine(storedTranscript(t), 2)
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
	if got := blockString(t, compactMessageContent(lines[1])[0], "text"); got != "ENTIRE_VERIFY_OK" {
		t.Fatalf("scoped assistant text = %q", got)
	}
}

// Tool calls and their results must land in the shapes Entire matches on:
// tool_use keeps type/id/name/input (compact.go:704), and a user tool_result
// uses snake_case tool_use_id with a string "content" (compact.go:665) so
// Entire inlines the output into the preceding assistant's tool_use.
func TestToolBlocksProjectToEntireShapes(t *testing.T) {
	lines := decodeLines(t, storedTranscript(t))

	toolUse := compactMessageContent(lines[1])[0]
	for key, want := range map[string]string{"type": "tool_use", "id": "toolu_1", "name": "write"} {
		if got := blockString(t, toolUse, key); got != want {
			t.Fatalf("tool_use %q = %q, want %q", key, got, want)
		}
	}
	var input map[string]any
	if err := json.Unmarshal(toolUse["input"], &input); err != nil {
		t.Fatalf("tool_use input = %s: %v", toolUse["input"], err)
	}
	if input["path"] != "/tmp/workspace/verify.txt" {
		t.Fatalf("tool_use input = %+v", input)
	}

	toolResult := compactMessageContent(lines[2])[0]
	for key, want := range map[string]string{"type": "tool_result", "tool_use_id": "toolu_1"} {
		if got := blockString(t, toolResult, key); got != want {
			t.Fatalf("tool_result %q = %q, want %q", key, got, want)
		}
	}
	if _, ok := toolResult["content"]; !ok {
		t.Fatal("tool_result has no content key — Entire reads it as a string")
	}
}

// A goose toolResult's text parts must reach the tool_result block, and a
// failed call must be flagged so Entire records it as an error.
func TestToolResultTextAndErrorProject(t *testing.T) {
	export, ok := legacyGooseExport([]byte(`{
	  "id": "20260611_2",
	  "conversation": [
	    {"id":"m1","role":"assistant","created":1781175772,"content":[
	      {"type":"toolRequest","id":"t1","toolCall":{"status":"success","value":{"name":"write","arguments":{"path":"a.txt"}}}}]},
	    {"id":"m2","role":"user","created":1781175773,"content":[
	      {"type":"toolResponse","id":"t1","toolResult":{"status":"error","value":{"content":[{"type":"text","text":"permission denied"}],"isError":true}}}]}
	  ]
	}`))
	if !ok {
		t.Fatal("fixture is not a goose export")
	}
	data, err := encodeSessionJSONL(export)
	if err != nil {
		t.Fatal(err)
	}
	block := compactMessageContent(decodeLines(t, data)[1])[0]
	if got := blockString(t, block, "content"); got != "permission denied" {
		t.Fatalf("tool_result content = %q", got)
	}
	var isError bool
	if err := json.Unmarshal(block["is_error"], &isError); err != nil || !isError {
		t.Fatalf("tool_result is_error = %s (%v)", block["is_error"], err)
	}
}

// get-transcript-position is what Entire records as the next checkpoint's start
// line, so it must equal the stored line count or scoping cuts in the wrong
// place.
func TestGetTranscriptPositionMatchesLineCount(t *testing.T) {
	data := storedTranscript(t)
	path := storedTranscriptFile(t, data)
	pos, err := (&Agent{}).GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition: %v", err)
	}
	if lines := bytes.Count(data, []byte{'\n'}); pos != lines {
		t.Fatalf("position %d != newline count %d (Entire slices by line)", pos, lines)
	}
}

// A scoped slice has no header, so the session-level fields every analyzer
// depends on must travel on the records themselves.
func TestScopedSliceRemainsAnalyzable(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", t.TempDir())
	scoped := sliceFromLine(storedTranscript(t), 2)
	path := storedTranscriptFile(t, scoped)
	a := &Agent{}

	if pos, err := a.GetTranscriptPosition(path); err != nil || pos != 2 {
		t.Fatalf("scoped position = (%d, %v), want 2", pos, err)
	}
	summary, ok, err := a.ExtractSummary(path)
	if err != nil || !ok || summary != "File verification request" {
		t.Fatalf("scoped summary = (%q, %v, %v)", summary, ok, err)
	}
	if model := modelFromSessionRef(path); model != "anthropic/claude-opus-4.6" {
		t.Fatalf("scoped model = %q", model)
	}
	usage, err := a.CalculateTokens(scoped, 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 5650 || usage.OutputTokens != 124 {
		t.Fatalf("scoped tokens = %+v", usage)
	}
	session, err := a.ReadSession(&protocol.HookInputJSON{SessionRef: path})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "20260611_1" {
		t.Fatalf("scoped session id = %q, want 20260611_1 (recovered from a record)", session.SessionID)
	}
	if session.StartTime != "2026-06-11T11:02:34Z" {
		t.Fatalf("scoped start time = %q", session.StartTime)
	}
}

// The native export is an ingestion format: prepare-transcript must convert it
// before storing, never write it to session_ref as-is.
func TestPrepareTranscriptStoresJSONL(t *testing.T) {
	runner := &stubRunner{export: []byte(exportFixture)}
	a := &Agent{CommandRunner: runner}
	ref := filepath.Join(t.TempDir(), "20260611_1.json")
	if err := a.PrepareTranscript(ref); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	if lines := bytes.Count(data, []byte{'\n'}); lines != 4 {
		t.Fatalf("stored %d lines, want 4: %s", lines, data)
	}
	for i, line := range decodeLines(t, data) {
		if compactKind(line) == "" || len(compactMessageContent(line)) == 0 {
			t.Fatalf("stored line %d does not satisfy the compact contract: %s", i, data)
		}
	}
}

// An export this build cannot parse must still be stored, so an unrecognised
// goose version degrades to today's behaviour instead of breaking checkpoints.
func TestPrepareTranscriptStoresUnparseableExportVerbatim(t *testing.T) {
	runner := &stubRunner{export: []byte("not json")}
	a := &Agent{CommandRunner: runner}
	ref := filepath.Join(t.TempDir(), "20260611_1.json")
	if err := a.PrepareTranscript(ref); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "not json" {
		t.Fatalf("stored = %q", data)
	}
}

// Session files written before the JSONL layout existed must keep reading; the
// next export rewrites them.
func TestParseGooseExportAcceptsLegacyDocument(t *testing.T) {
	export, err := parseGooseExport([]byte(exportFixture))
	if err != nil {
		t.Fatal(err)
	}
	if export.ID != "20260611_1" || len(export.Conversation) != 4 || export.Name != "File verification request" {
		t.Fatalf("legacy export = %+v", export.gooseSessionMeta)
	}
}

// A JSONL transcript written before the projection existed carries only the
// native fields. It must still decode, and re-encoding self-heals it.
func TestLegacyJSONLWithoutProjectionStillDecodes(t *testing.T) {
	legacy := []byte(`{"id":"m1","role":"user","created":1781175769,"content":[{"type":"text","text":"old line"}]}` + "\n")
	export, err := parseGooseExport(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(export.Conversation) != 1 || export.Conversation[0].Content[0].Text != "old line" {
		t.Fatalf("legacy decode = %+v", export.Conversation)
	}
	healed, err := encodeSessionJSONL(export)
	if err != nil {
		t.Fatal(err)
	}
	line := decodeLines(t, healed)[0]
	if compactKind(line) != "user" || len(compactMessageContent(line)) == 0 {
		t.Fatalf("re-encoded legacy line did not self-heal: %s", healed)
	}
}

// The projection is derived data: decoding ignores it and yields the native
// conversation unchanged, so a round-trip through the stored layout is lossless.
func TestProjectionIsIgnoredOnDecode(t *testing.T) {
	in, ok := legacyGooseExport([]byte(exportFixture))
	if !ok {
		t.Fatal("exportFixture is not a goose export")
	}
	data, err := encodeSessionJSONL(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseGooseExport(data)
	if err != nil {
		t.Fatal(err)
	}
	// Compared as JSON values: raw tool arguments keep their source
	// whitespace, which is not part of the data.
	if inJSON, outJSON := jsonValue(t, in.Conversation), jsonValue(t, out.Conversation); !reflect.DeepEqual(inJSON, outJSON) {
		t.Fatalf("round-trip lost data:\n in = %v\nout = %v", inJSON, outJSON)
	}
	if !reflect.DeepEqual(in.gooseSessionMeta, out.gooseSessionMeta) {
		t.Fatalf("round-trip lost the session header:\n in = %+v\nout = %+v", in.gooseSessionMeta, out.gooseSessionMeta)
	}
}

// A role Entire has no representation for stays unprojected, so the line is
// dropped rather than emitted empty.
func TestUnknownRolesAreNotProjected(t *testing.T) {
	data, err := encodeSessionJSONL(&gooseExport{Conversation: []gooseMessage{
		{ID: "m1", Role: "system", Content: []gooseContent{{Type: "text", Text: "boot"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	line := decodeLines(t, data)[0]
	if _, ok := line["message"]; ok {
		t.Fatalf("system message was projected: %s", data)
	}
	if kind := compactKind(line); kind != "system" {
		t.Fatalf("compactKind = %q, want system (which Entire drops)", kind)
	}
}

// jsonValue normalizes a Go value to its JSON shape, so comparisons ignore
// source formatting inside embedded json.RawMessage fields.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
