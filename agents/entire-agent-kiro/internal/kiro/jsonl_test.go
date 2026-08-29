package kiro

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sliceFromLine mirrors Entire's transcript.SliceFromLine
// (cmd/entire/cli/transcript/parse.go:130): return the bytes starting after the
// Nth newline, or nil when there aren't that many lines. This is what
// cmd/entire/cli/explain.go:1883 (scopeTranscriptForCheckpoint) and
// cmd/entire/cli/strategy/manual_commit_condensation.go:1006 apply to every
// agent that is not one of Entire's built-ins — kiro included.
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

// compactKind mirrors normalizeKind in Entire's
// cmd/entire/cli/transcript/compact/compact.go:197: the entry kind comes from
// the TOP-LEVEL "type", falling back to "role". A line that yields neither
// "user" nor "assistant" is dropped from the compact transcript entirely.
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
// from a TOP-LEVEL "message" object, never from the line's own payload keys.
// This is the half kiro was missing — its content lived under "user"/"assistant"
// keys of a paired history entry, where Entire does not look.
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

// storedTranscript materializes the committed fixture through the real write
// path (materializeTranscript -> encodeTranscriptJSONL), so every assertion
// below exercises the live encoder rather than a checked-in expected constant.
func storedTranscript(t *testing.T) []byte {
	t.Helper()
	data, ok := materializeTranscript([]byte(testCLIAnalyzerTranscript))
	if !ok {
		t.Fatal("testCLIAnalyzerTranscript did not materialize")
	}
	return data
}

// sourceHistory is the fixture as kiro parses it natively — the ground truth
// every expectation below is derived from.
func sourceHistory(t *testing.T) []kiroHistoryEntry {
	t.Helper()
	parsed, err := parseTranscript([]byte(testCLIAnalyzerTranscript))
	if err != nil {
		t.Fatalf("parseTranscript(fixture) error = %v", err)
	}
	return parsed.History
}

func storedTranscriptFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeStoredLines(t *testing.T, data []byte) []map[string]json.RawMessage {
	t.Helper()
	var out []map[string]json.RawMessage
	for _, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("stored line is not valid JSON: %v (%s)", err, line)
		}
		out = append(out, m)
	}
	return out
}

// mustParseStored reads a stored transcript through the real reader. Stored
// transcripts are JSONL now, so tests that assert on the on-disk file must go
// through parseTranscript rather than unmarshalling one JSON document.
func mustParseStored(t *testing.T, data []byte) kiroTranscript {
	t.Helper()
	parsed, err := parseTranscript(data)
	if err != nil {
		t.Fatalf("parse cached transcript: %v", err)
	}
	return *parsed
}

// --- the two halves of Entire's compact contract ---

func TestStoredTranscriptIsOneJSONObjectPerLine(t *testing.T) {
	data := storedTranscript(t)
	lines := decodeStoredLines(t, data)
	if len(lines) == 0 {
		t.Fatal("stored transcript has no lines")
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("stored transcript must end with a newline so slicing lands on a record boundary")
	}
	// The bug this format replaces: a pretty-printed document whose first line
	// is a bare brace, which SliceFromLine cuts mid-value.
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{\n")) {
		t.Error("stored transcript is a pretty-printed document, not JSONL")
	}
}

func TestStoredLinesSatisfyBothHalvesOfCompactContract(t *testing.T) {
	lines := decodeStoredLines(t, storedTranscript(t))

	kinds := map[string]int{}
	for i, line := range lines {
		kind := compactKind(line)
		if kind == "" {
			// A line with no recognised kind is dropped by normalizeKind, which
			// is deliberate for payloads this build cannot project. It must
			// then carry no "message" either, or it would be an empty envelope.
			if _, ok := line["message"]; ok {
				t.Errorf("line %d has a message wrapper but no kind normalizeKind accepts", i)
			}
			continue
		}
		if kind != "user" && kind != "assistant" {
			t.Errorf("line %d kind = %q, want user or assistant", i, kind)
			continue
		}
		kinds[kind]++

		blocks := compactMessageContent(line)
		if len(blocks) == 0 {
			t.Errorf("line %d (%s) has no content under the message wrapper — an empty envelope", i, kind)
		}
	}

	if kinds["user"] == 0 {
		t.Error("no user line survives normalizeKind")
	}
	if kinds["assistant"] == 0 {
		t.Error("no assistant line survives normalizeKind")
	}
}

