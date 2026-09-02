package devin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// TranscriptsDir returns the directory where Devin CLI stores canonical
// session transcripts: <data-dir>/cli/transcripts. The directory is flat —
// one <session_id>.json per session, not per-project.
func TranscriptsDir() (string, error) {
	if override := os.Getenv("ENTIRE_TEST_DEVIN_TRANSCRIPT_DIR"); override != "" {
		return override, nil
	}
	dataDir, err := devinDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "cli", "transcripts"), nil
}

// devinDataDir returns Devin's per-user data directory
// (~/.local/share/devin on Unix, %APPDATA%\devin on Windows).
func devinDataDir() (string, error) {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("APPDATA environment variable not set")
		}
		return filepath.Join(appData, "devin"), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "devin"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".local", "share", "devin"), nil
}

// GetSessionDir returns the directory where Devin stores session transcripts.
// Devin's transcript directory is flat, so repoPath is unused.
func (a *Agent) GetSessionDir(_ string) (string, error) {
	return TranscriptsDir()
}

// ResolveSessionFile returns the path to a Devin transcript file.
// Devin names transcripts directly as <session_id>.json.
func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return filepath.Join(sessionDir, sessionID+".json")
}

// sessionRefForID computes the canonical transcript path for a session ID.
// Returns "" when the transcript directory cannot be resolved (home dir /
// APPDATA unavailable — pathological).
func sessionRefForID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	dir, err := TranscriptsDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, sessionID+".json")
}
