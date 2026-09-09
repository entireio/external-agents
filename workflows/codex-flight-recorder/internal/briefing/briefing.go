// Package briefing builds evidence-backed, pre-change context for Codex.
package briefing

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Runner interface {
	Run(dir string, name string, args ...string) ([]byte, error)
}

type Request struct {
	Repo          string
	Task          string
	Files         []string
	HistoryPath   string
	HistorySource string
}

type Briefing struct {
	Task                string          `json:"task"`
	Risk                string          `json:"risk"`
	AffectedFiles       []string        `json:"affected_files"`
	RecommendedTests    []string        `json:"recommended_tests"`
	CheckpointCount     int             `json:"checkpoint_count"`
	CheckpointAvailable bool            `json:"checkpoint_available"`
	Graph               GraphEvidence   `json:"graph"`
	History             HistoryEvidence `json:"history"`
	Warnings            []string        `json:"warnings,omitempty"`
}

type GraphEvidence struct {
	SearchAvailable bool     `json:"search_available"`
	ImpactAvailable bool     `json:"impact_available"`
	Symbols         []string `json:"symbols,omitempty"`
	ImpactSummary   string   `json:"impact_summary,omitempty"`
}

type HistoryEvidence struct {
	Available       bool     `json:"available"`
	Source          string   `json:"source,omitempty"`
	Status          string   `json:"status,omitempty"`
	MatchedSessions int      `json:"matched_sessions"`
	FailedSessions  int      `json:"failed_sessions"`
	Retries         int      `json:"retries"`
	Reverts         int      `json:"reverts"`
	MaxRiskScore    float64  `json:"max_risk_score"`
	IgnoredEvents   []string `json:"ignored_events,omitempty"`
	Findings        []string `json:"findings,omitempty"`
}

type graphSearch struct {
	Results []struct {
		FilePath   string `json:"file_path"`
		FocusLine  int    `json:"focus_line"`
		SymbolName string `json:"symbol_name"`
	} `json:"results"`
}

type historyRecord struct {
	SessionID   string   `json:"session_id"`
	Files       []string `json:"files_touched"`
	Tests       []string `json:"tests"`
	TestResult  string   `json:"test_result"`
	Retries     int      `json:"retries"`
	RevertCount int      `json:"revert_count"`
	RiskScore   float64  `json:"risk_score"`
	Summary     string   `json:"summary"`
}

type parsedHistory struct {
	Records       []historyRecord
	Partial       bool
	IgnoredEvents []string
}

