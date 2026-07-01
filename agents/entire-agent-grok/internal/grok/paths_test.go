package grok

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeRepoCWD(t *testing.T) {
	repo := "/Users/test/project"
	encoded := encodeRepoCWD(repo)
	if encoded != "%2FUsers%2Ftest%2Fproject" {
		t.Fatalf("unexpected encoded cwd: %q", encoded)
	}
}

func TestNativeTranscriptPath(t *testing.T) {
	t.Setenv("GROK_HOME", t.TempDir())
	repo := "/Users/test/project"
	path := nativeTranscriptPath(repo, "session-123")
	wantSuffix := filepath.Join("sessions", "%2FUsers%2Ftest%2Fproject", "session-123", "chat_history.jsonl")
	if !strings.HasSuffix(path, wantSuffix) {
		t.Fatalf("unexpected transcript path %q", path)
	}
}