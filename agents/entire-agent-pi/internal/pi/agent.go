package pi

import (
	"os/exec"

	"github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"
)

type Agent struct{}

func New() *Agent {
	return &Agent{}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "pi",
		Type:            "Pi",
		Description:     "Pi coding agent integration for Entire",
		IsPreview:       true,
		ProtectedDirs:   []string{".pi"},
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
	_, err := exec.LookPath("pi")
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && input.SessionID != "" {
		return input.SessionID
	}
	return ""
}

func (a *Agent) FormatResumeCommand(_ string) string {
	return "pi --continue"
}
