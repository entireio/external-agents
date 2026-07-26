package omp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

func ompTitleSlot() string {
	prefix := `{"type":"title","v":1,"title":"test","pad":"`
	suffix := `"}`
	return prefix + strings.Repeat(" ", 255-len(prefix)-len(suffix)) + suffix + "\n"
}

func ompHeader() string {
	return `{"type":"session","version":3,"id":"session-header","timestamp":"2026-07-26T03:14:06.592Z","cwd":"/repo/from/header"}` + "\n"
}

func ompBranchFixture() []byte {
	return []byte(ompTitleSlot() + ompHeader() +
		`{"type":"model_change","id":"model","parentId":null,"timestamp":"2026-07-26T03:14:07Z","model":"openai/gpt"}` + "\n" +
		`{"type":"message","id":"excluded","parentId":"model","timestamp":"2026-07-26T03:14:08Z","message":{"role":"user","content":"excluded branch"}}` + "\n" +
		`{"type":"message","id":"user","parentId":"model","timestamp":"2026-07-26T03:14:09Z","message":{"role":"user","content":[{"type":"text","text":"keep one"},{"type":"image","data":"x"},{"type":"text","text":"keep two"}]}}` + "\n" +
		`{"type":"message","id":"assistant","parentId":"user","timestamp":"2026-07-26T03:14:10Z","message":{"role":"assistant","model":"openai/gpt-5","content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"summary"},{"type":"toolCall","id":"write-1","name":"write","arguments":{"path":"a.go"}},{"type":"toolCall","id":"edit-1","name":"Edit","arguments":{"file_path":"b.go"}},{"type":"toolCall","id":"write-2","name":"write","arguments":{"path":"a.go"}},{"type":"toolCall","id":"internal-write","name":"write","arguments":{"path":"xd://report_issue"}}],"usage":{"input":12,"output":7}}}` + "\n" +
		`{"type":"custom_message","customType":"notice","content":"tail","id":"tail","parentId":"assistant","timestamp":"2026-07-26T03:14:11Z"}` + "\n")
}

