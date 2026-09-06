package briefing

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeRunner struct {
	responses map[string][]byte
	errors    map[string]error
}

func (f fakeRunner) Run(_ string, name string, args ...string) ([]byte, error) {
	key := name + " " + args[0]
	if err := f.errors[key]; err != nil {
		return nil, err
	}
	return f.responses[key], nil
}

func TestBuildCombinesEntireGraphAndReviewedHistory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "payments"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"service.go", "webhook.go"} {
		if err := os.WriteFile(filepath.Join(dir, "payments", name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	history := filepath.Join(dir, "history.json")
	if err := os.WriteFile(history, []byte(`[{"session_id":"s-1","files_touched":["payments/service.go"],"test_result":"failed","retries":3,"revert_count":1,"summary":"Webhook migration previously failed."}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := fakeRunner{responses: map[string][]byte{
		"entire checkpoint": []byte(`[{"id":"cp-1"}]`),
		"entire graph":      []byte(`{"results":[{"file_path":"payments/service.go","focus_line":10,"symbol_name":"Charge"},{"file_path":"payments/webhook.go","focus_line":12,"symbol_name":"Verify"}]}`),
		"git ls-files":      []byte("payments/service_test.go\npayments/webhook_test.go\nother/other_test.go\n"),
	}, errors: map[string]error{}}
	b, err := Build(runner, Request{Repo: dir, Task: "replace payment gateway", HistoryPath: history})
	if err != nil {
		t.Fatal(err)
	}
	if b.Risk != "HIGH" {
		t.Fatalf("risk = %s, want HIGH", b.Risk)
	}
	if b.CheckpointCount != 1 || !b.CheckpointAvailable {
		t.Fatalf("checkpoint evidence = %#v", b)
	}
	if !b.Graph.SearchAvailable || !b.Graph.ImpactAvailable {
		t.Fatalf("graph evidence = %#v", b.Graph)
	}
	if b.History.FailedSessions != 1 || b.History.Retries != 3 || b.History.Reverts != 1 {
		t.Fatalf("history = %#v", b.History)
	}
	if len(b.RecommendedTests) != 2 {
		t.Fatalf("tests = %#v", b.RecommendedTests)
	}
}

func TestBuildLabelsUnavailableEvidenceInsteadOfInventingIt(t *testing.T) {
	runner := fakeRunner{responses: map[string][]byte{"git ls-files": []byte("")}, errors: map[string]error{
		"entire checkpoint": errors.New("offline"), "entire graph": errors.New("graph disabled"),
	}}
	b, err := Build(runner, Request{Task: "change authentication"})
	if err != nil {
		t.Fatal(err)
	}
	if b.CheckpointAvailable || b.Graph.SearchAvailable || len(b.Warnings) != 2 {
		t.Fatalf("briefing = %#v", b)
	}
}

func TestBuildRequiresTask(t *testing.T) {
	_, err := Build(fakeRunner{}, Request{})
	if err == nil {
		t.Fatal("expected task validation error")
	}
}
