package zcode

import (
	"path/filepath"
	"regexp"

	"github.com/entireio/external-agents/agents/entire-agent-zcode/internal/protocol"
)

// GetSessionDir returns the directory where Entire stores zcode session
// snapshots and exported transcripts. It deliberately lives OUTSIDE the repo:
// Entire attributes a session to the agent whose session dir contains its
// transcript path, and repo-local scratch dirs like .entire/tmp nest inside
// other external agents' session dirs (kilo, amp, ... use .entire/tmp), which
// misattributes zcode sessions to whichever such agent sorts first.
//
// ZCode keys sessions by ID globally (its SQLite store is not per-repo), so
// repoPath does not change the location; the dir hangs off ZCode's own home
// and follows the ZCODE_HOME override used by tests.
func (a *Agent) GetSessionDir(_ string) (string, error) {
	return entireSessionDir(), nil
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return protocol.ResolveSessionFile(sessionDir, sessionID)
}

// entireSessionDir is where write-session snapshots (<id>.json) and exported
// transcripts (<id>.jsonl) live.
func entireSessionDir() string {
	return filepath.Join(zcodeHome(), "entire", "sessions")
}

// transcriptPath is the JSONL transcript this agent exports from ZCode's
// SQLite store. One message per line so line index == message index, which is
// the unit Entire records via get-transcript-position.
func transcriptPath(sessionID string) string {
	return filepath.Join(entireSessionDir(), safeSessionID(sessionID)+".jsonl")
}

// sessionIDFromTranscriptRef recovers the ZCode session id from an exported
// transcript path (<session-dir>/<safe-id>.jsonl). Message ids do not encode
// the session id, so the filename is the source of truth.
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
