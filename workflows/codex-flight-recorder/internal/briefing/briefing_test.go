package briefing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(history, []byte(`[{"session_id":"s-1","files_touched":["payments/service.go"],"test_result":"failed","retries":3,"revert_count":1,"risk_score":0.9,"summary":"Webhook migration previously failed."}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := fakeRunner{responses: map[string][]byte{
		"entire checkpoint": []byte(`[{"id":"cp-1"}]`),
		"entire graph":      []byte(`{"results":[{"file_path":"payments/service.go","focus_line":10,"symbol_name":"Charge"},{"file_path":"payments/webhook.go","focus_line":12,"symbol_name":"Verify"}]}`),
		"git ls-files":      []byte("payments/service_test.go\npayments/webhook_test.go\nother/other_test.go\n"),
	}, errors: map[string]error{}}
	b, err := Build(runner, Request{Repo: dir, Task: "replace payment gateway", Files: []string{"payments/service.go"}, HistoryPath: history, HistorySource: "synthetic test history"})
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
	if b.History.Source != "synthetic test history" {
		t.Fatalf("history source = %q", b.History.Source)
	}
	if b.History.MaxRiskScore != 0.9 {
		t.Fatalf("max risk score = %v", b.History.MaxRiskScore)
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

func TestParseHistoryKeepsReviewedJSONArrayCompatible(t *testing.T) {
	parsed, err := parseHistory([]byte(`[{"session_id":"reviewed-1","files_touched":["payments/service.go"],"test_result":"failed","retries":2,"revert_count":1,"risk_score":0.8,"summary":"Reviewed export."}]`))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Partial || len(parsed.IgnoredEvents) != 0 || len(parsed.Records) != 1 {
		t.Fatalf("parsed = %#v", parsed)
	}
	record := parsed.Records[0]
	if record.SessionID != "reviewed-1" || record.Retries != 2 || record.RevertCount != 1 || record.RiskScore != 0.8 {
		t.Fatalf("record = %#v", record)
	}
}

func TestParseHistoryNormalizesTrack3JSONL(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "track-3-agent-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseHistory(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Partial || len(parsed.Records) != 1 || len(parsed.IgnoredEvents) != 0 {
		t.Fatalf("parsed = %#v", parsed)
	}
	record := parsed.Records[0]
	if record.SessionID != "btw-track3-demo-001" || record.TestResult != "failed" || record.Retries != 1 {
		t.Fatalf("record = %#v", record)
	}
	if len(record.Files) != 2 || !strings.Contains(record.Summary, "Historical checkpoint") || !strings.Contains(record.Summary, "Open question") {
		t.Fatalf("record = %#v", record)
	}
}

func TestLoadHistoryReportsUnknownJSONLEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown.jsonl")
	data := "{\"event\":\"session_started\",\"session_id\":\"s-1\"}\n" +
		"{\"event\":\"future_event\",\"session_id\":\"s-1\"}\n" +
		"{\"event\":\"file_changed\",\"session_id\":\"s-1\",\"path\":\"payments/service.go\"}\n" +
		"{\"event\":\"session_ended\",\"session_id\":\"s-1\",\"status\":\"completed\"}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	b := Briefing{AffectedFiles: []string{"payments/service.go"}}
	b.loadHistory(path, "test JSONL")
	if len(b.History.IgnoredEvents) != 1 || b.History.IgnoredEvents[0] != "future_event (1)" {
		t.Fatalf("ignored events = %#v", b.History.IgnoredEvents)
	}
	if !containsWarning(b.Warnings, "ignored unknown event types") {
		t.Fatalf("warnings = %#v", b.Warnings)
	}
}

func TestLoadHistoryLabelsIncompleteJSONLPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.jsonl")
	data := "{\"event\":\"session_started\",\"session_id\":\"s-1\"}\n" +
		"{\"event\":\"file_changed\",\"session_id\":\"s-1\",\"path\":\"payments/service.go\",\"summary\":\"Changed service.\"}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	b := Briefing{AffectedFiles: []string{"payments/service.go"}}
	b.loadHistory(path, "test JSONL")
	if b.History.Status != "PARTIAL" || b.History.MatchedSessions != 1 {
		t.Fatalf("history = %#v", b.History)
	}
	if !containsWarning(b.Warnings, "requires verification") {
		t.Fatalf("warnings = %#v", b.Warnings)
	}
}

func containsWarning(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}
