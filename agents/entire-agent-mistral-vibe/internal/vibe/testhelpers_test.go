package vibe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const testSessionID = "3aba6924-25dd-7098-7afd-c9ea6384aa75"

// fakeFetcher returns a canned snapshot and counts calls.
type fakeFetcher struct {
	snapshot *Snapshot
	err      error
	calls    int
}

func (f *fakeFetcher) FetchSession(context.Context, string, string) (*Snapshot, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.snapshot, nil
}

var errFetch = errors.New("app server unavailable")

// isolate points the repository root and the user cache at temp directories,
// so a test never touches the real cache. It returns the repository root.
func isolate(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("ENTIRE_REPO_ROOT", repo)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	return repo
}

type sessionFixture struct {
	Session struct {
		Model      string      `json:"model"`
		TokenUsage *TokenUsage `json:"tokenUsage"`
	} `json:"session"`
	Items []json.RawMessage `json:"items"`
}

// loadSnapshot returns the session captured from a real Vibe 3.1.2 run: one
// prompt, one file write, one reply, plus the hook notices around them.
func loadSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture sessionFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return &Snapshot{Entries: fixture.Items, Usage: fixture.Session.TokenUsage, Model: fixture.Session.Model}
}

// hookPayloadFixture returns a hook payload captured from a real run.
func hookPayloadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	payload, ok := all[name]
	if !ok {
		t.Fatalf("no hook fixture %q", name)
	}
	return payload
}

// storedTranscript refreshes a transcript from the fixture and returns its path.
func storedTranscript(t *testing.T) string {
	t.Helper()
	agent := &Agent{Fetcher: &fakeFetcher{snapshot: loadSnapshot(t)}}
	ref := transcriptPath(testSessionID)
	if _, err := agent.refresh(ref, testSessionID); err != nil {
		t.Fatal(err)
	}
	return ref
}
