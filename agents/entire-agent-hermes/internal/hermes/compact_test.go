package hermes

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Entire CLI compacts at checkpoint time from a temp file holding the
// already-redacted transcript, outside HERMES_HOME.
func TestCompactTranscriptAcceptsCallerProvidedFile(t *testing.T) {
	t.Setenv("HERMES_HOME", t.TempDir())
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "entire-compact-1234.jsonl")
	writeFixture(t, path, fixture, 0o600)

	out, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatalf("CompactTranscript caller temp file: %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(out.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"Create a small greeting command.", `"tool_use"`, `"write_file"`, "Implemented the greeting command"} {
		if !strings.Contains(got, want) {
			t.Fatalf("compact output missing %q:\n%s", want, got)
		}
	}
}

func TestCompactTranscriptRejectsSymlinkAndDirectory(t *testing.T) {
	t.Setenv("HERMES_HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "private.jsonl")
	writeFixture(t, target, []byte(`{"v":1,"type":"user","content":"outside-secret"}`+"\n"), 0o600)
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := New().CompactTranscript(link); err == nil {
		t.Fatal("CompactTranscript followed a symlink")
	}
	if _, err := New().CompactTranscript(dir); err == nil {
		t.Fatal("CompactTranscript accepted a directory")
	}
	if _, err := New().CompactTranscript("relative.jsonl"); err == nil {
		t.Fatal("CompactTranscript accepted a relative path")
	}
}

// The CLI passes its version to plugins; compact lines should carry it.
func TestCompactTranscriptStampsCLIVersion(t *testing.T) {
	t.Setenv("HERMES_HOME", t.TempDir())
	t.Setenv("ENTIRE_CLI_VERSION", "9.8.7")
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "entire-compact-5678.jsonl")
	writeFixture(t, path, fixture, 0o600)

	out, err := New().CompactTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(out.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"cli_version":"unknown"`) || !strings.Contains(string(data), `"cli_version":"9.8.7"`) {
		t.Fatalf("compact lines should carry ENTIRE_CLI_VERSION:\n%s", data)
	}
}