// TestContentIsAbsentFromWhereEntireDoesNotLook guards the failure mode that
// made kilo look fixed: the file was JSONL and non-empty, but its content sat
// under keys Entire's compactor never reads, so transcript.jsonl stayed empty.
func TestMessageWrapperCarriesContentNotOnlyNativeKeys(t *testing.T) {
	lines := decodeStoredLines(t, storedTranscript(t))
	for i, line := range lines {
		if compactKind(line) == "" {
			continue
		}
		if _, ok := line["message"]; !ok {
			t.Fatalf("line %d has a kind but no top-level %q object; parseMessage (compact.go:617) would find nothing", i, "message")
		}
	}
}

// --- the paired-entry split ---

func TestPairedHistoryEntryBecomesTwoRecordsSharingAnEntryIndex(t *testing.T) {
	entries := sourceHistory(t)
	lines := decodeStoredLines(t, storedTranscript(t))

	// Every fixture entry has both halves, so the live encoder must emit two
	// records per entry: normalizeKind yields one kind per line, so a paired
	// entry cannot survive as one line without losing a half.
	wantRecords := 0
	for _, e := range entries {
		if len(e.User.Content) > 0 {
			wantRecords++
		}
		if len(e.Assistant) > 0 {
			wantRecords++
		}
	}
	if len(lines) != wantRecords {
		t.Fatalf("stored records = %d, want %d (one per non-empty history half)", len(lines), wantRecords)
	}

	byEntry := map[int][]string{}
	for i, line := range lines {
		var idx int
		if err := json.Unmarshal(line["entry"], &idx); err != nil {
			t.Fatalf("line %d has no entry index: %v", i, err)
		}
		half := "user"
		if _, ok := line["assistant"]; ok {
			half = "assistant"
		}
		byEntry[idx] = append(byEntry[idx], half)
	}
	for idx, halves := range byEntry {
		if !reflect.DeepEqual(halves, []string{"user", "assistant"}) {
			t.Errorf("entry %d halves = %v, want [user assistant] in order", idx, halves)
		}
	}
	if len(byEntry) != len(entries) {
		t.Errorf("distinct entry indices = %d, want %d", len(byEntry), len(entries))
	}
}

// --- content actually survives, not just envelopes ---

func TestEveryUserPromptReachesTheMessageWrapper(t *testing.T) {
	entries := sourceHistory(t)
	stored := storedTranscript(t)

	found := 0
	for _, e := range entries {
		prompt := extractUserPrompt(e.User.Content)
		if prompt == "" {
			continue
		}
		found++
		var seen bool
		for _, line := range decodeStoredLines(t, stored) {
			if compactKind(line) != "user" {
				continue
			}
			for _, block := range compactMessageContent(line) {
				var text string
				if json.Unmarshal(block["text"], &text) == nil && text == prompt {
					seen = true
				}
			}
		}
		if !seen {
			t.Errorf("prompt %q never reaches message.content[].text", prompt)
		}
	}
	if found == 0 {
		t.Fatal("fixture carries no user prompts; the assertion would be vacuous")
	}
}

func TestEveryAssistantResponseReachesTheMessageWrapper(t *testing.T) {
	entries := sourceHistory(t)
	stored := storedTranscript(t)

	found := 0
	for _, e := range entries {
		var response kiroResponseContent
		if json.Unmarshal(e.Assistant, &response) != nil || response.Response.Content == "" {
			continue
		}
		found++
		var seen bool
		for _, line := range decodeStoredLines(t, stored) {
			if compactKind(line) != "assistant" {
				continue
			}
			for _, block := range compactMessageContent(line) {
				var text string
				if json.Unmarshal(block["text"], &text) == nil && text == response.Response.Content {
					seen = true
				}
			}
		}
		if !seen {
			t.Errorf("response %q never reaches message.content[].text", response.Response.Content)
		}
	}
	if found == 0 {
		t.Fatal("fixture carries no assistant responses; the assertion would be vacuous")
	}
}

