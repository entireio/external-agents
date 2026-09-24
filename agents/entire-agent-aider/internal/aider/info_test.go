package aider

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	return repo
}
func TestInfoAndDetect(t *testing.T) {
	repo := testRepo(t)
	agent := New()
	info := agent.Info()
	if info.Name != "aider" || info.Type != "Aider" || info.ProtocolVersion != 1 || !info.IsPreview || info.Capabilities.Hooks || !info.Capabilities.TranscriptAnalyzer || !info.Capabilities.TokenCalculator {
		t.Fatalf("info = %+v", info)
	}
	agent.LookPath = func(name string) (string, error) {
		if name != "aider" {
			t.Fatalf("lookup = %q", name)
		}
		return "", errors.New("not installed")
	}
	if agent.Detect() {
		t.Fatal("detected absent Aider")
	}
	path := filepath.Join(repo, HistoryFile)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if agent.Detect() {
		t.Fatal("directory is not history")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("history"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !agent.Detect() {
		t.Fatal("history not detected")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	agent.LookPath = func(string) (string, error) { return "aider", nil }
	if !agent.Detect() {
		t.Fatal("binary not detected")
	}
}
func TestFormatResumeCommand(t *testing.T) {
	if got := New().FormatResumeCommand("ignored & unsafe"); got != "aider --restore-chat-history" {
		t.Fatal(got)
	}
}
