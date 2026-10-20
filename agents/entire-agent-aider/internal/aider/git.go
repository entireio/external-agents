package aider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

var aiderIdentity = regexp.MustCompile(`(?i)(?:<aider@aider\.chat>|^aider@aider\.chat$|\(aider\))`)
var commitRecord = regexp.MustCompile(`^>\s*Commit\s+([0-9a-fA-F]{7,64})\b`)

func IsAiderCommit(author, committer, message string) bool {
	if aiderIdentity.MatchString(author) || aiderIdentity.MatchString(committer) {
		return true
	}
	// Only the final trailer paragraph counts, not quoted attribution in prose.
	paragraphs := strings.Split(strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n")), "\n\n")
	for _, line := range strings.Split(paragraphs[len(paragraphs)-1], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "Co-authored-by") && aiderIdentity.MatchString(strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}
func gitOutput(repo string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return data, nil
}
func resolveCommit(repo, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" || strings.HasPrefix(ref, "-") {
		return "", errors.New("invalid commit reference")
	}
	data, err := gitOutput(repo, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(string(data)), err
}
func (a *Agent) CommitModifiedFiles(ref string) ([]string, error) {
	repo := RepoRoot()
	commits := []string{}
	if left, right, ok := strings.Cut(ref, ".."); ok {
		if strings.HasPrefix(right, ".") {
			return nil, errors.New("use a two-dot commit range")
		}
		first, err := resolveCommit(repo, left)
		if err != nil {
			return nil, err
		}
		last, err := resolveCommit(repo, right)
		if err != nil {
			return nil, err
		}
		data, err := gitOutput(repo, "rev-list", first+".."+last, "--")
		if err != nil {
			return nil, err
		}
		commits = strings.Fields(string(data))
	} else {
		commit, err := resolveCommit(repo, ref)
		if err != nil {
			return nil, err
		}
		commits = append(commits, commit)
	}
	files := map[string]bool{}
	for _, commit := range commits {
		data, err := gitOutput(repo, "show", "-s", "--format=%an <%ae>%x00%cn <%ce>%x00%B", commit, "--")
		if err != nil {
			return nil, err
		}
		fields := strings.SplitN(string(data), "\x00", 3)
		if len(fields) != 3 || !IsAiderCommit(fields[0], fields[1], fields[2]) {
			continue
		}
		data, err = gitOutput(repo, "diff-tree", "--root", "--no-commit-id", "--name-only", "--no-renames", "-r", "-m", "-z", commit, "--")
		if err != nil {
			return nil, err
		}
		for _, name := range strings.Split(string(data), "\x00") {
			if name != "" {
				files[name] = true
			}
		}
	}
	return sortedFiles(files), nil
}
func sortedFiles(files map[string]bool) []string {
	result := make([]string, 0, len(files))
	for file := range files {
		result = append(result, file)
	}
	sort.Strings(result)
	return result
}
func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	if offset < 0 {
		return nil, 0, errors.New("offset must be non-negative")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !strings.ContainsAny(path, `/\`) && !strings.HasSuffix(path, ".md") {
		files, err := a.CommitModifiedFiles(path)
		return files, 0, err
	}
	if err != nil {
		return nil, 0, err
	}
	files := map[string]bool{}
	refs := map[string]bool{}
	err = visitLines(data, offset, func(line string) {
		if match := commitRecord.FindStringSubmatch(line); match != nil {
			refs[match[1]] = true
		}
	})
	if err != nil {
		return nil, len(data), err
	}
	for ref := range refs {
		changed, err := a.CommitModifiedFiles(ref)
		if err != nil {
			return nil, len(data), err
		}
		for _, file := range changed {
			files[file] = true
		}
	}
	return sortedFiles(files), len(data), nil
}
