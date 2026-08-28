package grok

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
)

const nativeTranscriptFile = "chat_history.jsonl"

func grokHome() string {
	if home := strings.TrimSpace(os.Getenv("GROK_HOME")); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".grok")
}

func encodeRepoCWD(repoPath string) string {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		repoPath = protocol.RepoRoot()
	}
	if resolved, err := filepath.EvalSymlinks(repoPath); err == nil {
		repoPath = resolved
	}
	repoPath = filepath.Clean(repoPath)
	return strings.ReplaceAll(repoPath, "/", "%2F")
}

func nativeSessionDir(repoPath string) string {
	return filepath.Join(grokHome(), "sessions", encodeRepoCWD(repoPath))
}

func nativeTranscriptPath(repoPath, sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = stubSessionID
	}
	return filepath.Join(nativeSessionDir(repoPath), sessionID, nativeTranscriptFile)
}

func (a *Agent) resolveSessionRef(sessionID, repoPath string) string {
	if strings.TrimSpace(repoPath) == "" {
		repoPath = protocol.RepoRoot()
	}
	return nativeTranscriptPath(repoPath, sessionID)
}
