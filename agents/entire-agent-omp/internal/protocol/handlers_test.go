package protocol

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type testResolver struct {
	dir, file string
	err       error
}

func (r testResolver) GetSessionDir(string) (string, error)     { return r.dir, r.err }
func (r testResolver) ResolveSessionFile(string, string) string { return r.file }

type testProvider struct{}

func (testProvider) GetSessionID(input *HookInputJSON) string { return input.SessionID }

type testReader struct {
	session AgentSessionJSON
	err     error
}

func (r testReader) ReadSession(*HookInputJSON) (AgentSessionJSON, error) { return r.session, r.err }

type testWriter struct{ got AgentSessionJSON }

func (w *testWriter) WriteSession(session AgentSessionJSON) error { w.got = session; return nil }

type testResume struct{}

func (testResume) FormatResumeCommand(id string) string { return "resume " + id }

type testTranscript struct{}

func (testTranscript) ReadTranscript(string) ([]byte, error) { return []byte("transcript"), nil }
func (testTranscript) ChunkTranscript(data []byte, _ int) ([][]byte, error) {
	return [][]byte{data}, nil
}
func (testTranscript) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, nil), nil
}
func (testTranscript) CompactTranscript(string) (CompactTranscriptResponse, error) {
	return CompactTranscriptResponse{Transcript: "encoded"}, nil
}

type testHooks struct{ event *EventJSON }

func (h testHooks) ParseHook(string, []byte) (*EventJSON, error) { return h.event, nil }
func (testHooks) InstallHooks(bool, bool) (int, error)           { return 4, nil }
func (testHooks) UninstallHooks() error                          { return nil }
func (testHooks) AreHooksInstalled() bool                        { return true }

type testAnalyzer struct{}

func (testAnalyzer) GetTranscriptPosition(string) (int, error) { return 7, nil }
func (testAnalyzer) ExtractModifiedFiles(string, int) ([]string, int, error) {
	return []string{"a.go"}, 9, nil
}
func (testAnalyzer) ExtractPrompts(string, int) ([]string, error) { return []string{"hello"}, nil }
func (testAnalyzer) ExtractSummary(string) (string, bool, error)  { return "summary", true, nil }

func TestCoreProtocolHandlers(t *testing.T) {
	var out bytes.Buffer
	if err := HandleGetSessionID(strings.NewReader(`{"session_id":"id-1"}`), &out, testProvider{}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != `{"session_id":"id-1"}`+"\n" {
		t.Fatalf("get-session-id output = %q", got)
	}

	out.Reset()
	if err := HandleGetSessionDir([]string{"--repo-path", "/repo"}, &out, testResolver{dir: "/sessions/-repo"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != `{"session_dir":"/sessions/-repo"}`+"\n" {
		t.Fatalf("get-session-dir output = %q", got)
	}

	out.Reset()
	if err := HandleResolveSessionFile([]string{"--session-dir", "/sessions/-repo", "--session-id", "id-1"}, &out, testResolver{file: "/sessions/-repo/file.jsonl"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != `{"session_file":"/sessions/-repo/file.jsonl"}`+"\n" {
		t.Fatalf("resolve-session-file output = %q", got)
	}

	out.Reset()
	if err := HandleReadSession(strings.NewReader(`{}`), &out, testReader{session: AgentSessionJSON{SessionID: "id-1", AgentName: "omp"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"session_id":"id-1"`) {
		t.Fatalf("read-session output = %q", out.String())
	}

	writer := &testWriter{}
	if err := HandleWriteSession(strings.NewReader(`{"session_id":"id-2","native_data":"YWJj"}`), writer); err != nil {
		t.Fatal(err)
	}
	if writer.got.SessionID != "id-2" || string(writer.got.NativeData) != "abc" {
		t.Fatalf("write-session input = %#v", writer.got)
	}

	out.Reset()
	if err := HandleFormatResumeCommand([]string{"--session-id", "id-1"}, &out, testResume{}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != `{"command":"resume id-1"}`+"\n" {
		t.Fatalf("format-resume output = %q", got)
	}
}

func TestTranscriptAndAnalyzerHandlers(t *testing.T) {
	impl := testTranscript{}
	var out bytes.Buffer
	if err := HandleReadTranscript([]string{"--session-ref", "file"}, &out, impl); err != nil || out.String() != "transcript" {
		t.Fatalf("read transcript: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := HandleChunkTranscript([]string{"--max-size", "10"}, strings.NewReader("abc"), &out, impl); err != nil || out.String() != `{"chunks":["YWJj"]}`+"\n" {
		t.Fatalf("chunk: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := HandleReassembleTranscript(strings.NewReader(`{"chunks":["YQ==","Yg=="]}`), &out, impl); err != nil || out.String() != "ab" {
		t.Fatalf("reassemble: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := HandleCompactTranscript([]string{"--session-ref", "file"}, &out, impl); err != nil || out.String() != `{"transcript":"encoded"}`+"\n" {
		t.Fatalf("compact: %q, %v", out.String(), err)
	}

	analyzer := testAnalyzer{}
	out.Reset()
	if err := HandleGetTranscriptPosition([]string{"--path", "file"}, &out, analyzer); err != nil || out.String() != `{"position":7}`+"\n" {
		t.Fatalf("position: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := HandleExtractModifiedFiles([]string{"--path", "file", "--offset", "2"}, &out, analyzer); err != nil || out.String() != `{"files":["a.go"],"current_position":9}`+"\n" {
		t.Fatalf("files: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := HandleExtractPrompts([]string{"--session-ref", "file", "--offset", "2"}, &out, analyzer); err != nil || out.String() != `{"prompts":["hello"]}`+"\n" {
		t.Fatalf("prompts: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := HandleExtractSummary([]string{"--session-ref", "file"}, &out, analyzer); err != nil || out.String() != `{"summary":"summary","has_summary":true}`+"\n" {
		t.Fatalf("summary: %q, %v", out.String(), err)
	}
}

func TestHookHandlersAndErrors(t *testing.T) {
	var out bytes.Buffer
	if err := HandleParseHook([]string{"--hook", "agent_end"}, strings.NewReader(`{}`), &out, testHooks{}); err != nil || out.String() != "null\n" {
		t.Fatalf("nil hook: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := HandleParseHook([]string{"--hook", "session_start"}, strings.NewReader(`{}`), &out, testHooks{event: &EventJSON{Type: 1, SessionID: "id"}}); err != nil || !strings.Contains(out.String(), `"type":1`) {
		t.Fatalf("hook: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := HandleInstallHooks([]string{"--force"}, &out, testHooks{}); err != nil || out.String() != `{"hooks_installed":4}`+"\n" {
		t.Fatalf("install: %q, %v", out.String(), err)
	}

	wantErr := errors.New("resolver failed")
	if err := HandleGetSessionDir(nil, &out, testResolver{err: wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if err := HandleGetSessionDir([]string{"--unknown"}, &out, testResolver{}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestWriteJSONDoesNotEscapeHTML(t *testing.T) {
	var out bytes.Buffer
	if err := WriteJSON(&out, map[string]string{"value": "<omp>&"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != `{"value":"<omp>&"}`+"\n" {
		t.Fatalf("WriteJSON() = %q", got)
	}
}
