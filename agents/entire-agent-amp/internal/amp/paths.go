package amp

import (
	"path/filepath"
	"regexp"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return protocol.DefaultSessionDir(repoPath), nil
}

// safePathSessionID strips every character that could move a session file
// out of its session directory (path separators, "..", control characters).
// An empty result falls back to "unknown" so callers always get a real
// filename. This mirrors the sanitization already applied to amp's
// transcriptPath in hooks.go, so both write paths now share the same
// defensive contract.
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
