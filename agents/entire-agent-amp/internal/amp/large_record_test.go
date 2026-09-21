package amp

import (
	"strings"
	"testing"
)

func TestProjectedLargeMessageRoundTrip(t *testing.T) {
	large := strings.Repeat("x", 6*1024*1024)
	messages := []ThreadMessage{{Role: ThreadMessageRoleUser, Content: []ThreadContentBlock{{Type: ThreadContentText, Text: large}}}}
	data, err := encodeMessagesJSONL(messages)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTranscript(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded[0].Content[0].Text != large {
		t.Fatal("large message lost")
	}
}