// TestToolUseBlocksCarryTheFieldsStripAssistantContentKeeps pins the assistant
// tool-call shape to what compact.go:704 preserves: type, id, name, input.
func TestToolUseBlocksCarryTheFieldsStripAssistantContentKeeps(t *testing.T) {
	entries := sourceHistory(t)
	stored := decodeStoredLines(t, storedTranscript(t))

	want := map[string]string{} // tool id -> tool name, from the source
	for _, e := range entries {
		var toolUse kiroToolUseContent
		if json.Unmarshal(e.Assistant, &toolUse) != nil {
			continue
		}
		for _, call := range toolUse.ToolUse.ToolUses {
			want[call.ID] = call.Name
		}
	}
	if len(want) == 0 {
		t.Fatal("fixture carries no tool calls; the assertion would be vacuous")
	}

	got := map[string]string{}
	for _, line := range stored {
		if compactKind(line) != "assistant" {
			continue
		}
		for _, block := range compactMessageContent(line) {
			var typ string
			if json.Unmarshal(block["type"], &typ) != nil || typ != "tool_use" {
				continue
			}
			var id, name string
			_ = json.Unmarshal(block["id"], &id)
			_ = json.Unmarshal(block["name"], &name)
			if _, ok := block["input"]; !ok {
				t.Errorf("tool_use %q has no %q field; stripAssistantContent keeps input, not args", id, "input")
			}
			got[id] = name
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tool_use blocks = %v, want %v", got, want)
	}
}

// TestToolResultBlocksMatchExtractUserContent pins the user-side tool result
// shape to what compact.go:665 reads: snake_case "tool_use_id" and a STRING
// "content". inlineToolResults (compact.go:454) then matches it back onto the
// assistant's tool_use block by that id.
func TestToolResultBlocksMatchExtractUserContent(t *testing.T) {
	entries := sourceHistory(t)
	stored := decodeStoredLines(t, storedTranscript(t))

	want := map[string]string{}
	for _, e := range entries {
		var results kiroToolUseResultsContent
		if json.Unmarshal(e.User.Content, &results) != nil {
			continue
		}
		for _, r := range results.ToolUseResults.ToolUseResults {
			want[r.ID] = toolResultText(r.Result)
		}
	}
	if len(want) == 0 {
		t.Fatal("fixture carries no tool results; the assertion would be vacuous")
	}

	got := map[string]string{}
	for _, line := range stored {
		if compactKind(line) != "user" {
			continue
		}
		for _, block := range compactMessageContent(line) {
			var typ string
			if json.Unmarshal(block["type"], &typ) != nil || typ != "tool_result" {
				continue
			}
			var id, content string
			if err := json.Unmarshal(block["tool_use_id"], &id); err != nil {
				t.Errorf("tool_result has no snake_case %q: %v", "tool_use_id", err)
				continue
			}
			if err := json.Unmarshal(block["content"], &content); err != nil {
				t.Errorf("tool_result %q content is not a JSON string: %v", id, err)
				continue
			}
			got[id] = content
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tool_result blocks = %v, want %v", got, want)
	}
}

// --- line-offset scoping ---

func TestSliceFromLineKeepsEveryScopedSliceValidJSONL(t *testing.T) {
	stored := storedTranscript(t)
	total := countTranscriptLines(stored)
	if total < 2 {
		t.Fatalf("stored transcript has %d lines; need at least 2 to test scoping", total)
	}

	for start := 1; start < total; start++ {
		scoped := sliceFromLine(stored, start)
		if len(scoped) == 0 {
			t.Fatalf("slice from line %d is empty", start)
		}
		if got := countTranscriptLines(scoped); got != total-start {
			t.Errorf("slice from line %d has %d lines, want %d", start, got, total-start)
		}
		parsed, err := decodeTranscriptJSONL(scoped)
		if err != nil {
			t.Fatalf("slice from line %d does not decode: %v", start, err)
		}
		if len(parsed.History) == 0 {
			t.Errorf("slice from line %d decodes to no history", start)
		}
		// Session-level fields are stamped per record, so a header-less slice
		// still carries them.
		if parsed.ConversationID == "" {
			t.Errorf("slice from line %d lost conversation_id", start)
		}
	}
}

func TestScopedSliceStartingMidEntryKeepsTheSurvivingHalf(t *testing.T) {
	stored := storedTranscript(t)
	// Line 1 starts on an assistant record whose user half was cut away.
	scoped := sliceFromLine(stored, 1)
	parsed, err := decodeTranscriptJSONL(scoped)
	if err != nil {
		t.Fatalf("decode mid-entry slice: %v", err)
	}
	if len(parsed.History) == 0 {
		t.Fatal("mid-entry slice decoded to no history")
	}
	first := parsed.History[0]
	if len(first.Assistant) == 0 {
		t.Error("mid-entry slice dropped the assistant half that survived the cut")
	}
	if len(first.User.Content) != 0 {
		t.Error("mid-entry slice invented a user half that was sliced away")
	}
}

func TestGetTranscriptPositionCountsStoredLines(t *testing.T) {
	stored := storedTranscript(t)
	path := storedTranscriptFile(t, stored)

	pos, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition() error = %v", err)
	}
	want := countTranscriptLines(stored)
	if pos != want {
		t.Fatalf("GetTranscriptPosition() = %d, want %d (stored line count)", pos, want)
	}
	// The unit must be lines, not history entries — that is the whole point of
	// the change, and the two differ because entries are split in half.
	if entries := len(sourceHistory(t)); pos == entries {
		t.Fatalf("GetTranscriptPosition() = %d, which is the history-entry count; SliceFromLine consumes LINES", pos)
	}
}

func TestExtractPromptsScopesByStoredLine(t *testing.T) {
	stored := storedTranscript(t)
	path := storedTranscriptFile(t, stored)
	total := countTranscriptLines(stored)

	all, err := New().ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts(0) error = %v", err)
	}
	if len(all) == 0 {
		t.Fatal("ExtractPrompts(0) returned nothing")
	}

	// Scoping by line must agree with what Entire itself would see after
	// SliceFromLine at the same offset.
	for start := 1; start < total; start++ {
		got, err := New().ExtractPrompts(path, start)
		if err != nil {
			t.Fatalf("ExtractPrompts(%d) error = %v", start, err)
		}
		scopedPath := storedTranscriptFile(t, sliceFromLine(stored, start))
		want, err := New().ExtractPrompts(scopedPath, 0)
		if err != nil {
			t.Fatalf("ExtractPrompts on pre-sliced file error = %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ExtractPrompts(offset=%d) = %v, but Entire's own slice yields %v", start, got, want)
		}
	}
}

