package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/entireio/external-agents/workflows/codex-flight-recorder/internal/briefing"
)

type commandRunner struct{}

func (commandRunner) Run(dir string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, fmt.Errorf("%s timed out after 10 seconds", name)
	}
	return out, err
}

func main() {
	task := flag.String("task", "", "The requested coding change")
	repo := flag.String("repo", ".", "Repository root")
	files := flag.String("files", "", "Comma-separated files already known to be affected")
	history := flag.String("history", "", "Path to a reviewed development-history JSON export")
	format := flag.String("format", "markdown", "Output format: markdown or json")
	flag.Parse()

	b, err := briefing.Build(commandRunner{}, briefing.Request{Repo: *repo, Task: *task, Files: split(*files), HistoryPath: *history})
	if err != nil {
		fmt.Fprintln(os.Stderr, "codebase-flight-recorder:", err)
		os.Exit(2)
	}
	if *format == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(b)
		return
	}
	if *format != "markdown" {
		fmt.Fprintln(os.Stderr, "codebase-flight-recorder: format must be markdown or json")
		os.Exit(2)
	}
	printMarkdown(b)
}

func split(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func printMarkdown(b briefing.Briefing) {
	fmt.Println("# BEFORE YOU CODE")
	fmt.Println()
	fmt.Println("**Risk:**", b.Risk)
	fmt.Println("**Task:**", b.Task)
	fmt.Println("**Entire checkpoints:**", b.CheckpointCount)
	section("Affected files", b.AffectedFiles)
	section("Graph symbols", b.Graph.Symbols)
	section("Recommended tests", b.RecommendedTests)
	if b.Graph.ImpactSummary != "" {
		fmt.Println("## Graph impact\n\n```text\n" + b.Graph.ImpactSummary + "\n```")
	}
	if b.History.Available {
		fmt.Printf("## Historical development evidence\n\nSource: %s\n\nMatched sessions: %d; failed: %d; retries: %d; reverts: %d\n", b.History.Source, b.History.MatchedSessions, b.History.FailedSessions, b.History.Retries, b.History.Reverts)
		section("Findings", b.History.Findings)
	}
	section("Warnings", b.Warnings)
}

func section(title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Println("##", title)
	for _, value := range values {
		fmt.Println("-", value)
	}
	fmt.Println()
}
