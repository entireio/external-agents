package pi

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return protocol.DefaultSessionDir(repoPath), nil
}

// ResolveSessionFile returns the transcript path for sessionID.
//
// Pi sessions live in two places:
//
//  1. <sessionDir>/<id>.json — a snapshot written by the agent_end hook so that
//     Entire can read a stable transcript during condensation.
//  2. ~/.pi/agent/sessions/--<escaped-repo>--/<timestamp>_<id>.jsonl — the live
//     pi session file that exists whether or not Entire hooks ran.
//
// The captured snapshot is preferred when present (it matches what condense
// already saw). For `entire attach` on sessions that ran without hooks
// installed, the snapshot does not exist, so fall back to the live file.
//
// The live-file fallback is scoped to the repo derived from sessionDir, not
// to the process-global ENTIRE_REPO_ROOT: the protocol accepts sessionDir as
// an argument specifically so callers can resolve sessions for a repo other
// than the one the binary was launched in.
func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	captured := protocol.ResolveSessionFile(sessionDir, sessionID)
	if _, err := os.Stat(captured); err == nil {
		return captured
	}
	if repoPath := repoRootFromSessionDir(sessionDir); repoPath != "" {
		if live := findPiSessionFile(repoPath, sessionID); live != "" {
			return live
		}
	}
	return captured
}

// repoRootFromSessionDir reverses protocol.DefaultSessionDir, which appends
// ".entire/tmp" to the repo root. Returns "" if sessionDir does not have that
// suffix — in that case the caller is using a non-standard layout and we
// cannot safely infer which repo to search for a live session file.
func repoRootFromSessionDir(sessionDir string) string {
	if sessionDir == "" {
		return ""
	}
	cleaned := filepath.Clean(sessionDir)
	parent := filepath.Dir(cleaned)
	if filepath.Base(cleaned) != "tmp" || filepath.Base(parent) != ".entire" {
		return ""
	}
	return filepath.Dir(parent)
}

// piSessionsBaseDir returns the root where the pi CLI stores per-repo session
// directories. Returns "" if the home directory cannot be determined.
func piSessionsBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

// piRepoDirName mirrors pi's session-directory naming scheme: the absolute
// repo path with '/' replaced by '-', wrapped with '--' on both sides.
//
// Example: /Users/soph/Work/entire/devenv/go-git-api
//
//	-> --Users-soph-Work-entire-devenv-go-git-api--
func piRepoDirName(repoPath string) string {
	trimmed := strings.TrimPrefix(repoPath, "/")
	return "--" + strings.ReplaceAll(trimmed, "/", "-") + "--"
}

// findPiSessionFile searches the live pi session directory for a transcript
// matching sessionID. Accepts either the full "<timestamp>_<uuid>" form or
// just the trailing "<uuid>". When multiple files match, the most recently
// modified is returned.
//
// Entries whose Info() fails (e.g., the file was rotated between ReadDir and
// the stat) are skipped rather than crashing the resolver: this code reads a
// live session directory, so transient disappearances are expected.
func findPiSessionFile(repoPath, sessionID string) string {
	if sessionID == "" || repoPath == "" {
		return ""
	}
	base := piSessionsBaseDir()
	if base == "" {
		return ""
	}
	dir := filepath.Join(base, piRepoDirName(repoPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type match struct {
		path    string
		modTime time.Time
	}
	var matches []match
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		stem := strings.TrimSuffix(name, ".jsonl")
		if stem == name {
			continue // not a .jsonl file
		}
		if stem != sessionID && !strings.HasSuffix(stem, "_"+sessionID) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // file vanished between ReadDir and stat — skip
		}
		matches = append(matches, match{
			path:    filepath.Join(dir, name),
			modTime: info.ModTime(),
		})
	}
	if len(matches) == 0 {
		return ""
	}
	// Prefer newest mtime when a short UUID matches multiple timestamped files.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
	return matches[0].path
}
