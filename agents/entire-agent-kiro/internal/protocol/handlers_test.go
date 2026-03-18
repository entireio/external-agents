package protocol

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type testSessionDirResolver struct {
	dir string
	err error
}

func (r testSessionDirResolver) GetSessionDir(_ string) (string, error) {
	return r.dir, r.err
}

type testSessionFileResolver struct{}

func (testSessionFileResolver) ResolveSessionFile(sessionDir, sessionID string) string {
	return ResolveSessionFile(sessionDir, sessionID)
}

type testSessionReader struct {
	session AgentSessionJSON
	err     error
}

func (r testSessionReader) ReadSession(_ *HookInputJSON) (AgentSessionJSON, error) {
	return r.session, r.err
}

type testSessionWriter struct {
	called  bool
	session AgentSessionJSON
	err     error
}

func (w *testSessionWriter) WriteSession(session AgentSessionJSON) error {
	w.called = true
	w.session = session
	return w.err
}

type testTranscriptReader struct {
	data []byte
	err  error
}

func (r testTranscriptReader) ReadTranscript(_ string) ([]byte, error) {
	return r.data, r.err
}

type testTranscriptChunker struct {
	chunks [][]byte
	data   []byte
	err    error
}

func (c testTranscriptChunker) ChunkTranscript(_ []byte, _ int) ([][]byte, error) {
	return c.chunks, c.err
}

func (c testTranscriptChunker) ReassembleTranscript(_ [][]byte) ([]byte, error) {
	return c.data, c.err
}

type testResumeFormatter struct{}

func (testResumeFormatter) FormatResumeCommand(sessionID string) string {
	return "resume " + sessionID
}

func TestHandleGetSessionDirPropagatesResolverError(t *testing.T) {
	var stdout bytes.Buffer

	err := HandleGetSessionDir([]string{"--repo-path", "/tmp/repo"}, &stdout, testSessionDirResolver{
		err: errors.New("boom"),
	})

	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("HandleGetSessionDir() error = %v, want boom", err)
	}
}

func TestHandleReadSessionPropagatesReaderError(t *testing.T) {
	var stdout bytes.Buffer

	err := HandleReadSession(strings.NewReader(`{"session_id":"s1"}`), &stdout, testSessionReader{
		err: errors.New("missing transcript"),
	})

	if err == nil || !strings.Contains(err.Error(), "missing transcript") {
		t.Fatalf("HandleReadSession() error = %v, want missing transcript", err)
	}
}

func TestHandlerRoundTripForCoreProtocolCommands(t *testing.T) {
	t.Run("get-session-dir", func(t *testing.T) {
		var stdout bytes.Buffer
		err := HandleGetSessionDir([]string{"--repo-path", "/tmp/repo"}, &stdout, testSessionDirResolver{
			dir: "/tmp/repo/.entire/tmp",
		})
		if err != nil {
			t.Fatalf("HandleGetSessionDir() error = %v", err)
		}
		if got := stdout.String(); !strings.Contains(got, `"session_dir":"/tmp/repo/.entire/tmp"`) {
			t.Fatalf("stdout = %s", got)
		}
	})

	t.Run("resolve-session-file", func(t *testing.T) {
		var stdout bytes.Buffer
		err := HandleResolveSessionFile([]string{"--session-dir", "/tmp/repo/.entire/tmp", "--session-id", "abc123"}, &stdout, testSessionFileResolver{})
		if err != nil {
			t.Fatalf("HandleResolveSessionFile() error = %v", err)
		}
		if got := stdout.String(); !strings.Contains(got, `"session_file":"/tmp/repo/.entire/tmp/abc123.json"`) {
			t.Fatalf("stdout = %s", got)
		}
	})

	t.Run("read-session", func(t *testing.T) {
		var stdout bytes.Buffer
		err := HandleReadSession(strings.NewReader(`{"session_id":"abc123","session_ref":"/tmp/repo/.entire/tmp/abc123.json"}`), &stdout, testSessionReader{
			session: AgentSessionJSON{
				SessionID:  "abc123",
				AgentName:  "kiro",
				RepoPath:   "/tmp/repo",
				SessionRef: "/tmp/repo/.entire/tmp/abc123.json",
			},
		})
		if err != nil {
			t.Fatalf("HandleReadSession() error = %v", err)
		}
		if got := stdout.String(); !strings.Contains(got, `"session_id":"abc123"`) || !strings.Contains(got, `"agent_name":"kiro"`) {
			t.Fatalf("stdout = %s", got)
		}
	})

	t.Run("write-session", func(t *testing.T) {
		writer := &testSessionWriter{}
		err := HandleWriteSession(strings.NewReader(`{"session_id":"abc123","agent_name":"kiro","repo_path":"/tmp/repo","session_ref":"/tmp/repo/.entire/tmp/abc123.json","start_time":"2026-03-17T00:00:00Z","native_data":"e30=","modified_files":[],"new_files":[],"deleted_files":[]}`), writer)
		if err != nil {
			t.Fatalf("HandleWriteSession() error = %v", err)
		}
		if !writer.called || writer.session.SessionID != "abc123" {
			t.Fatalf("writer = %#v", writer)
		}
	})

	t.Run("read-transcript", func(t *testing.T) {
		var stdout bytes.Buffer
		err := HandleReadTranscript([]string{"--session-ref", "/tmp/repo/.entire/tmp/abc123.json"}, &stdout, testTranscriptReader{
			data: []byte(`{"conversation_id":"abc123"}`),
		})
		if err != nil {
			t.Fatalf("HandleReadTranscript() error = %v", err)
		}
		if got := stdout.String(); got != `{"conversation_id":"abc123"}` {
			t.Fatalf("stdout = %q", got)
		}
	})

	t.Run("chunk-transcript", func(t *testing.T) {
		var stdout bytes.Buffer
		err := HandleChunkTranscript([]string{"--max-size", "32"}, strings.NewReader("hello"), &stdout, testTranscriptChunker{
			chunks: [][]byte{[]byte("hello")},
		})
		if err != nil {
			t.Fatalf("HandleChunkTranscript() error = %v", err)
		}
		if got := stdout.String(); !strings.Contains(got, `"chunks":["aGVsbG8="]`) {
			t.Fatalf("stdout = %s", got)
		}
	})

	t.Run("reassemble-transcript", func(t *testing.T) {
		var stdout bytes.Buffer
		err := HandleReassembleTranscript(strings.NewReader(`{"chunks":["aGVsbG8="]}`), &stdout, testTranscriptChunker{
			data: []byte("hello"),
		})
		if err != nil {
			t.Fatalf("HandleReassembleTranscript() error = %v", err)
		}
		if got := stdout.String(); got != "hello" {
			t.Fatalf("stdout = %q", got)
		}
	})

	t.Run("format-resume-command", func(t *testing.T) {
		var stdout bytes.Buffer
		err := HandleFormatResumeCommand([]string{"--session-id", "abc123"}, &stdout, testResumeFormatter{})
		if err != nil {
			t.Fatalf("HandleFormatResumeCommand() error = %v", err)
		}
		if got := stdout.String(); !strings.Contains(got, `"command":"resume abc123"`) {
			t.Fatalf("stdout = %s", got)
		}
	})
}
