package vibe

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

const (
	transcriptSubdir = "mistral-vibe"
	transcriptExt    = ".jsonl"
)

// GetSessionDir returns a per-repository directory in the user's cache.
//
// It is deliberately not under <repo>/.entire/tmp: Entire attributes a
// transcript to the first agent whose session directory contains it, and
// several agents claim .entire/tmp as a whole, so a transcript stored there is
// taken for theirs and every hook is skipped as "forwarded".
func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return sessionDir(repoPath), nil
}

func sessionDir(repoPath string) string {
	if strings.TrimSpace(repoPath) == "" {
		repoPath = protocol.RepoRoot()
	}
	// Hooks and git see the same repository through different spellings
	// (/var vs /private/var on macOS); resolve so both map to one directory.
	if resolved, err := filepath.EvalSymlinks(repoPath); err == nil {
		repoPath = resolved
	}
	sum := sha256.Sum256([]byte(repoPath))
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "entire", transcriptSubdir, hex.EncodeToString(sum[:8]))
}

var sessionIDPathSanitizer = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// safeSessionID preserves non-device IDs made only of A-Za-z0-9_.-. Other IDs
// use a reserved prefix plus a stable hash, so transformed IDs cannot collide
// with unchanged IDs or collapse onto one another. Dots are safe because
// callers append a filename extension, so "." and ".." cannot become path
// segments. Vibe session IDs are UUIDs and pass through unchanged.
func safeSessionID(sessionID string) string {
	safeID := sessionIDPathSanitizer.ReplaceAllString(sessionID, "_")
	if safeID != "" && safeID == sessionID && !isWindowsDeviceName(sessionID) {
		return safeID
	}
	sum := sha256.Sum256([]byte(sessionID))
	return "~" + hex.EncodeToString(sum[:16])
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return filepath.Join(sessionDir, safeSessionID(sessionID)+transcriptExt)
}

func transcriptPath(sessionID string) string {
	return filepath.Join(sessionDir(protocol.RepoRoot()), safeSessionID(sessionID)+transcriptExt)
}

// sessionIDFromRef recovers the session id from a transcript path. It returns
// "" for a hashed name, which cannot be reversed.
func sessionIDFromRef(sessionRef string) string {
	name := strings.TrimSuffix(filepath.Base(sessionRef), transcriptExt)
	if name == "" || strings.HasPrefix(name, "~") || name == filepath.Base(sessionRef) {
		return ""
	}
	return name
}
