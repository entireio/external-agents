package goose

import (
	"strings"
	"testing"
)

func TestProjectedLargeMessageRoundTrip(t *testing.T) {
	large := strings.Repeat("x", 6*1024*1024)
	export := &gooseExport{Conversation: []gooseMessage{{Role: "user", Content: []gooseContent{{Type: "text", Text: large}}}}}
	data, err := encodeSessionJSONL(export)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeSessionJSONL(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Conversation) != 1 || decoded.Conversation[0].Content[0].Text != large {
		t.Fatal("large message lost")
	}
}
