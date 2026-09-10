package kilo

import (
	"strings"
	"testing"
)

func TestProjectedLargeMessageRoundTrip(t *testing.T) {
	large := strings.Repeat("x", 6*1024*1024)
	data, err := encodeMessagesJSONL([]SessionMessage{makeTextMessage("large", MessageRoleUser, large)})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTranscript(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded[0].Parts[0].Text != large {
		t.Fatal("large message lost")
	}
}
