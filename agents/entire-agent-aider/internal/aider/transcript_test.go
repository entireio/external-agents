package aider

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMarkdownPromptsAndTokens(t *testing.T) {
	data, err := os.ReadFile("testdata/history.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, crlf := range []bool{false, true} {
		fixture := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		if crlf {
			fixture = bytes.ReplaceAll(fixture, []byte("\n"), []byte("\r\n"))
		}
		path := filepath.Join(t.TempDir(), HistoryFile)
		if err := os.WriteFile(path, fixture, 0o600); err != nil {
			t.Fatal(err)
		}
		a := New()
		prompts, err := a.ExtractPrompts(path, 0)
		want := []string{"Add a greeting function.", "Make the greeting Unicode-safe: café."}
		if err != nil || !reflect.DeepEqual(prompts, want) {
			t.Fatalf("prompts = %q, %v", prompts, err)
		}
		usage, err := a.CalculateTokens(fixture, 0)
		if err != nil || usage.InputTokens != 3500 || usage.OutputTokens != 550 || usage.APICallCount != 2 || math.Abs(usage.CostUSD-0.03) > 1e-9 {
			t.Fatalf("usage = %+v, %v", usage, err)
		}
		offset := bytes.Index(fixture, []byte("#### Make"))
		prompts, err = a.ExtractPrompts(path, offset)
		if err != nil || !reflect.DeepEqual(prompts, want[1:]) {
			t.Fatalf("scoped prompts = %q, %v", prompts, err)
		}
		usage, err = a.CalculateTokens(fixture, offset)
		if err != nil || usage.InputTokens != 1200 || usage.APICallCount != 1 {
			t.Fatalf("scoped tokens = %+v, %v", usage, err)
		}
		position, err := a.GetTranscriptPosition(path)
		if err != nil || position != len(fixture) {
			t.Fatalf("position = %d, %v", position, err)
		}
		prompts, err = a.ExtractPrompts(path, len(fixture)+1)
		if err != nil || len(prompts) != 0 {
			t.Fatalf("past EOF = %q, %v", prompts, err)
		}
	}
}
func TestTokenCountsAndInvalidOffsets(t *testing.T) {
	a := New()
	usage, err := a.CalculateTokens([]byte("> Tokens: 1.5m sent, 2K received."), 0)
	if err != nil || usage.InputTokens != 1500000 || usage.OutputTokens != 2000 {
		t.Fatalf("usage = %+v, %v", usage, err)
	}
	if _, err := a.CalculateTokens(nil, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, err := a.CalculateTokens([]byte("> Tokens: 9999999999999999999999999m sent, 2 received."), 0); err == nil {
		t.Fatal("overflow accepted")
	}
}
func TestChunkTranscriptRoundTrip(t *testing.T) {
	a := New()
	for _, data := range [][]byte{nil, []byte("café\x00\r\nhello"), bytes.Repeat([]byte("x"), 1000)} {
		for _, size := range []int{1, 3, 100} {
			chunks, err := a.ChunkTranscript(data, size)
			if err != nil {
				t.Fatal(err)
			}
			for _, chunk := range chunks {
				if len(chunk) > size {
					t.Fatal("oversized chunk")
				}
			}
			got, err := a.ReassembleTranscript(chunks)
			if err != nil || !bytes.Equal(data, got) {
				t.Fatalf("round trip: %q, %v", got, err)
			}
		}
	}
	if _, err := a.ChunkTranscript(nil, 0); err == nil {
		t.Fatal("zero max-size accepted")
	}
}
