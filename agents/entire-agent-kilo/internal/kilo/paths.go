package kilo

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
)

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return filepath.Join(protocol.DefaultSessionDir(repoPath), transcriptSubdir), nil
}

var sessionIDPathSanitizer = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// safeSessionID preserves non-device IDs made only of A-Za-z0-9_.-. Other IDs
// use a reserved prefix plus a stable hash, so transformed IDs cannot collide
// with unchanged IDs or collapse onto one another through normalization.
// Dots are safe here because callers append a filename extension, so even "."
// and ".." cannot become path segments.
func safeSessionID(sessionID string) string {
	safeID := sessionIDPathSanitizer.ReplaceAllString(sessionID, "_")
	if safeID != "" && safeID == sessionID && !isWindowsDeviceName(sessionID) {
		return safeID
	}
	sum := sha256.Sum256([]byte(sessionID))
	return "~" + hex.EncodeToString(sum[:16])
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return filepath.Join(sessionDir, safeSessionID(sessionID)+".json")
}