func writeOMPFixture(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSessionTitleHeaderAndActiveLeaf(t *testing.T) {
	session, err := parseSession(ompBranchFixture())
	if err != nil {
		t.Fatal(err)
	}
	if session.Header.ID != "session-header" || session.Header.Cwd != "/repo/from/header" {
		t.Fatalf("unexpected header: %+v", session.Header)
	}
	if session.Lines != 7 {
		t.Fatalf("lines = %d, want 7", session.Lines)
	}
	var ids []string
	for _, entry := range session.Active {
		ids = append(ids, entry.Entry.ID)
	}
	if want := []string{"model", "user", "assistant", "tail"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("active IDs = %v, want %v", ids, want)
	}
}

func TestParseSessionWithoutTitleAndMalformedLaterLines(t *testing.T) {
	data := []byte(ompHeader() +
		`{"type":"message","id":"user","parentId":null,"message":{"role":"user","content":"hello"}}` + "\n" +
		"{broken\n" +
		`{"type":"message","id":"assistant","parentId":"user","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}` + "\n" +
		"not json\n")
	session, err := parseSession(data)
	if err != nil {
		t.Fatal(err)
	}
	if session.Lines != 5 || len(session.Active) != 2 || session.Active[1].Entry.ID != "assistant" {
		t.Fatalf("unexpected parse result: lines=%d active=%+v", session.Lines, session.Active)
	}
}

func TestParseSessionCycleGuard(t *testing.T) {
	data := []byte(ompHeader() +
		`{"type":"custom","id":"a","parentId":"b"}` + "\n" +
		`{"type":"custom","id":"b","parentId":"a"}` + "\n")
	session, err := parseSession(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Active) != 2 {
		t.Fatalf("cycle branch length = %d, want 2", len(session.Active))
	}
}

func TestParseSessionRejectsMalformedHeaderAndOversizedLine(t *testing.T) {
	if _, err := parseSession([]byte(`{"type":"title"}` + "\n" + `{"type":"message"}` + "\n")); err == nil {
		t.Fatal("expected malformed header error")
	}
	prefix := `{"type":"custom","id":"large","parentId":null,"value":"`
	suffix := `"}`
	atLimit := ompHeader() + prefix + strings.Repeat("x", maxSessionLine-len(prefix)-len(suffix)) + suffix + "\n"
	if _, err := parseSession([]byte(atLimit)); err != nil {
		t.Fatalf("line at limit rejected: %v", err)
	}
	oversized := append([]byte(ompHeader()), bytes.Repeat([]byte("x"), maxSessionLine+1)...)
	if _, err := parseSession(oversized); err == nil {
		t.Fatal("expected scanner size error")
	}
}

func TestAnalyzerUsesPhysicalOffsetsAndActiveBranch(t *testing.T) {
	path := writeOMPFixture(t, ompBranchFixture())
	agent := &Agent{}
	position, err := agent.GetTranscriptPosition(path)
	if err != nil || position != 7 {
		t.Fatalf("position = %d, err = %v", position, err)
	}
	prompts, err := agent.ExtractPrompts(path, 2)
	if err != nil || !reflect.DeepEqual(prompts, []string{"keep one\nkeep two"}) {
		t.Fatalf("prompts = %v, err = %v", prompts, err)
	}
	if prompts, err = agent.ExtractPrompts(path, 5); err != nil || len(prompts) != 0 {
		t.Fatalf("offset prompts = %v, err = %v", prompts, err)
	}
	files, current, err := agent.ExtractModifiedFiles(path, 0)
	if err != nil || current != 7 || !reflect.DeepEqual(files, []string{"a.go", "b.go"}) {
		t.Fatalf("files = %v, current = %d, err = %v", files, current, err)
	}
	if files, _, err = agent.ExtractModifiedFiles(path, 6); err != nil || len(files) != 0 {
		t.Fatalf("offset files = %v, err = %v", files, err)
	}
	summary, ok, err := agent.ExtractSummary(path)
	if err != nil || !ok || summary != "summary" {
		t.Fatalf("summary = %q, ok = %v, err = %v", summary, ok, err)
	}
	if model := extractModelFromPath(path); model != "openai/gpt-5" {
		t.Fatalf("model = %q", model)
	}
}

func TestModifiedFilesExtractsApplyPatchFreeformPaths(t *testing.T) {
	patch := "*** Begin Patch\n" +
		"*** Add File: added.go\n+package added\n" +
		"*** Update File: old.go\n*** Move to: renamed.go\n@@\n-old\n+new\n" +
		"*** Delete File: deleted.go\n" +
		"*** Update File: added.go\n@@\n-old\n+new\n" +
		"*** Add File: xd://report_issue\n+ignored\n" +
		"*** End Patch\n"
	arguments, err := json.Marshal(map[string]string{"input": patch})
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal([]contentBlock{{
		Type:      "toolCall",
		Name:      "apply_patch",
		Arguments: arguments,
	}})
	if err != nil {
		t.Fatal(err)
	}
	entries := []parsedEntry{{
		Line: 2,
		Entry: sessionEntry{Message: &sessionMessage{
			Role:    "assistant",
			Content: content,
		}},
	}}

	if got, want := modifiedFiles(entries, 0), []string{"added.go", "old.go", "renamed.go", "deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("modified files = %v, want %v", got, want)
	}
	if got := modifiedFiles(entries, 2); len(got) != 0 {
		t.Fatalf("offset modified files = %v, want none", got)
	}
}

func TestModifiedFilesIgnoresMalformedApplyPatchInput(t *testing.T) {
	tests := []string{
		"ordinary text\n*** Update File: ordinary.go\n",
		"*** Begin Patch\n*** Delete File: missing-end.go\n",
		"*** Begin Patch\n*** End Patch\ntrailing text",
		"<<EOF\n*** Begin Patch\n*** Delete File: unclosed.go\n*** End Patch",
		"<<'EOF'\n*** Begin Patch\n*** Delete File: mismatched.go\n*** End Patch\nNOT_EOF",
		"<<EOF\n*** Begin Patch\n*** Delete File: mismatched.go\n*** End Patch\n'EOF'",
		"*** Begin Patch\n*** Move to: orphan.go\n*** End Patch\n",
		"*** Begin Patch\n*** Update File: \n*** Move to: orphan.go\n*** End Patch\n",
		"*** Begin Patch\n*** Add File: \n*** Update File:\n*** End Patch\n",
	}
	for _, input := range tests {
		arguments, err := json.Marshal(map[string]string{"input": input})
		if err != nil {
			t.Fatal(err)
		}
		content, err := json.Marshal([]contentBlock{{
			Type:      "toolCall",
			Name:      "apply_patch",
			Arguments: arguments,
		}})
		if err != nil {
			t.Fatal(err)
		}
		entries := []parsedEntry{{
			Line: 1,
			Entry: sessionEntry{Message: &sessionMessage{
				Role:    "assistant",
				Content: content,
			}},
		}}
		if got := modifiedFiles(entries, 0); len(got) != 0 {
			t.Errorf("input %q produced modified files %v", input, got)
		}
	}
}

func TestPatchPathsAcceptsOfficialWrappersAndWhitespace(t *testing.T) {
	bare := "\n \t*** Begin Patch \n\t*** Add File: bare.go \t\n *** End Patch\t\n\n"
	if got, want := patchPaths(bare), []string{"bare.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("bare envelope paths = %v, want %v", got, want)
	}

	for _, opener := range []string{"<<EOF", "<<'EOF'", `<<"EOF"`} {
		input := " \t" + opener + "\n" +
			"  *** Begin Patch \t\n" +
			"\t*** Add File: added.go  \n" +
			" \t*** Update File: old.go\t\n" +
			"*** Move to: renamed.go\n" +
			"@@\n-old\n+new\n" +
			"*** Delete File: deleted.go \n" +
			" *** End Patch\t\n" +
			" EOF \n"
		want := []string{"added.go", "old.go", "renamed.go", "deleted.go"}
		if got := patchPaths(input); !reflect.DeepEqual(got, want) {
			t.Errorf("opener %q: paths = %v, want %v", opener, got, want)
		}
	}
}

func TestTrimECMAScriptSpace(t *testing.T) {
	for _, r := range []rune{
		'\t', '\v', '\f', '\n', '\r',
		' ', '\u00A0', '\u1680',
		'\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005',
		'\u2006', '\u2007', '\u2008', '\u2009', '\u200A',
		'\u2028', '\u2029', '\u202F', '\u205F', '\u3000', '\uFEFF',
	} {
		if got := trimECMAScriptSpace(string(r) + "value" + string(r)); got != "value" {
			t.Errorf("U+%04X was not trimmed: %q", r, got)
		}
	}

	for _, r := range []rune{'\u0085', '\u180E', '\u200B'} {
		want := string(r) + "value" + string(r)
		if got := trimECMAScriptSpace(want); got != want {
			t.Errorf("U+%04X was trimmed: %q", r, got)
		}
	}
}

func TestPatchPathsUsesECMAScriptTrimCharacters(t *testing.T) {
	input := "\uFEFF<<EOF\n" +
		"\uFEFF*** Begin Patch\uFEFF\n" +
		"\uFEFF\n" +
		"\uFEFF*** Add File: \uFEFFleading-feff.go\uFEFF\n" +
		"+package added\n" +
		"\uFEFF*** End Patch\uFEFF\n" +
		"\uFEFFEOF\uFEFF"
	if got, want := patchPaths(input), []string{"\uFEFFleading-feff.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}

	for _, input := range []string{
		"\u0085*** Begin Patch\n*** Delete File: file.go\n*** End Patch",
		"*** Begin Patch\n\u0085\n*** Delete File: file.go\n*** End Patch",
		"*** Begin Patch\n\u0085*** Delete File: file.go\n*** End Patch",
		"*** Begin Patch\n*** Delete File: file.go\n*** End Patch\u0085",
	} {
		if got := patchPaths(input); got != nil {
			t.Errorf("input containing U+0085 produced paths %q, want nil", got)
		}
	}
}

func TestPatchPathsMatchesApplyPatchStateMachine(t *testing.T) {
	t.Run("body markers and column-zero move", func(t *testing.T) {
		input := "*** Begin Patch\n" +
			"*** Update File: old.go\n" +
			"\t*** Move to: body-marker.go\n" +
			" *** Add File: body-add.go\n" +
			" *** End Patch\n" +
			"*** Update File: next.go\n" +
			"*** Move to: moved.go\n" +
			"@@\n-old\n+new\n" +
			"*** End Patch"
		want := []string{"old.go", "next.go", "moved.go"}
		if got := patchPaths(input); !reflect.DeepEqual(got, want) {
			t.Fatalf("paths = %q, want %q", got, want)
		}
	})

	t.Run("leading path space is preserved", func(t *testing.T) {
		input := "*** Begin Patch\n*** Add File:  spaced.go\n+package spaced\n*** End Patch"
		if got, want := patchPaths(input), []string{" spaced.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("paths = %q, want %q", got, want)
		}
	})

	t.Run("malformed trailing hunk is atomic", func(t *testing.T) {
		input := "*** Begin Patch\n" +
			"*** Add File: valid.go\n+package valid\n" +
			"*** Update File: empty.go\n" +
			"*** Delete File: never-returned.go\n" +
			"*** End Patch"
		if got := patchPaths(input); got != nil {
			t.Fatalf("paths = %q, want nil", got)
		}
	})

	t.Run("unknown top-level text is atomic", func(t *testing.T) {
		input := "*** Begin Patch\n" +
			"*** Delete File: valid.go\n" +
			"not a hunk\n" +
			"*** End Patch"
		if got := patchPaths(input); got != nil {
			t.Fatalf("paths = %q, want nil", got)
		}
	})

	t.Run("add consumes only plus lines", func(t *testing.T) {
		input := "*** Begin Patch\n" +
			"*** Add File: valid.go\n+package valid\n" +
			"context is not add-file content\n" +
			"*** Delete File: never-returned.go\n" +
			"*** End Patch"
		if got := patchPaths(input); got != nil {
			t.Fatalf("paths = %q, want nil", got)
		}
	})
}

func TestModifiedFilesPreservesApplyPatchPathButNormalizesWritePath(t *testing.T) {
	patchArguments, err := json.Marshal(map[string]string{
		"input": "*** Begin Patch\n*** Add File:  spaced.go\n+package spaced\n*** End Patch",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeArguments, err := json.Marshal(map[string]string{"path": " normalized.go "})
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal([]contentBlock{
		{Type: "toolCall", Name: "apply_patch", Arguments: patchArguments},
		{Type: "toolCall", Name: "write", Arguments: writeArguments},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := []parsedEntry{{
		Line: 1,
		Entry: sessionEntry{Message: &sessionMessage{
			Role:    "assistant",
			Content: content,
		}},
	}}

	want := []string{" spaced.go", "normalized.go"}
	if got := modifiedFiles(entries, 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("modified files = %q, want %q", got, want)
	}
}

func TestAnalyzerAcceptsHeaderlessDataAndMissingPosition(t *testing.T) {
	agent := &Agent{}
	path := writeOMPFixture(t, []byte("{}\n"))
	if position, err := agent.GetTranscriptPosition(path); err != nil || position != 1 {
		t.Fatalf("position = %d, err = %v", position, err)
	}
	if prompts, err := agent.ExtractPrompts(path, 0); err != nil || len(prompts) != 0 {
		t.Fatalf("prompts = %v, err = %v", prompts, err)
	}
	if files, position, err := agent.ExtractModifiedFiles(path, 0); err != nil || position != 1 || len(files) != 0 {
		t.Fatalf("files = %v, position = %d, err = %v", files, position, err)
	}
	if summary, ok, err := agent.ExtractSummary(path); err != nil || ok || summary != "" {
		t.Fatalf("summary = %q, ok = %v, err = %v", summary, ok, err)
	}
	if position, err := agent.GetTranscriptPosition(filepath.Join(t.TempDir(), "missing")); err != nil || position != 0 {
		t.Fatalf("missing position = %d, err = %v", position, err)
	}
}

func TestReadSessionUsesHeaderAuthorityAndOpaqueFallback(t *testing.T) {
	agent := &Agent{}
	if _, err := agent.ReadSession(nil); err == nil {
		t.Fatal("expected nil input error")
	}
	path := writeOMPFixture(t, ompBranchFixture())
	session, err := agent.ReadSession(&protocol.HookInputJSON{SessionID: "wrong", SessionRef: path})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "session-header" || session.RepoPath != "/repo/from/header" || session.StartTime != "2026-07-26T03:14:06.592Z" {
		t.Fatalf("header was not authoritative: %+v", session)
	}
	if !bytes.Equal(session.NativeData, ompBranchFixture()) || !reflect.DeepEqual(session.ModifiedFiles, []string{"a.go", "b.go"}) {
		t.Fatalf("unexpected native session: %+v", session)
	}
	opaque := writeOMPFixture(t, []byte(`{"test":true}`))
	session, err = agent.ReadSession(&protocol.HookInputJSON{SessionID: "opaque-id", SessionRef: opaque})
	if err != nil || session.SessionID != "opaque-id" || len(session.NativeData) == 0 || session.StartTime == "" {
		t.Fatalf("opaque session = %+v, err = %v", session, err)
	}
	if session.ModifiedFiles == nil || session.NewFiles == nil || session.DeletedFiles == nil {
		t.Fatalf("file slices must be initialized: %+v", session)
	}
}

func TestWriteSessionAtomicPrivateAndReadTranscriptOpaque(t *testing.T) {
	agent := &Agent{}
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "session.jsonl")
	if err := agent.WriteSession(protocol.AgentSessionJSON{SessionRef: path, NativeData: []byte(`{"test":true}`)}); err != nil {
		t.Fatal(err)
	}
	data, err := agent.ReadTranscript(path)
	if err != nil || string(data) != `{"test":true}` {
		t.Fatalf("data = %q, err = %v", data, err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("directory mode = %v", mode)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %v", mode)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.WriteSession(protocol.AgentSessionJSON{SessionRef: path, NativeData: []byte("replacement")}); err != nil {
		t.Fatal(err)
	}
	if data, err = os.ReadFile(path); err != nil || string(data) != "replacement" {
		t.Fatalf("replacement = %q, err = %v", data, err)
	}
	replacementInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := replacementInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("replacement mode = %v", mode)
	}
}

func TestChunkAndReassembleBoundaries(t *testing.T) {
	agent := &Agent{}
	if chunks, err := agent.ChunkTranscript(nil, 3); err != nil || len(chunks) != 0 {
		t.Fatalf("empty chunks = %v, err = %v", chunks, err)
	}
	if _, err := agent.ChunkTranscript([]byte("x"), 0); err == nil {
		t.Fatal("expected nonpositive max-size error")
	}
	chunks, err := agent.ChunkTranscript([]byte("abcdef"), 3)
	if err != nil || len(chunks) != 2 || string(chunks[0]) != "abc" || string(chunks[1]) != "def" {
		t.Fatalf("chunks = %q, err = %v", chunks, err)
	}
	data, err := agent.ReassembleTranscript(chunks)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("reassembled = %q, err = %v", data, err)
	}
}
