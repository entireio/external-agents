package goose

import (
	"os"
	"path/filepath"
)

// sessionsDir returns goose's session storage directory. Goose stores all
// sessions in a SQLite database (sessions.db) inside this directory.
// GOOSE_PATH_ROOT overrides all goose paths (used by goose for hermetic
// testing); otherwise the data dir follows XDG conventions.
func sessionsDir() string {
	if root := os.Getenv("GOOSE_PATH_ROOT"); root != "" {
		return filepath.Join(root, "data", "sessions")
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "goose", "sessions")
}

// GetSessionDir returns the session directory. Goose keys sessions by
// session ID globally (not per-repo), so the repo path does not change the
// location.
func (a *Agent) GetSessionDir(_ string) (string, error) {
	dir := sessionsDir()
	if dir == "" {
		return "", os.ErrNotExist
	}
	return dir, nil
}

// ResolveSessionFile returns the transcript file for a session. Goose has no
// native per-session file; prepare-transcript materializes one at this path
// via `goose session export`.
func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return filepath.Join(sessionDir, sessionID+".export.json")
}