func TestExtractModifiedFilesScopesByStoredLine(t *testing.T) {
	stored := storedTranscript(t)
	path := storedTranscriptFile(t, stored)

	files, total, err := New().ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles(0) error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("ExtractModifiedFiles(0) found no files")
	}
	if want := countTranscriptLines(stored); total != want {
		t.Errorf("ExtractModifiedFiles total = %d, want %d (stored line count)", total, want)
	}

	// Scoping past the last write must drop it.
	last, _, err := New().ExtractModifiedFiles(path, total)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles(total) error = %v", err)
	}
	if len(last) != 0 {
		t.Errorf("ExtractModifiedFiles at end-of-file = %v, want none", last)
	}
}

// --- round trip and back-compat ---

func TestStoredTranscriptRoundTripsBackToTheSourceHistory(t *testing.T) {
	want := sourceHistory(t)
	got, err := decodeTranscriptJSONL(storedTranscript(t))
	if err != nil {
		t.Fatalf("decodeTranscriptJSONL: %v", err)
	}
	if len(got.History) != len(want) {
		t.Fatalf("round-tripped history = %d entries, want %d", len(got.History), len(want))
	}
	for i := range want {
		// Compare semantically: the source fixture is pretty-printed, so the
		// raw bytes differ by whitespace even when the payload is identical.
		if !sameJSON(t, got.History[i].User.Content, want[i].User.Content) {
			t.Errorf("entry %d user content = %s, want %s", i, got.History[i].User.Content, want[i].User.Content)
		}
		if !sameJSON(t, got.History[i].Assistant, want[i].Assistant) {
			t.Errorf("entry %d assistant = %s, want %s", i, got.History[i].Assistant, want[i].Assistant)
		}
	}
	if got.ConversationID == "" {
		t.Error("round trip lost conversation_id")
	}
}

// TestLegacyWholeDocumentTranscriptStillReads is the back-compat guarantee: a
// transcript written by an older build keeps reading, in the history-entry unit
// the positions recorded against it were taken in. The next capture rewrites it
// as JSONL and the unit moves with the bytes.
func TestLegacyWholeDocumentTranscriptStillReads(t *testing.T) {
	path := storedTranscriptFile(t, []byte(testCLIAnalyzerTranscript))

	pos, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition(legacy) error = %v", err)
	}
	if want := len(sourceHistory(t)); pos != want {
		t.Fatalf("GetTranscriptPosition(legacy) = %d, want %d history entries", pos, want)
	}

	prompts, err := New().ExtractPrompts(path, 0)
	if err != nil {
		t.Fatalf("ExtractPrompts(legacy) error = %v", err)
	}
	if len(prompts) == 0 {
		t.Error("legacy transcript yielded no prompts")
	}

	files, _, err := New().ExtractModifiedFiles(path, 0)
	if err != nil {
		t.Fatalf("ExtractModifiedFiles(legacy) error = %v", err)
	}
	if len(files) == 0 {
		t.Error("legacy transcript yielded no modified files")
	}
}

