package amp

import (
	"os/exec"

	"github.com/entireio/external-agents/agents/entire-agent-amp/internal/protocol"
)

type Agent struct {
	CommandRunner CommandRunner
}

func New() *Agent {
	return &Agent{CommandRunner: &DefaultCommandRunner{}}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "amp",
		Type:            "Amp",
		Description:     "Amp coding agent integration for Entire",
		IsPreview:       true,
		ProtectedDirs:   []string{".amp"},
		ProtectedFiles:  []string{".amp/plugins/entire.ts"},
		HookNames:       []string{"session.start", "agent.start", "agent.end"},
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
	_, err := exec.LookPath("amp")
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && input.SessionID != "" {
		return input.SessionID
	}
	return ""
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	if sessionID == "" {
		return "PLUGINS=all amp threads continue --last"
	}
	return "PLUGINS=all amp threads continue " + sessionID
}
