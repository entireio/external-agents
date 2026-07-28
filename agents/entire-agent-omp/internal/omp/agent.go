package omp

import (
	"os/exec"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

type Agent struct{}

func New() *Agent { return &Agent{} }

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "omp",
		Type:            "Oh My Pi",
		Description:     "Oh My Pi terminal coding agent",
		IsPreview:       true,
		ProtectedDirs:   []string{".omp"},
		ProtectedFiles:  []string{extensionRelFile},
		HookNames: []string{
			HookNameSessionStart,
			HookNameAgentStart,
			HookNameAgentEnd,
			HookNameSessionShutdown,
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
	_, err := exec.LookPath("omp")
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input == nil {
		return ""
	}
	return input.SessionID
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	if sessionID == "" {
		return "omp --continue"
	}
	return "omp --resume " + shellQuote(sessionID)
}

func shellQuote(value string) string {
	if strings.IndexFunc(value, func(r rune) bool { return !safeShellRune(r) }) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func safeShellRune(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == ':' ||
		(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
