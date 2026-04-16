package omp

import (
	"os/exec"

	"github.com/entireio/external-agents/agents/entire-agent-omp/internal/protocol"
)

type Agent struct{}

func New() *Agent {
	return &Agent{}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "omp",
		Type:            "Oh My Pi",
		Description:     "Oh My Pi coding agent integration for Entire",
		IsPreview:       true,
		ProtectedDirs:   []string{".omp"},
		HookNames:       []string{"session_start", "before_agent_start", "agent_end", "session_shutdown"},
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			TokenCalculator:    true,
			UsesTerminal:       true,
		},
	}
}

func (a *Agent) Detect() protocol.DetectResponse {
	_, err := exec.LookPath("omp")
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && input.SessionID != "" {
		return input.SessionID
	}
	return ""
}

func (a *Agent) FormatResumeCommand(_ string) string {
	return "omp --continue"
}
