package kilo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

// restoreSessionID and restoreExport mirror what `kilo export` prints on Kilo
// 7.8.27 (trimmed): the envelope plus messages carrying fields SessionMessage
// does not model, all of which `kilo import` requires.
const restoreSessionID = "ses_e12fdb4b7ffe83qgNNrXYWgaM9"

const restoreExport = `{
  "info": {
    "id": "ses_e12fdb4b7ffe83qgNNrXYWgaM9",
    "slug": "neon-squid",
    "projectID": "d0780c7d4b74bf61609c99bf30bb90f0b17cc0eb",
    "directory": "/home/dev/repo",
    "path": "",
    "title": "Say hello <b> & greet",
    "version": "7.8.27",
    "time": {"created": 1794977712968, "updated": 1794977718291}
  },
  "messages": [
    {
      "info": {"role": "user", "time": {"created": 1794977716568}, "agent": "code",
        "model": {"providerID": "mock", "modelID": "mock-model"}, "summary": {"diffs": []},
        "id": "msg_1ed025958001JJQ6gsM3v1UUij", "sessionID": "ses_e12fdb4b7ffe83qgNNrXYWgaM9"},
      "parts": [{"type": "text", "text": "say hello", "id": "prt_1ed025964001UqK570DY5fs27J",
        "sessionID": "ses_e12fdb4b7ffe83qgNNrXYWgaM9", "messageID": "msg_1ed025958001JJQ6gsM3v1UUij"}]
    },
    {
      "info": {"parentID": "msg_1ed025958001JJQ6gsM3v1UUij", "role": "assistant", "mode": "code", "agent": "code",
        "path": {"cwd": "/home/dev/repo", "root": "/home/dev/repo"}, "cost": 0,
        "tokens": {"total": 18, "input": 11, "output": 7, "reasoning": 0, "cache": {"write": 0, "read": 0}},
        "modelID": "mock-model", "providerID": "mock", "time": {"created": 1794977717985, "completed": 1794977718250},
        "finish": "stop", "id": "msg_1ed025ee100160iiZOlQO6wbqL", "sessionID": "ses_e12fdb4b7ffe83qgNNrXYWgaM9"},
      "parts": [
        {"snapshot": "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "type": "step-start", "time": {"start": 1794977718163},
          "id": "prt_1ed025f93001e7c7131hYBQF7g", "sessionID": "ses_e12fdb4b7ffe83qgNNrXYWgaM9", "messageID": "msg_1ed025ee100160iiZOlQO6wbqL"},
        {"type": "text", "text": "hello <there> & welcome", "time": {"start": 1794977718166, "end": 1794977718170},
          "id": "prt_1ed025f96001j0gESH1UHknXaG", "sessionID": "ses_e12fdb4b7ffe83qgNNrXYWgaM9", "messageID": "msg_1ed025ee100160iiZOlQO6wbqL"}
      ]
    }
  ]
}`

func restoreTranscript(t *testing.T) []byte {
	t.Helper()
	export, err := parseKiloExport([]byte(restoreExport))
	if err != nil {
		t.Fatalf("parseKiloExport: %v", err)
	}
	data, err := encodeExportJSONL(export)
	if err != nil {
		t.Fatalf("encodeExportJSONL: %v", err)
	}
	return data
}

// assertSameJSON fails unless got and want decode to the same value.
func assertSameJSON(t *testing.T, label string, got, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: got is not JSON: %v (%s)", label, err, got)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("%s: want is not JSON: %v", label, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s differs:\n got: %s\nwant: %s", label, got, want)
	}
}

