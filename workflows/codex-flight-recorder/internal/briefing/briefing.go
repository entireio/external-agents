// Package briefing builds evidence-backed, pre-change context for Codex.
package briefing

import (
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
	Repo        string
	Task        string
	Files       []string
	HistoryPath string
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
	MatchedSessions int      `json:"matched_sessions"`
	FailedSessions  int      `json:"failed_sessions"`
	Retries         int      `json:"retries"`
	Reverts         int      `json:"reverts"`
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
	Summary     string   `json:"summary"`
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
	b.loadHistory(request.HistoryPath)
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

func (b *Briefing) loadHistory(path string) {
	if path == "" {
		return
	}
	data, err := readFile(path)
	if err != nil {
		b.Warnings = append(b.Warnings, "Historical analytics file is unavailable: "+err.Error())
		return
	}
	var records []historyRecord
	if err := json.Unmarshal(data, &records); err != nil {
		b.Warnings = append(b.Warnings, "Historical analytics file is invalid: "+err.Error())
		return
	}
	b.History.Available = true
	b.History.Source = "local development-history export (verify provenance before use)"
	for _, record := range records {
		if !overlaps(record.Files, b.AffectedFiles) {
			continue
		}
		b.History.MatchedSessions++
		b.History.Retries += record.Retries
		b.History.Reverts += record.RevertCount
		if strings.EqualFold(record.TestResult, "failed") {
			b.History.FailedSessions++
		}
		if record.Summary != "" {
			b.History.Findings = append(b.History.Findings, record.Summary)
		}
	}
	b.History.Findings = unique(b.History.Findings)
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
