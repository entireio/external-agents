package aider

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/external-agents/agents/entire-agent-aider/internal/protocol"
)

// These exercise Aider-specific command wiring; shared protocol compliance
// remains the responsibility of the external-agents-tests runner.
func TestAiderCommandWiring(t *testing.T) {
	repo := testRepo(t)
	a := New()
	history := []byte("#### Hello café\r\nResponse\r\n> Tokens: 2.3k sent, 450 received. Cost: $0.02\r\n")
	ref := filepath.Join(repo, HistoryFile)
	if err := os.WriteFile(ref, history, 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(input []byte, args ...string) []byte {
		t.Helper()
		var output bytes.Buffer
		if err := protocol.Run(args, bytes.NewReader(input), &output, a, a.Info()); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return output.Bytes()
	}
	idResponse := run([]byte(`{}`), "get-session-id")
	if !bytes.Equal(idResponse, run([]byte(`{}`), "get-session-id")) {
		t.Fatal("command rekeyed session")
	}
	var session protocol.Session
	input, err := json.Marshal(protocol.HookInput{SessionRef: ref})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(run(input, "read-session"), &session); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(session.NativeData, history) {
		t.Fatal("read-session lost history bytes")
	}
	session.SessionRef = filepath.Join(repo, "restored", HistoryFile)
	input, err = json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if output := run(input, "write-session"); len(output) != 0 {
		t.Fatalf("write-session output = %q", output)
	}
	if got := run(nil, "read-transcript", "--session-ref", session.SessionRef); !bytes.Equal(got, history) {
		t.Fatalf("restore = %q", got)
	}
	chunked := run(history, "chunk-transcript", "--max-size", "7")
	if got := run(chunked, "reassemble-transcript"); !bytes.Equal(got, history) {
		t.Fatal("wire chunk round trip failed")
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"extract-prompts", "--session-ref", ref}, `{"prompts":["Hello café"]}`},
		{[]string{"extract-modified-files", "--path", ref}, `"files":[]`},
		{[]string{"extract-summary", "--session-ref", ref}, `"has_summary":false`},
		{[]string{"format-resume-command", "--session-id", "anything"}, `{"command":"aider --restore-chat-history"}`},
		{[]string{"install-hooks", "--force", "--local-dev"}, `{"hooks_installed":0}`},
		{[]string{"are-hooks-installed"}, `{"installed":false}`},
		{[]string{"parse-hook", "--hook", "stop"}, `null`},
	} {
		if got := string(run(nil, tc.args...)); !strings.Contains(got, tc.want) {
			t.Errorf("%v = %s, want %s", tc.args, got, tc.want)
		}
	}
	var tokens protocol.Tokens
	if err := json.Unmarshal(run(history, "calculate-tokens", "--offset", "0"), &tokens); err != nil {
		t.Fatal(err)
	}
	if tokens.InputTokens != 2300 || tokens.OutputTokens != 450 {
		t.Fatalf("tokens = %+v", tokens)
	}
}

func TestAiderCommandErrors(t *testing.T) {
	testRepo(t)
	a := New()
	for _, tc := range []struct {
		args  []string
		input string
	}{
		{nil, ""},
		{[]string{"unknown"}, ""},
		{[]string{"info", "--unexpected"}, ""},
		{[]string{"detect", "unexpected"}, ""},
		{[]string{"get-session-id"}, "not-json"},
		{[]string{"chunk-transcript", "--max-size", "0"}, "hello"},
		{[]string{"reassemble-transcript"}, `{"chunks":["invalid!"]}`},
		{[]string{"calculate-tokens", "--offset", "-1"}, ""},
		{[]string{"extract-modified-files", "--commit", "HEAD", "--range", "HEAD~1..HEAD"}, ""},
	} {
		var output bytes.Buffer
		if err := protocol.Run(tc.args, strings.NewReader(tc.input), &output, a, a.Info()); err == nil {
			t.Errorf("%v should fail", tc.args)
		}
		if output.Len() != 0 {
			t.Errorf("%v wrote error to stdout: %q", tc.args, output.String())
		}
	}
}