func TestEncodeExportJSONLKeepsKiloMessagesWhole(t *testing.T) {
	t.Parallel()
	data := restoreTranscript(t)
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("want one line per message, got %d:\n%s", len(lines), data)
	}

	var export kiloExport
	if err := json.Unmarshal([]byte(restoreExport), &export); err != nil {
		t.Fatal(err)
	}
	var first map[string]json.RawMessage
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("first line: %v", err)
	}
	envelope, ok := first[sessionEnvelopeKey]
	if !ok {
		t.Fatalf("first line has no %s: %s", sessionEnvelopeKey, lines[0])
	}
	assertSameJSON(t, "envelope", envelope, export.Info)
	delete(first, sessionEnvelopeKey)
	firstMsg, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, "message 0", firstMsg, export.Messages[0])
	if bytes.Contains(lines[1], []byte(sessionEnvelopeKey)) {
		t.Fatalf("envelope repeated on a later line: %s", lines[1])
	}
	assertSameJSON(t, "message 1", lines[1], export.Messages[1])
	// No HTML escaping: Kilo gets its own bytes back.
	if !bytes.Contains(data, []byte("hello <there> & welcome")) {
		t.Fatalf("text was re-escaped:\n%s", data)
	}
}

func TestEnvelopeLineKeepsLineIndexEqualToMessageIndex(t *testing.T) {
	t.Parallel()
	data := restoreTranscript(t)
	messages, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if len(messages) != 2 || messages[0].Info.ID != "msg_1ed025958001JJQ6gsM3v1UUij" {
		t.Fatalf("decoded %+v", messages)
	}
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	pos, err := New().GetTranscriptPosition(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := bytes.Count(data, []byte{'\n'}); pos != lines {
		t.Fatalf("position %d != line count %d", pos, lines)
	}
	// The slice Entire takes for the second turn starts at the assistant
	// message and still compacts.
	compacted, err := compactTranscriptBytes(sliceFromLine(data, 1))
	if err != nil {
		t.Fatalf("compact slice: %v", err)
	}
	if !bytes.Contains(compacted, []byte("welcome")) {
		t.Fatalf("slice lost the assistant message: %s", compacted)
	}
}

func TestExportFromTranscriptRebuildsKiloExport(t *testing.T) {
	t.Parallel()
	got, ok, err := exportFromTranscript(restoreTranscript(t))
	if err != nil || !ok {
		t.Fatalf("exportFromTranscript ok=%v err=%v", ok, err)
	}
	blob, err := marshalNoHTMLEscape(got)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, "export", blob, []byte(restoreExport))
}

func TestExportFromTranscriptNeedsEnvelope(t *testing.T) {
	t.Parallel()
	legacy, err := encodeMessagesJSONL([]SessionMessage{makeTextMessage("m1", MessageRoleUser, "hi")})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"legacy": legacy, "empty": nil, "garbage": []byte("not json\n")} {
		if _, ok, err := exportFromTranscript(data); ok || err != nil {
			t.Fatalf("%s: ok=%v err=%v, want not importable", name, ok, err)
		}
	}
}

