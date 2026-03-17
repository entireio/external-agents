package kiro

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/obra/external-agents/agents/entire-agent-kiro/internal/protocol"
)

var runSQLiteCommand = func(args ...string) ([]byte, error) {
	return exec.Command("sqlite3", args...).Output()
}

type ideSessionIndexEntry struct {
	SessionID   string `json:"sessionId"`
	DateCreated string `json:"dateCreated"`
}

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, errors.New("max-size must be greater than zero")
	}
	if len(content) == 0 {
		return [][]byte{[]byte{}}, nil
	}

	var chunks [][]byte
	for start := 0; start < len(content); start += maxSize {
		end := start + maxSize
		if end > len(content) {
			end = len(content)
		}
		chunk := make([]byte, end-start)
		copy(chunk, content[start:end])
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, nil), nil
}

func (a *Agent) querySessionID(cwd string) (string, error) {
	db, err := kiroCLIDataDBPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(db); err != nil {
		return "", fmt.Errorf("kiro database not found at %s: %w", db, err)
	}

	query := fmt.Sprintf(
		"SELECT json_extract(value, '$.conversation_id') FROM conversations_v2 WHERE key = '%s' ORDER BY updated_at DESC LIMIT 1",
		escapeSQLString(cwd),
	)

	out, err := runSQLiteCommand("-json", db, query)
	if err != nil {
		return "", fmt.Errorf("sqlite3 query failed: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" || result == "[]" {
		return "", nil
	}

	var rows []map[string]string
	if err := json.Unmarshal([]byte(result), &rows); err != nil {
		return "", fmt.Errorf("failed to parse sqlite3 output: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	for _, value := range rows[0] {
		return value, nil
	}
	return "", nil
}

func (a *Agent) ensureCachedTranscript(cwd string, sessionID string) (string, error) {
	db, err := kiroCLIDataDBPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(db); err != nil {
		return "", fmt.Errorf("kiro database not found at %s: %w", db, err)
	}

	query := fmt.Sprintf(
		"SELECT value FROM conversations_v2 WHERE key = '%s' ORDER BY updated_at DESC LIMIT 1",
		escapeSQLString(cwd),
	)

	out, err := runSQLiteCommand(db, query)
	if err != nil {
		return "", fmt.Errorf("sqlite3 transcript query failed: %w", err)
	}

	transcript := strings.TrimSpace(string(out))
	if transcript == "" {
		return "", errors.New("no transcript found")
	}

	cachePath, err := a.cacheTranscriptPath(cwd, sessionID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(cachePath, []byte(transcript), 0o600); err != nil {
		return "", fmt.Errorf("failed to write cached transcript: %w", err)
	}
	return cachePath, nil
}

func (a *Agent) ensureIDETranscript(cwd string, sessionID string) (string, error) {
	sessionsDir, err := ideWorkspaceSessionsDir(cwd)
	if err != nil {
		return "", err
	}

	indexPath := filepath.Join(sessionsDir, "sessions.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return "", fmt.Errorf("IDE sessions.json not found: %w", err)
	}

	var sessions []ideSessionIndexEntry
	if err := json.Unmarshal(indexData, &sessions); err != nil {
		return "", fmt.Errorf("failed to parse IDE sessions.json: %w", err)
	}
	if len(sessions) == 0 {
		return "", errors.New("no IDE sessions found")
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].DateCreated > sessions[j].DateCreated
	})

	transcriptPath := filepath.Join(sessionsDir, sessions[0].SessionID+".json")
	transcriptData, err := os.ReadFile(transcriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read IDE transcript %s: %w", transcriptPath, err)
	}

	cachePath, err := a.cacheTranscriptPath(cwd, sessionID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(cachePath, transcriptData, 0o600); err != nil {
		return "", fmt.Errorf("failed to write cached IDE transcript: %w", err)
	}
	return cachePath, nil
}

func (a *Agent) captureTranscriptForStop(cwd string, sessionID string) string {
	if sessionRef, err := a.ensureCachedTranscript(cwd, sessionID); err == nil && sessionRef != "" {
		return sessionRef
	}
	if sessionRef, err := a.ensureIDETranscript(cwd, sessionID); err == nil && sessionRef != "" {
		return sessionRef
	}
	return a.createPlaceholderTranscript(cwd, sessionID)
}

func (a *Agent) createPlaceholderTranscript(cwd string, sessionID string) string {
	cachePath, err := a.cacheTranscriptPath(cwd, sessionID)
	if err != nil {
		return ""
	}
	if err := os.WriteFile(cachePath, []byte("{}"), 0o600); err != nil {
		return ""
	}
	return cachePath
}

func (a *Agent) cacheTranscriptPath(cwd string, sessionID string) (string, error) {
	repoRoot := protocol.RepoRoot()
	if repoRoot == "" {
		repoRoot = cwd
	}
	sessionDir, err := a.GetSessionDir(repoRoot)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}
	return a.ResolveSessionFile(sessionDir, sessionID), nil
}

func kiroCLIDataDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "kiro-cli", "data.sqlite3"), nil
	default:
		return filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3"), nil
	}
}

func ideWorkspaceSessionsDir(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(cwd))
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent", "workspace-sessions", encoded), nil
	default:
		return filepath.Join(home, ".config", "Kiro", "User", "globalStorage", "kiro.kiroagent", "workspace-sessions", encoded), nil
	}
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return len(data), nil
}

func (a *Agent) ExtractModifiedFiles(_ string, offset int) ([]string, int, error) {
	return []string{}, offset, nil
}

func (a *Agent) ExtractPrompts(_ string, _ int) ([]string, error) {
	return []string{}, nil
}

func (a *Agent) ExtractSummary(_ string) (string, bool, error) {
	return "", false, nil
}
