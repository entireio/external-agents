// Package goose implements the Entire external agent protocol for Goose,
// Block's open-source AI coding agent. See AGENT.md for the protocol mapping
// and verified payload formats.
package goose

import (
	"os/exec"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// Hook names registered with the Entire CLI. These are the verbs Entire uses
// when routing hook payloads back to parse-hook, and the verbs install-hooks
// wires into the goose plugin's hooks.json commands.
const (
	HookNameSessionStart     = "session-start"
	HookNameUserPromptSubmit = "user-prompt-submit"
	HookNameStop             = "stop"
	HookNameSessionEnd       = "session-end"
)

type Agent struct{}

func New() *Agent {
	return &Agent{}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "goose",
		Type:            "Goose",
		Description:     "Goose - Block's open-source AI coding agent",
		IsPreview:       true,
		ProtectedDirs:   []string{".agents"},
		HookNames: []string{
			HookNameSessionStart,
			HookNameUserPromptSubmit,
			HookNameStop,
			HookNameSessionEnd,
		},
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			TranscriptPreparer: true,
			TokenCalculator:    true,
		},
	}
}

func (a *Agent) Detect() protocol.DetectResponse {
	_, err := exec.LookPath("goose")
	return protocol.DetectResponse{Present: err == nil}
}

// GetSessionID extracts the goose session ID from a hook input. Every goose
// hook payload carries session_id (format YYYYMMDD_N), which Entire passes
// through on the HookInput.
func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	return input.SessionID
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	return "goose session --resume --session-id " + sessionID
}
