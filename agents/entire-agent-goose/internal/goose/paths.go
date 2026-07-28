package goose

import (
	"os"
	"path/filepath"
)

// Goose stores sessions in a SQLite database (sessions.db) inside its data
// directory; there are no native per-session files. Transcripts materialized
// by prepare-transcript / parse-hook (via `goose session export`) are written
// alongside the database as <session-id>.json.
//
// The session dir deliberately lives OUTSIDE the repo: Entire attributes a
// session to the agent whose session dir contains its transcript path, and
// repo-local scratch dirs like .entire/tmp nest inside other external
// agents' session dirs, which misattributes sessions when several
// entire-agent-* binaries are installed.

// GetSessionDir returns goose's session storage directory. Goose keys
// sessions by ID globally (not per-repo), so repoPath does not change the
// location. GOOSE_PATH_ROOT overrides all goose paths (goose's own hermetic
// test mechanism); otherwise the data dir follows XDG conventions.
func (a *Agent) GetSessionDir(_ string) (string, error) {
	if root := os.Getenv("GOOSE_PATH_ROOT"); root != "" {
		return filepath.Join(root, "data", "sessions"), nil
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "goose", "sessions"), nil
}

func (a *Agent) ResolveSessionFile(sessionDirPath, sessionID string) string {
	return filepath.Join(sessionDirPath, sessionID+".json")
}

// transcriptPath is the materialized export location for a session.
func transcriptPath(sessionID string) string {
	a := &Agent{}
	dir, err := a.GetSessionDir("")
	if err != nil {
		return ""
	}
	return filepath.Join(dir, sessionID+".json")
}
