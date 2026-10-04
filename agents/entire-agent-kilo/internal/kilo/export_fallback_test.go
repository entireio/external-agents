package kilo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKilo installs a `kilo` stub earlier on PATH and returns the log of the
// invocations it received. Real Kilo is an opaque binary, so the only honest
// way to test which subcommand the runner picks — and what it does with the
// output — is to stand in a script that behaves the way the installed version
// behaves.
func fakeKilo(t *testing.T, script string) func() []string {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "calls.log")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(record) + "\n" +
		script
	if err := os.WriteFile(filepath.Join(dir, "kilo"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake kilo: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Read on demand: the stub writes the log as it runs, so a snapshot taken
	// at install time would always be empty.
	return func() []string {
		data, err := os.ReadFile(record)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

const validExport = `{"info":{"id":"ses_abc"},"messages":[{"info":{"id":"m1","sessionID":"ses_abc","role":"user"},"parts":[{"type":"text","text":"hi"}]}]}`

func TestExportSessionPrefersKiloExport(t *testing.T) {
	// Kilo 7.8.x spells the native export `kilo export [id]`.
	calls := fakeKilo(t, `case "$1" in
  export) printf '%s\n' '`+validExport+`' ;;
  *) printf 'unknown command\n'; exit 1 ;;
esac`)
	got, err := (&DefaultCommandRunner{}).ExportSession(context.Background(), "ses_abc")
	if err != nil {
		t.Fatalf("ExportSession: %v", err)
	}
	if !strings.Contains(string(got), "ses_abc") {
		t.Fatalf("unexpected payload: %s", got)
	}
	if len(calls()) != 1 || !strings.HasPrefix(calls()[0], "export ses_abc") {
		t.Fatalf("expected a single `kilo export` call, got %v", calls())
	}
}

func TestExportSessionFallsBackToSessionShow(t *testing.T) {
	// Newer builds expose `kilo session show --format json`.
	calls := fakeKilo(t, `case "$1" in
  export) printf 'unknown command\n' >&2; exit 1 ;;
  session) printf '%s\n' '`+validExport+`' ;;
esac`)
	if _, err := (&DefaultCommandRunner{}).ExportSession(context.Background(), "ses_abc"); err != nil {
		t.Fatalf("ExportSession: %v", err)
	}
	if len(calls()) != 2 {
		t.Fatalf("expected the fallback to be tried, got %v", calls())
	}
	if calls()[1] != "session show --format json ses_abc" {
		t.Fatalf("unexpected fallback call: %q", calls()[1])
	}
}

// An unknown subcommand prints help and still exits 0, so a runner that trusts
// the exit code hands Entire a help page as if it were a session. Only output
// that parses as an export with messages counts.
func TestExportSessionRejectsOutputThatIsNotASession(t *testing.T) {
	calls := fakeKilo(t, `case "$1" in
  export) printf 'Exporting session: ses_abc\n' ;;
  session) printf 'kilo session\n\nmanage sessions\n'; exit 0 ;;
esac`)
	_, err := (&DefaultCommandRunner{}).ExportSession(context.Background(), "ses_abc")
	if err == nil {
		t.Fatal("expected an error when no candidate produced a session export")
	}
	if !strings.Contains(err.Error(), "not a session export") {
		t.Fatalf("error should say the output was rejected, got: %v", err)
	}
	if len(calls()) != 2 {
		t.Fatalf("expected both candidates to be tried, got %v", calls())
	}
}

func TestExportSessionRejectsAnEmptySession(t *testing.T) {
	fakeKilo(t, `printf '{"info":{"id":"ses_abc"},"messages":[]}\n'`)
	if _, err := (&DefaultCommandRunner{}).ExportSession(context.Background(), "ses_abc"); err == nil {
		t.Fatal("a session with no messages is not a transcript")
	}
}

func TestExportSessionRequiresASessionID(t *testing.T) {
	if _, err := (&DefaultCommandRunner{}).ExportSession(context.Background(), "  "); err == nil {
		t.Fatal("expected an error for a blank session id")
	}
}

func TestSessionIDFromRefPath(t *testing.T) {
	cases := map[string]string{
		"/repo/.entire/tmp/kilo/ses_abc.json":          "ses_abc",
		"/repo/.entire/tmp/kilo/ses_abc":               "ses_abc",
		"/repo/.entire/tmp/kilo/session.json":          "",
		"/repo/.entire/tmp/kilo/ses_.json":             "",
		"/repo/.entire/tmp/kilo/not-a-session-id.json": "",
		"": "",
	}
	for ref, want := range cases {
		if got := sessionIDFromRefPath(ref); got != want {
			t.Errorf("sessionIDFromRefPath(%q) = %q, want %q", ref, got, want)
		}
	}
}
