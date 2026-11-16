package gemini

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-gemini/internal/protocol"
)

type Agent struct {
	LookPath func(string) (string, error)
}

func New() *Agent {
	return &Agent{LookPath: exec.LookPath}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            AgentName,
		Type:            AgentType,
		Description:     "Gemini CLI external agent integration for Entire",
		IsPreview:       true,
		ProtectedDirs:   []string{".gemini"},
		HookNames: []string{
			HookNameSessionStart,
			HookNameBeforeAgent,
			HookNameAfterAgent,
			HookNameBeforeTool,
			HookNameAfterTool,
			HookNamePreCompress,
			HookNameNotification,
			HookNameSessionEnd,
		},
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			CompactTranscript:  true,
			UsesTerminal:       true,
		},
	}
}

func (a *Agent) Detect() protocol.DetectResponse {
	lookPath := a.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	_, err := lookPath(geminiBinary)
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && strings.TrimSpace(input.SessionID) != "" {
		return input.SessionID
	}
	// Gemini CLI exposes session ID via env var
	if sid := os.Getenv("GEMINI_SESSION_ID"); sid != "" {
		return sid
	}
	return stubSessionID
}

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	if strings.TrimSpace(repoPath) == "" {
		repoPath = protocol.RepoRoot()
	}
	if resolved, err := filepath.EvalSymlinks(repoPath); err == nil {
		repoPath = resolved
	}
	sum := sha256.Sum256([]byte(repoPath))
	key := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(os.TempDir(), "entire-gemini", key), nil
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	if strings.TrimSpace(sessionDir) == "" {
		sessionDir, _ = a.GetSessionDir(protocol.RepoRoot())
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = stubSessionID
	}
	return filepath.Join(sessionDir, safeFilename(sessionID)+".jsonl")
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return "gemini --continue"
	}
	return "gemini --resume " + shellQuote(sessionID)
}

func safeFilename(s string) string {
	r := []rune(s)
	for i, c := range r {
		if !isSafeFilenameRune(c) {
			r[i] = '_'
		}
	}
	return string(r)
}

func isSafeFilenameRune(r rune) bool {
	return r == '-' || r == '_' || r == '.' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !isSafeShellQuoteRune(r)
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func isSafeShellQuoteRune(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == ':' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

var errMissingSessionRef = errors.New("session_ref or session_id is required")