type lifecycleEvent struct {
	Event         string          `json:"event"`
	SessionID     string          `json:"session_id"`
	Repository    string          `json:"repository"`
	Branch        string          `json:"branch"`
	Path          string          `json:"path"`
	Summary       string          `json:"summary"`
	Intent        string          `json:"intent"`
	OpenQuestions []string        `json:"open_questions"`
	Status        string          `json:"status"`
	Output        json.RawMessage `json:"output"`
	Agent         struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"agent"`
}

type lifecycleSession struct {
	record       historyRecord
	ended        bool
	failedTools  int
	failedSeen   bool
	startFinding string
}

func Build(r Runner, request Request) (Briefing, error) {
	if strings.TrimSpace(request.Task) == "" {
		return Briefing{}, fmt.Errorf("task is required")
	}
	if request.Repo == "" {
		request.Repo = "."
	}
	repo, err := filepath.Abs(request.Repo)
	if err != nil {
		return Briefing{}, fmt.Errorf("resolve repository: %w", err)
	}
	request.Repo = repo

	b := Briefing{Task: request.Task}
	b.loadCheckpoints(r, request.Repo)
	b.loadGraph(r, request)
	b.AffectedFiles = unique(append(request.Files, b.AffectedFiles...))
	b.loadHistory(request.HistoryPath, request.HistorySource)
	b.RecommendedTests = recommendTests(r, request.Repo, b.AffectedFiles)
	b.Risk = assessRisk(b)
	return b, nil
}

func (b *Briefing) loadCheckpoints(r Runner, repo string) {
	out, err := r.Run(repo, "entire", "checkpoint", "list", "--json", "--no-pager")
	if err != nil {
		b.Warnings = append(b.Warnings, "Entire checkpoint history is unavailable: "+err.Error())
		return
	}
	b.CheckpointAvailable = true
	var items []json.RawMessage
	if json.Unmarshal(out, &items) == nil {
		b.CheckpointCount = len(items)
	}
}

func (b *Briefing) loadGraph(r Runner, request Request) {
	out, err := r.Run(request.Repo, "entire", "graph", "search", "--repo", request.Repo, "--profile", "full", "--query", request.Task)
	if err != nil {
		b.Warnings = append(b.Warnings, "Entire Graph search is unavailable: "+err.Error())
		return
	}
	b.Graph.SearchAvailable = true
	var result graphSearch
	if err := json.Unmarshal(out, &result); err != nil {
		b.Warnings = append(b.Warnings, "Entire Graph returned unreadable search output: "+err.Error())
		return
	}
	impactTarget := ""
	requestedFiles := make(map[string]bool, len(request.Files))
	for _, file := range request.Files {
		requestedFiles[file] = true
	}
	hasRequestedFiles := len(requestedFiles) > 0
	for _, hit := range result.Results {
		if hit.FilePath != "" && isRepositoryFile(request.Repo, hit.FilePath) {
			b.AffectedFiles = append(b.AffectedFiles, hit.FilePath)
			if hit.SymbolName != "" {
				b.Graph.Symbols = append(b.Graph.Symbols, hit.SymbolName)
			}
			if hit.FocusLine > 0 && (!hasRequestedFiles || requestedFiles[hit.FilePath]) && (impactTarget == "" || requestedFiles[hit.FilePath]) {
				impactTarget = fmt.Sprintf("%s:%d", hit.FilePath, hit.FocusLine)
			}
		}
	}
	b.AffectedFiles = unique(b.AffectedFiles)
	b.Graph.Symbols = unique(b.Graph.Symbols)
	if hasRequestedFiles && impactTarget == "" {
		impactTarget = b.lookupRequestedImpactTarget(r, request.Repo, requestedFiles)
	}
	if impactTarget == "" {
		if hasRequestedFiles {
			b.Warnings = append(b.Warnings, "Entire Graph found no resolvable symbol in the explicitly requested files; no unrelated impact result was used")
		}
		return
	}
	impact, err := r.Run(request.Repo, "entire", "graph", "impact", "--repo", request.Repo, "--symbol", impactTarget, "--format", "json", "--limit", "10")
	if err != nil {
		b.Warnings = append(b.Warnings, "Entire Graph impact is unavailable: "+err.Error())
		return
	}
	b.Graph.ImpactAvailable = true
	b.Graph.ImpactSummary = truncate(strings.TrimSpace(string(impact)), 700)
}

func (b *Briefing) lookupRequestedImpactTarget(r Runner, repo string, requestedFiles map[string]bool) string {
	files := make([]string, 0, len(requestedFiles))
	for file := range requestedFiles {
		files = append(files, file)
	}
	out, err := r.Run(repo, "entire", "graph", "search", "--repo", repo, "--profile", "full", "--query", strings.Join(files, " "))
	if err != nil {
		return ""
	}
	var result graphSearch
	if json.Unmarshal(out, &result) != nil {
		return ""
	}
	for _, hit := range result.Results {
		if requestedFiles[hit.FilePath] && hit.FocusLine > 0 && isRepositoryFile(repo, hit.FilePath) {
			return fmt.Sprintf("%s:%d", hit.FilePath, hit.FocusLine)
		}
	}
	return ""
}

func isRepositoryFile(repo, graphPath string) bool {
	if filepath.IsAbs(graphPath) || strings.HasPrefix(graphPath, "..") {
		return false
	}
	info, err := os.Stat(filepath.Join(repo, graphPath))
	return err == nil && !info.IsDir()
}

func (b *Briefing) loadHistory(path, source string) {
	if path == "" {
		return
	}
	data, err := readFile(path)
	if err != nil {
		b.Warnings = append(b.Warnings, "Historical analytics file is unavailable: "+err.Error())
		return
	}
	parsed, err := parseHistory(data)
	if err != nil {
		b.Warnings = append(b.Warnings, "Historical analytics file is invalid: "+err.Error())
		return
	}
	b.History.Available = true
	b.History.Status = "COMPLETE"
	if parsed.Partial {
		b.History.Status = "PARTIAL"
		b.Warnings = append(b.Warnings, "Historical JSONL evidence is PARTIAL because it ends before session_ended; it requires verification")
	}
	b.History.IgnoredEvents = parsed.IgnoredEvents
	if len(parsed.IgnoredEvents) > 0 {
		b.Warnings = append(b.Warnings, "Historical JSONL ignored unknown event types: "+strings.Join(parsed.IgnoredEvents, ", "))
	}
	b.History.Source = source
	if b.History.Source == "" {
		b.History.Source = "local development-history export (verify provenance before use)"
	}
	for _, record := range parsed.Records {
		if !overlaps(record.Files, b.AffectedFiles) {
			continue
		}
		b.History.MatchedSessions++
		b.History.Retries += record.Retries
		b.History.Reverts += record.RevertCount
		if record.RiskScore > b.History.MaxRiskScore {
			b.History.MaxRiskScore = record.RiskScore
		}
		if strings.EqualFold(record.TestResult, "failed") {
			b.History.FailedSessions++
		}
		if record.Summary != "" {
			b.History.Findings = append(b.History.Findings, record.Summary)
		}
	}
	b.History.Findings = unique(b.History.Findings)
}

// parseHistory keeps the reviewed JSON-array format intact while accepting
// line-delimited lifecycle exports and normalizing both to historyRecord.
func parseHistory(data []byte) (parsedHistory, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return parsedHistory{}, fmt.Errorf("history file is empty")
	}
	if trimmed[0] == '[' {
		var records []historyRecord
		if err := json.Unmarshal(trimmed, &records); err != nil {
			return parsedHistory{}, err
		}
		return parsedHistory{Records: records}, nil
	}
	return parseLifecycleJSONL(trimmed)
}

func parseLifecycleJSONL(data []byte) (parsedHistory, error) {
	sessions := map[string]*lifecycleSession{}
	ignored := map[string]int{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var event lifecycleEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return parsedHistory{}, fmt.Errorf("parse JSONL line %d: %w", line, err)
		}
		if event.Event == "" {
			ignored["(missing event)"]++
			continue
		}
		if !knownLifecycleEvent(event.Event) {
			ignored[event.Event]++
			continue
		}
		if event.SessionID == "" {
			continue
		}
		session := sessions[event.SessionID]
		if session == nil {
			session = &lifecycleSession{record: historyRecord{SessionID: event.SessionID}}
			sessions[event.SessionID] = session
		}
		switch event.Event {
		case "session_started":
			session.startFinding = sessionStartedFinding(event)
		case "file_changed":
			session.record.Files = append(session.record.Files, event.Path)
			if event.Summary != "" {
				session.record.Summary = appendFinding(session.record.Summary, event.Summary)
			}
		case "tool_result":
			var output struct {
				ExitCode *int   `json:"exit_code"`
				Summary  string `json:"summary"`
			}
			if json.Unmarshal(event.Output, &output) == nil && output.ExitCode != nil {
				if *output.ExitCode != 0 {
					session.failedTools++
					session.failedSeen = true
					if output.Summary != "" {
						session.record.Summary = appendFinding(session.record.Summary, "Tool failure: "+output.Summary)
					}
				} else if session.failedSeen {
					session.record.Retries++
				}
			}
		case "checkpoint_created":
			if event.Summary != "" {
				session.record.Summary = appendFinding(session.record.Summary, "Historical checkpoint: "+event.Summary)
			}
			if event.Intent != "" {
				session.record.Summary = appendFinding(session.record.Summary, "Intent: "+event.Intent)
			}
			for _, question := range event.OpenQuestions {
				session.record.Summary = appendFinding(session.record.Summary, "Open question: "+question)
			}
		case "session_ended":
			session.ended = true
			if !strings.EqualFold(event.Status, "completed") && event.Status != "" {
				session.record.TestResult = "failed"
				session.record.Summary = appendFinding(session.record.Summary, "Session ended with status: "+event.Status)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedHistory{}, fmt.Errorf("scan JSONL: %w", err)
	}
	result := parsedHistory{IgnoredEvents: ignoredEventLabels(ignored)}
	for _, id := range sortedSessionIDs(sessions) {
		session := sessions[id]
		session.record.Files = unique(session.record.Files)
		if session.startFinding != "" {
			session.record.Summary = appendFinding(session.startFinding, session.record.Summary)
		}
		if session.failedTools > 0 {
			session.record.TestResult = "failed"
		}
		if !session.ended {
			result.Partial = true
		}
		result.Records = append(result.Records, session.record)
	}
	return result, nil
}

func knownLifecycleEvent(event string) bool {
	switch event {
	case "session_started", "user_prompt", "agent_response", "tool_call", "tool_result", "file_read", "file_changed", "usage", "checkpoint_created", "session_ended":
		return true
	default:
		return false
	}
}

func sessionStartedFinding(event lifecycleEvent) string {
	parts := []string{"Session started"}
	if event.Agent.Name != "" {
		parts = append(parts, "agent "+event.Agent.Name+" "+event.Agent.Version)
	}
	if event.Repository != "" {
		parts = append(parts, event.Repository)
	}
	if event.Branch != "" {
		parts = append(parts, "branch "+event.Branch)
	}
	return strings.Join(parts, "; ")
}

func appendFinding(existing, finding string) string {
	if finding == "" {
		return existing
	}
	if existing == "" {
		return finding
	}
	return existing + " | " + finding
}

func sortedSessionIDs(sessions map[string]*lifecycleSession) []string {
	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func ignoredEventLabels(events map[string]int) []string {
	labels := make([]string, 0, len(events))
	for event, count := range events {
		labels = append(labels, fmt.Sprintf("%s (%d)", event, count))
	}
	sort.Strings(labels)
	return labels
}

func recommendTests(r Runner, repo string, files []string) []string {
	out, err := r.Run(repo, "git", "ls-files")
	if err != nil {
		return nil
	}
	var tests []string
	for _, candidate := range strings.Fields(string(out)) {
		if !strings.HasSuffix(candidate, "_test.go") {
			continue
		}
		for _, file := range files {
			if filepath.Dir(candidate) == filepath.Dir(file) || strings.Contains(filepath.Base(candidate), strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))) {
				tests = append(tests, candidate)
				break
			}
		}
	}
	return unique(tests)
}

func assessRisk(b Briefing) string {
	score := 0
	if len(b.AffectedFiles) >= 3 {
		score += 2
	} else if len(b.AffectedFiles) > 0 {
		score++
	}
	if b.Graph.ImpactAvailable {
		score++
	}
	score += b.History.FailedSessions + b.History.Reverts
	if b.History.Retries >= 3 {
		score++
	}
	if b.History.MaxRiskScore >= 0.75 {
		score++
	}
	if score >= 4 {
		return "HIGH"
	}
	if score >= 2 {
		return "MEDIUM"
	}
	return "LOW"
}

func overlaps(left, right []string) bool {
	for _, item := range left {
		for _, other := range right {
			if item == other {
				return true
			}
		}
	}
	return false
}

func unique(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "…"
}
