package hermes

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type idLine struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Message   *struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func assistantIDs(t *testing.T, data []byte) []string {
	t.Helper()
	var ids []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		var l idLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		if l.Type != entryTypeAssistant || l.Message == nil {
			continue
		}
		if l.Message.ID == "" || l.RequestID != l.Message.ID {
			t.Fatalf("assistant line needs message.id and a matching requestId: %s", sc.Bytes())
		}
		var blocks []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(l.Message.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "tool_use" && b.ID == "" {
					t.Fatalf("tool_use block needs an id: %s", sc.Bytes())
				}
			}
		}
		ids = append(ids, l.Message.ID)
	}
	return ids
}

// Claude-format readers (entire-api) drop assistant lines without a
// message.id/requestId and tool_use blocks without an id.
func TestPortableAssistantLinesCarryStableIDs(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_, first, err := parseEntries(fixture, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := assistantIDs(t, first)
	if len(ids) < 2 {
		t.Fatalf("want ids on the tool and reply lines, got %v", ids)
	}
	_, again, err := parseEntries(fixture, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := assistantIDs(t, again); len(got) != len(ids) || got[0] != ids[0] {
		t.Fatalf("ids not stable across reads: %v vs %v", ids, got)
	}
	_, tail, err := parseEntries(fixture, 1)
	if err != nil {
		t.Fatal(err)
	}
	tailIDs := assistantIDs(t, tail)
	if len(tailIDs) == 0 || tailIDs[len(tailIDs)-1] != ids[len(ids)-1] {
		t.Fatalf("a line must keep its id when read from an offset: full %v, from offset 1 %v", ids, tailIDs)
	}
}
