package aider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsAiderCommit(t *testing.T) {
	tests := []struct {
		author, committer, message string
		want                       bool
	}{
		{"Aider <aider@aider.chat>", "Human <human@example.com>", "change", true},
		{"Human <human@example.com>", "Human (aider) <human@example.com>", "change", true},
		{"Human (aider) <human@example.com>", "", "change", true},
		{"Human", "Human", "change\n\nCo-authored-by: Aider <aider@aider.chat>\n", true},
		{"", "AIDER@AIDER.CHAT", "", true},
		{"Human", "Human", "mention aider@aider.chat in prose", false},
		{"Human <not-aider@aider.chat>", "", "", false},
		{"Human", "Human", "Co-authored-by: Aider <aider@aider.chat>\n\nThis was only an example.", false},
	}
	for _, tt := range tests {
		if got := IsAiderCommit(tt.author, tt.committer, tt.message); got != tt.want {
			t.Errorf("%+v: got %v", tt, got)
		}
	}
}
func testGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func TestCommitFilesAndSessionIDIndependentOfHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	repo := testRepo(t)
	home := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "no-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for key, value := range map[string]string{"GIT_AUTHOR_NAME": "Human", "GIT_AUTHOR_EMAIL": "human@example.com", "GIT_COMMITTER_NAME": "Human", "GIT_COMMITTER_EMAIL": "human@example.com"} {
		t.Setenv(key, value)
	}
	testGit(t, repo, "init")
	testGit(t, repo, "config", "commit.gpgsign", "false")
	testGit(t, repo, "config", "core.hooksPath", filepath.Join(home, "no-hooks"))
	commit := func(file, message string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, file), []byte(message), 0o600); err != nil {
			t.Fatal(err)
		}
		testGit(t, repo, "add", "--", file)
		testGit(t, repo, "commit", "-m", message)
		return testGit(t, repo, "rev-parse", "HEAD")
	}
	first := commit("first file.txt", "initial\n\nCo-authored-by: Aider <aider@aider.chat>")
	a := New()
	id, err := a.GetSessionID(nil)
	if err != nil || id == first {
		t.Fatalf("ID = %q, HEAD = %q, error = %v", id, first, err)
	}
	files, err := a.CommitModifiedFiles(first)
	if err != nil || !reflect.DeepEqual(files, []string{"first file.txt"}) {
		t.Fatalf("root commit files = %q, %v", files, err)
	}
	human := commit("human.txt", "human change")
	files, err = a.CommitModifiedFiles(human)
	if err != nil || len(files) != 0 {
		t.Fatalf("human commit attributed: %q, %v", files, err)
	}
	t.Setenv("GIT_AUTHOR_EMAIL", "aider@aider.chat")
	last := commit("aider.txt", "agent change")
	files, err = a.CommitModifiedFiles(first + ".." + last)
	if err != nil || !reflect.DeepEqual(files, []string{"aider.txt"}) {
		t.Fatalf("range = %q, %v", files, err)
	}
	next, err := New().GetSessionID(nil)
	if err != nil || next != id {
		t.Fatalf("HEAD change rekeyed session: %q, %v", next, err)
	}
	state := filepath.Join(repo, ".entire", "tmp", "aider-session")
	if err := os.Remove(state); err != nil {
		t.Fatal(err)
	}
	next, err = New().GetSessionID(nil)
	if err != nil || next == id || next == last {
		t.Fatalf("same HEAD must allow new identity: %q, %v", next, err)
	}
	path := filepath.Join(repo, HistoryFile)
	history := fmt.Sprintf("#### Edit\n> Commit %s initial\n> Commit %s human\n> Commit %s agent\n", first, human, last)
	if err := os.WriteFile(path, []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	files, pos, err := a.ExtractModifiedFiles(path, strings.Index(history, "> Commit "+last))
	if err != nil || pos != len(history) || !reflect.DeepEqual(files, []string{"aider.txt"}) {
		t.Fatalf("transcript files = %q, %d, %v", files, pos, err)
	}
	if _, err := a.CommitModifiedFiles("--all"); err == nil {
		t.Fatal("git option accepted as revision")
	}
}
