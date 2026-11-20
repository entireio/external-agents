package vibe

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

const agentName = "mistral-vibe"

type Agent struct {
	// Fetcher reads a session from Vibe. Tests substitute their own.
	Fetcher SessionFetcher
}

func New() *Agent {
	return &Agent{Fetcher: &AppServerFetcher{}}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            agentName,
		Type:            "Mistral Vibe",
		Description:     "Mistral Vibe coding agent integration for Entire",
		IsPreview:       true,
		ProtectedDirs:   []string{".vibe"},
		HookNames: []string{
			HookNameSessionStart,
			HookNamePreAgent,
			HookNameNotification,
			HookNameSessionEnd,
		},
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			TranscriptPreparer: true,
			TokenCalculator:    true,
			CompactTranscript:  true,
			UsesTerminal:       true,
		},
	}
}

func (a *Agent) Detect() protocol.DetectResponse {
	if _, err := exec.LookPath("vibe"); err == nil {
		return protocol.DetectResponse{Present: true}
	}
	info, err := os.Stat(filepath.Join(protocol.RepoRoot(), ".vibe"))
	return protocol.DetectResponse{Present: err == nil && info.IsDir()}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil {
		return input.SessionID
	}
	return ""
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	if sessionID == "" {
		return "vibe --continue"
	}
	return "vibe --resume " + shellQuote(sessionID)
}

// shellQuote returns value unchanged when it only contains characters safe to
// pass through a shell verbatim; otherwise it single-quotes it. The resume
// command is a display/exec string, so a hostile session id must not be able to
// inject shell syntax.
func shellQuote(value string) string {
	for _, r := range value {
		if !isSafeResumeRune(r) {
			return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
		}
	}
	return value
}

func isSafeResumeRune(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == ':' || r == '/' ||
		(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
