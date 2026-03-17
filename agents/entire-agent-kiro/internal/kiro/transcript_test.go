package kiro

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.json")
	want := []byte(`{"conversation_id":"session-789","history":[]}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	got, err := New().ReadTranscript(path)
	if err != nil {
		t.Fatalf("ReadTranscript() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadTranscript() = %q, want %q", string(got), string(want))
	}
}

func TestChunkTranscriptRoundTrip(t *testing.T) {
	original := []byte("abcdefghijklmnopqrstuvwxyz")

	chunks, err := New().ChunkTranscript(original, 8)
	if err != nil {
		t.Fatalf("ChunkTranscript() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if len(chunk) > 8 {
			t.Fatalf("chunk %d length = %d, want <= 8", i, len(chunk))
		}
	}

	reassembled, err := New().ReassembleTranscript(chunks)
	if err != nil {
		t.Fatalf("ReassembleTranscript() error = %v", err)
	}
	if !bytes.Equal(reassembled, original) {
		t.Fatalf("reassembled = %q, want %q", string(reassembled), string(original))
	}
}
