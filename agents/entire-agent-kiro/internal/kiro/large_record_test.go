package kiro

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectedLargeMessageRoundTrip(t *testing.T) {
	large := strings.Repeat("x", 6*1024*1024)
	raw, err := json.Marshal(map[string]any{"conversation_id": "large", "history": []any{map[string]any{"user": map[string]any{"content": map[string]any{"Prompt": map[string]any{"prompt": large}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	data, ok := materializeTranscript(raw)
	if !ok {
		t.Fatal("materialization failed")
	}
	decoded, err := decodeTranscriptJSONL(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.History) != 1 || extractUserPrompt(decoded.History[0].User.Content) != large {
		t.Fatal("large message lost")
	}
}
