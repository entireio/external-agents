package zcode

import (
	"path/filepath"
	"regexp"

	"github.com/entireio/external-agents/agents/entire-agent-zcode/internal/protocol"
)

const transcriptSubdir = "zcode"

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return protocol.DefaultSessionDir(repoPath), nil
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return protocol.ResolveSessionFile(sessionDir, sessionID)
}

// transcriptPath is the JSONL transcript this agent exports from ZCode's
// SQLite store. One message per line so line index == message index, which is
// the unit Entire records via get-transcript-position.
func transcriptPath(sessionID string) string {
	return filepath.Join(protocol.DefaultSessionDir(protocol.RepoRoot()), transcriptSubdir, safeSessionID(sessionID)+".jsonl")
}

// sessionIDFromTranscriptRef recovers the ZCode session id from an exported
// transcript path (<session_dir>/zcode/<safe-id>.jsonl). Message ids do not
// encode the session id, so the filename is the source of truth.
func sessionIDFromTranscriptRef(sessionRef string) string {
	base := filepath.Base(sessionRef)
	ext := filepath.Ext(base)
	if ext == "" {
		return base
	}
	return base[:len(base)-len(ext)]
}

var unsafeSessionIDChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func safeSessionID(sessionID string) string {
	if sessionID == "" {
		return "unknown"
	}
	return unsafeSessionIDChars.ReplaceAllString(sessionID, "_")
}
