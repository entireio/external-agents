package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDispatch(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"info"}, strings.NewReader(""), &out); err != nil { t.Fatal(err) }
	if !strings.Contains(out.String(), `"name":"windsurf"`) { t.Fatalf("info = %q", out.String()) }
	if err := run([]string{"get-session-id"}, strings.NewReader(`{"raw_data":{"trajectory_id":"t-1"}}`), &out); err != nil { t.Fatal(err) }
	if err := run([]string{"not-a-command"}, strings.NewReader(""), &out); err == nil { t.Fatal("unknown command accepted") }
}

func TestDispatchRejectsMalformedProtocolInput(t *testing.T) {
	if err := run([]string{"get-session-id"}, strings.NewReader("bad-json"), &bytes.Buffer{}); err == nil { t.Fatal("malformed protocol input accepted") }
}
