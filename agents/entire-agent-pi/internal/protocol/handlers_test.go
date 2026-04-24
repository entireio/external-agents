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

func (c testTranscriptCompactor) CompactTranscript(_ string) (CompactTranscriptResponse, error) {
	return c.response, c.err
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
