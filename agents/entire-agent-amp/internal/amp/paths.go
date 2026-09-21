package amp

import (
	"path/filepath"
	"regexp"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return protocol.DefaultSessionDir(repoPath), nil
}

// safePathSessionID replaces runs outside A-Za-z0-9_.- with an underscore
// and maps an empty ID to "unknown". Dots are preserved; ResolveSessionFile
// appends .json so even "." and ".." become filenames rather than path segments.
// This matches the sanitization used by transcriptPath in hooks.go.
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