func TestPrepareTranscriptStoresImportableExport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ref := filepath.Join(dir, "s.json")
	seed, err := encodeMessagesJSONL([]SessionMessage{{Info: MessageInfo{ID: "m0", Role: MessageRoleUser, SessionID: restoreSessionID}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{CommandRunner: &fakeRestoreRunner{export: []byte(restoreExport)}}
	if err := agent.PrepareTranscript(ref); err != nil {
		t.Fatalf("PrepareTranscript: %v", err)
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := exportFromTranscript(data)
	if err != nil || !ok {
		t.Fatalf("stored transcript not importable: ok=%v err=%v", ok, err)
	}
	blob, err := marshalNoHTMLEscape(got)
	if err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, "stored export", blob, []byte(restoreExport))
}

type fakeRestoreRunner struct {
	export     []byte
	has        bool
	hasErr     error
	importErr  error
	hasCalls   []string
	imports    []string
	importDirs []string
	imported   [][]byte
}

func (f *fakeRestoreRunner) ExportSession(context.Context, string) ([]byte, error) {
	return f.export, nil
}

func (f *fakeRestoreRunner) HasSession(_ context.Context, dir, sessionID string) (bool, error) {
	f.hasCalls = append(f.hasCalls, dir+"|"+sessionID)
	return f.has, f.hasErr
}

func (f *fakeRestoreRunner) ImportSession(_ context.Context, dir, exportPath string) error {
	data, err := os.ReadFile(exportPath)
	if err != nil {
		return err
	}
	f.imports = append(f.imports, exportPath)
	f.importDirs = append(f.importDirs, dir)
	f.imported = append(f.imported, data)
	return f.importErr
}

func restoredSession(t *testing.T, nativeData []byte) protocol.AgentSessionJSON {
	t.Helper()
	repo := t.TempDir()
	return protocol.AgentSessionJSON{
		SessionID:  restoreSessionID,
		AgentName:  "kilo",
		RepoPath:   repo,
		SessionRef: filepath.Join(repo, ".entire", "tmp", "kilo", restoreSessionID+".json"),
		NativeData: nativeData,
	}
}

func assertTranscriptWritten(t *testing.T, session protocol.AgentSessionJSON) {
	t.Helper()
	got, err := os.ReadFile(session.SessionRef)
	if err != nil {
		t.Fatalf("transcript not written: %v", err)
	}
	if !bytes.Equal(got, session.NativeData) {
		t.Fatalf("transcript = %q, want the restored bytes", got)
	}
}

func TestWriteSessionImportsWhenKiloLacksSession(t *testing.T) {
	t.Parallel()
	runner := &fakeRestoreRunner{}
	session := restoredSession(t, restoreTranscript(t))
	if err := (&Agent{CommandRunner: runner}).WriteSession(session); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	assertTranscriptWritten(t, session)
	if len(runner.hasCalls) != 1 || runner.hasCalls[0] != session.RepoPath+"|"+restoreSessionID {
		t.Fatalf("HasSession calls = %v", runner.hasCalls)
	}
	if len(runner.imported) != 1 {
		t.Fatalf("ImportSession calls = %d, want 1", len(runner.imported))
	}
	if runner.importDirs[0] != session.RepoPath {
		t.Fatalf("import ran in %q, want the repo %q", runner.importDirs[0], session.RepoPath)
	}
	assertSameJSON(t, "imported export", runner.imported[0], []byte(restoreExport))
	if _, err := os.Stat(runner.imports[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("import file %s left behind (stat err %v)", runner.imports[0], err)
	}
}

func TestWriteSessionLeavesSessionKiloAlreadyHas(t *testing.T) {
	t.Parallel()
	runner := &fakeRestoreRunner{has: true}
	session := restoredSession(t, restoreTranscript(t))
	if err := (&Agent{CommandRunner: runner}).WriteSession(session); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	assertTranscriptWritten(t, session)
	if len(runner.imported) != 0 {
		t.Fatalf("imported a session Kilo already holds")
	}
}

func TestWriteSessionSkipsImportForTranscriptWithoutEnvelope(t *testing.T) {
	t.Parallel()
	legacy, err := encodeMessagesJSONL([]SessionMessage{makeTextMessage("m1", MessageRoleUser, "hi")})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRestoreRunner{}
	session := restoredSession(t, legacy)
	if err := (&Agent{CommandRunner: runner}).WriteSession(session); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	assertTranscriptWritten(t, session)
	if len(runner.hasCalls) != 0 || len(runner.imported) != 0 {
		t.Fatalf("ran kilo for a transcript it cannot import: has=%v imports=%d", runner.hasCalls, len(runner.imported))
	}
}

func TestWriteSessionSkipsImportWithoutKilo(t *testing.T) {
	t.Parallel()
	runner := &fakeRestoreRunner{hasErr: &exec.Error{Name: "kilo", Err: exec.ErrNotFound}}
	session := restoredSession(t, restoreTranscript(t))
	if err := (&Agent{CommandRunner: runner}).WriteSession(session); err != nil {
		t.Fatalf("WriteSession without kilo: %v", err)
	}
	assertTranscriptWritten(t, session)
	if len(runner.imported) != 0 {
		t.Fatal("imported without kilo")
	}
}

func TestWriteSessionReportsFailedImport(t *testing.T) {
	t.Parallel()
	runner := &fakeRestoreRunner{importErr: errors.New("kilo import: exit status 1: Missing key at [\"slug\"]")}
	session := restoredSession(t, restoreTranscript(t))
	err := (&Agent{CommandRunner: runner}).WriteSession(session)
	if err == nil || !strings.Contains(err.Error(), restoreSessionID) || !strings.Contains(err.Error(), "slug") {
		t.Fatalf("WriteSession error = %v, want the failed import named", err)
	}
	assertTranscriptWritten(t, session)
}

func TestWriteSessionRefusesEnvelopeIDThatReadsAsOption(t *testing.T) {
	t.Parallel()
	transcript := bytes.Replace(restoreTranscript(t), []byte(`"id":"`+restoreSessionID+`"`), []byte(`"id":"--help"`), 1)
	runner := &fakeRestoreRunner{}
	session := restoredSession(t, transcript)
	session.SessionID = ""
	err := (&Agent{CommandRunner: runner}).WriteSession(session)
	if err == nil || !strings.Contains(err.Error(), "--help") {
		t.Fatalf("WriteSession error = %v, want the bad id refused", err)
	}
	if len(runner.hasCalls) != 0 || len(runner.imported) != 0 {
		t.Fatal("ran kilo with an id that reads as an option")
	}
}

func TestWriteSessionReportsFailedSessionCheck(t *testing.T) {
	t.Parallel()
	runner := &fakeRestoreRunner{hasErr: errors.New("kilo export: exit status 1: Error: database is locked")}
	session := restoredSession(t, restoreTranscript(t))
	err := (&Agent{CommandRunner: runner}).WriteSession(session)
	if err == nil || !strings.Contains(err.Error(), restoreSessionID) || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("WriteSession error = %v, want the failed check named", err)
	}
	if len(runner.imported) != 0 {
		t.Fatal("imported without knowing whether Kilo holds the session")
	}
	assertTranscriptWritten(t, session)
}

// Kilo runs in RepoPath, so a SessionRef relative to this process's directory
// must reach it as an absolute path.
func TestWriteSessionGivesKiloAnAbsoluteImportPath(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	runner := &fakeRestoreRunner{}
	session := restoredSession(t, restoreTranscript(t))
	session.SessionRef = filepath.Join(".entire", "tmp", "kilo", restoreSessionID+".json")
	if err := (&Agent{CommandRunner: runner}).WriteSession(session); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	assertTranscriptWritten(t, session)
	if len(runner.imports) != 1 {
		t.Fatalf("ImportSession calls = %d, want 1", len(runner.imports))
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runner.imports[0], filepath.Join(wd, ".entire", "tmp", "kilo"); !filepath.IsAbs(got) || filepath.Dir(got) != want {
		t.Fatalf("import path = %q, want an absolute path in %q", got, want)
	}
	if runner.importDirs[0] != session.RepoPath {
		t.Fatalf("import ran in %q, want the repo %q", runner.importDirs[0], session.RepoPath)
	}
}

func TestWriteSessionRejectsTranscriptOfAnotherSession(t *testing.T) {
	t.Parallel()
	runner := &fakeRestoreRunner{}
	session := restoredSession(t, restoreTranscript(t))
	session.SessionID = "ses_someoneelse"
	err := (&Agent{CommandRunner: runner}).WriteSession(session)
	if err == nil || !strings.Contains(err.Error(), "ses_someoneelse") {
		t.Fatalf("WriteSession error = %v, want a session mismatch", err)
	}
	if len(runner.hasCalls) != 0 || len(runner.imported) != 0 {
		t.Fatal("ran kilo for a mismatched transcript")
	}
}

// installStubKilo puts a `kilo` shell script first on PATH. It records its
// arguments, working directory and KILO_PURE to calls.log, then runs body.
func installStubKilo(t *testing.T, body string) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\nprintf '%s|%s|%s\\n' \"$*\" \"$(pwd -P)\" \"$KILO_PURE\" >> '" + logPath + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "kilo"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return logPath
}

func stubCalls(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestDefaultRunnerHasSession(t *testing.T) {
	cases := map[string]struct {
		body    string
		want    bool
		wantErr string
	}{
		"exported": {body: `printf '{"info":{"id":"%s"},"messages":[{"info":{"id":"m1"}}]}' "$2"`, want: true},
		// Kilo 7.8.27's answer for an unknown id, colors included.
		"not found":      {body: `printf 'Exporting session: %s\n\033[91m\033[1mError: \033[0mSession not found: %s\n' "$2" "$2" >&2; exit 1`, want: false},
		"other session":  {body: `printf '{"info":{"id":"ses_other"},"messages":[{"info":{"id":"m1"}}]}'`, want: false},
		"help exit zero": {body: `echo 'Commands: kilo run'`, want: false},
		// What a `kilo import` that failed on its first message leaves behind.
		"no messages": {body: `printf '{"info":{"id":"%s"},"messages":[]}' "$2"`, want: false},
		// A failure that says nothing about the session must not read as absent.
		"other failure": {body: `echo 'Error: database is locked' >&2; exit 1`, wantErr: "database is locked"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logPath := installStubKilo(t, tc.body)
			repo, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			got, err := (&DefaultCommandRunner{}).HasSession(context.Background(), repo, restoreSessionID)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("HasSession error = %v, want one naming %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("HasSession: %v", err)
			}
			if got != tc.want {
				t.Fatalf("HasSession = %v, want %v", got, tc.want)
			}
			calls := stubCalls(t, logPath)
			if want := "export " + restoreSessionID + "|" + repo + "|1"; len(calls) != 1 || calls[0] != want {
				t.Fatalf("calls = %v, want [%s]", calls, want)
			}
		})
	}
}

func TestDefaultRunnerWithoutKiloOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := (&DefaultCommandRunner{}).HasSession(context.Background(), t.TempDir(), restoreSessionID)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("HasSession error = %v, want exec.ErrNotFound", err)
	}
	err = (&DefaultCommandRunner{}).ImportSession(context.Background(), t.TempDir(), "x.json")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("ImportSession error = %v, want exec.ErrNotFound", err)
	}
}

func TestDefaultRunnerImportSession(t *testing.T) {
	logPath := installStubKilo(t, `echo "Imported session: x"`)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := (&DefaultCommandRunner{}).ImportSession(context.Background(), repo, "/tmp/export.json"); err != nil {
		t.Fatalf("ImportSession: %v", err)
	}
	if calls := stubCalls(t, logPath); len(calls) != 1 || calls[0] != "import /tmp/export.json|"+repo+"|1" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestDefaultRunnerImportSessionReportsKiloError(t *testing.T) {
	installStubKilo(t, `printf 'INFO  2026-11-18 service=default\n\033[91m\033[1mError: \033[0mUnexpected error\n\nMissing key\n  at ["slug"]\n' >&2; exit 1`)
	err := (&DefaultCommandRunner{}).ImportSession(context.Background(), t.TempDir(), "/tmp/export.json")
	if err == nil {
		t.Fatal("ImportSession succeeded on a failing kilo")
	}
	msg := err.Error()
	if !strings.Contains(msg, `Error: Unexpected error Missing key at ["slug"]`) || strings.Contains(msg, "\x1b") || strings.Contains(msg, "INFO") {
		t.Fatalf("error = %q, want Kilo's message without colors or log lines", msg)
	}
}
