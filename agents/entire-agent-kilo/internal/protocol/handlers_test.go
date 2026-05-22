package protocol

import (
	"bytes"
	"strings"
	"testing"
)

type testTranscriptCompactor struct {
	response CompactTranscriptResponse
	err      error
}

type testTranscriptPreparer struct {
	sessionRef string
	err        error
}

func (c testTranscriptCompactor) CompactTranscript(_ string) (CompactTranscriptResponse, error) {
	return c.response, c.err
}

func (p *testTranscriptPreparer) PrepareTranscript(sessionRef string) error {
	p.sessionRef = sessionRef
	return p.err
}

func TestHandleCompactTranscript(t *testing.T) {
	var stdout bytes.Buffer
	err := HandleCompactTranscript([]string{"--session-ref", "/tmp/repo/.entire/tmp/abc123.json"}, &stdout, testTranscriptCompactor{
		response: CompactTranscriptResponse{Transcript: "eyJ2IjoxfQo="},
	})
	if err != nil {
		t.Fatalf("HandleCompactTranscript() error = %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, `"transcript":"eyJ2IjoxfQo="`) {
		t.Fatalf("stdout = %s", got)
	}
}

func TestHandlePrepareTranscript(t *testing.T) {
	preparer := &testTranscriptPreparer{}
	err := HandlePrepareTranscript([]string{"--session-ref", "/tmp/repo/.entire/tmp/amp/T-123.json"}, preparer)
	if err != nil {
		t.Fatalf("HandlePrepareTranscript() error = %v", err)
	}
	if preparer.sessionRef != "/tmp/repo/.entire/tmp/amp/T-123.json" {
		t.Fatalf("sessionRef = %q", preparer.sessionRef)
	}
}
