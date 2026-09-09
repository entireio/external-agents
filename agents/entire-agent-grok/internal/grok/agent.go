package grok

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-grok/internal/protocol"
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
		Description:     "Grok Build external agent integration for Entire",
		IsPreview:       true,
		ProtectedDirs:   []string{".grok"},
		HookNames: []string{
			HookNameSessionStart,
			HookNameUserPromptSubmit,
			HookNamePreToolUse,
			HookNameStop,
			HookNameStopCancelled,
			HookNameStopFailure,
			HookNameSessionEnd,
			HookNamePreCompact,
			HookNamePostCompact,
			HookNamePostToolUse,
			HookNamePostToolUseFailure,
			HookNameNotification,
			HookNamePermissionRequest,
			HookNameSubagentStart,
			HookNameSubagentStop,
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
	_, err := lookPath(grokBinary)
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && strings.TrimSpace(input.SessionID) != "" {
		return input.SessionID
	}
	return stubSessionID
}

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	if strings.TrimSpace(repoPath) == "" {
		repoPath = protocol.RepoRoot()
	}
	return nativeSessionDir(repoPath), nil
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = stubSessionID
	}
	return filepath.Join(sessionDir, sessionID, nativeTranscriptFile)
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	// "grok --continue" resumes whichever session Grok saw most recently, which
	// is not necessarily this one: a stale session in the same directory wins
	// silently and the agent answers from the wrong history. Always name the id.
	if strings.TrimSpace(sessionID) == "" {
		return "grok --continue"
	}
	return "grok --resume " + shellQuote(sessionID)
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

var errEmptySessionData = errors.New("refusing to write an empty transcript over the Grok session")