func TestPlaceholderTranscriptStillReportsZero(t *testing.T) {
	path := storedTranscriptFile(t, []byte("{}"))
	pos, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatalf("GetTranscriptPosition(placeholder) error = %v", err)
	}
	if pos != 0 {
		t.Fatalf("GetTranscriptPosition(placeholder) = %d, want 0", pos)
	}
}

func TestMaterializeLeavesUnrecognisedDocumentsAlone(t *testing.T) {
	for _, in := range []string{"{}", "", "not json at all", `{"history":[]}`} {
		if _, ok := materializeTranscript([]byte(in)); ok {
			t.Errorf("materializeTranscript(%q) claimed to convert an unrecognised document", in)
		}
	}
}

func TestSessionFieldsAreStampedOnEveryRecord(t *testing.T) {
	for i, line := range decodeStoredLines(t, storedTranscript(t)) {
		var conv string
		if json.Unmarshal(line["conversation_id"], &conv) != nil || conv == "" {
			t.Errorf("line %d has no conversation_id; a scoped slice past line 0 would lose it", i)
		}
	}
}

func TestIDETranscriptAlsoMaterializesAsJSONL(t *testing.T) {
	data, ok := materializeTranscript([]byte(testIDEAnalyzerTranscript))
	if !ok {
		t.Fatal("IDE transcript did not materialize")
	}
	lines := decodeStoredLines(t, data)
	if len(lines) == 0 {
		t.Fatal("IDE transcript materialized to no lines")
	}
	var withContent int
	for _, line := range lines {
		if compactKind(line) != "" && len(compactMessageContent(line)) > 0 {
			withContent++
		}
	}
	if withContent == 0 {
		t.Fatal("no IDE line survives both halves of the compact contract")
	}
	if !strings.Contains(string(data), "Open the workspace") {
		t.Error("IDE prompt text did not survive materialization")
	}
}

// sameJSON reports whether two raw JSON payloads are semantically equal,
// ignoring formatting.
func sameJSON(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// TestEnsureCachedTranscriptStoresJSONLOnDisk is the end-to-end guard on the
// write path: it is not enough for the encoder to be correct, the capture path
// has to actually run the transcript through it before writing. Without that
// call the file on disk stays a single JSON document and every checkpoint's
// transcript.jsonl is empty again.
func TestEnsureCachedTranscriptStoresJSONLOnDisk(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repoRoot)
	t.Setenv("HOME", home)

	stubData := buildTranscript("cli-session", 3)
	createFakeKiroDB(t, home)
	restore := stubSQLiteRunner(t, func(_ ...string) ([]byte, error) {
		return stubData, nil
	})
	defer restore()

	cachePath, err := New().ensureCachedTranscript(repoRoot, "stable-session", "")
	if err != nil {
		t.Fatalf("ensureCachedTranscript() error = %v", err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached transcript: %v", err)
	}

	// The stored file must be JSONL, one record per line...
	lines := decodeStoredLines(t, data)
	if len(lines) < 2 {
		t.Fatalf("stored transcript has %d lines; a single JSON document is what this replaces", len(lines))
	}
	// ...and every content-bearing line must satisfy both halves of Entire's
	// compact contract.
	survivors := 0
	for i, line := range lines {
		kind := compactKind(line)
		if kind == "" {
			continue
		}
		if len(compactMessageContent(line)) == 0 {
			t.Errorf("stored line %d (%s) is an empty envelope", i, kind)
			continue
		}
		survivors++
	}
	if survivors == 0 {
		t.Fatal("no stored line satisfies normalizeKind + parseMessage")
	}

	// And the position Entire records must be the stored line count, because
	// that is the unit SliceFromLine consumes.
	pos, err := New().GetTranscriptPosition(cachePath)
	if err != nil {
		t.Fatalf("GetTranscriptPosition() error = %v", err)
	}
	if pos != len(lines) {
		t.Fatalf("GetTranscriptPosition() = %d, want %d stored lines", pos, len(lines))
	}
	if !strings.Contains(string(data), "prompt 0") {
		t.Error("prompt text did not survive the write path")
	}
}
