package kiro

import (
	"path/filepath"
	"regexp"

	"github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"
)

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return protocol.DefaultSessionDir(repoPath), nil
}

// safePathSessionID strips every character that could move a session file
// out of its session directory (path separators, "..", control characters).
// An empty result falls back to "unknown" so callers always get a real
// filename. cacheTranscriptPath writes transcripts through this path, so
// sanitizing here protects kiro's transcript write site.
func safePathSessionID(sessionID string) string {
	if sessionID == "" {
		return "unknown"
	}
	return sessionIDPathSanitizer.ReplaceAllString(sessionID, "_")
}

var sessionIDPathSanitizer = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return filepath.Join(sessionDir, safePathSessionID(sessionID)+".json")
}
