package protocol

import (
	"bytes"
	"strings"
	"testing"
)

type testProvider struct{}
func (testProvider) GetSessionID(input *HookInputJSON) string { return input.SessionID }

func TestGetSessionIDRejectsMalformedInput(t *testing.T) {
	var out bytes.Buffer
	if err := HandleGetSessionID(strings.NewReader("not json"), &out, testProvider{}); err == nil { t.Fatal("malformed input accepted") }
}

func TestReadTranscriptRequiresReference(t *testing.T) {
	var out bytes.Buffer
	if err := HandleReadTranscript(nil, &out, testTranscript{}); err == nil { t.Fatal("empty session-ref accepted") }
}

type testTranscript struct{}
func (testTranscript) ReadTranscript(string) ([]byte, error) { return nil, nil }
