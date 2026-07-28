package kilo

import (
	"os/exec"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-kilo/internal/protocol"
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
		Name:            "kilo",
		Type:            "Kilo",
		Description:     "Kilo Code coding agent integration for Entire",
		IsPreview:       true,
		ProtectedDirs:   []string{".kilo"},
		ProtectedFiles:  []string{".kilo/plugins/entire.ts"},
		HookNames: []string{
			HookNameSessionStart,
			HookNameTurnStart,
			HookNameTurnEnd,
			HookNameCompaction,
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
	_, err := exec.LookPath("kilo")
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
		return "kilo run --continue"
	}
	return "kilo run --session " + shellQuote(sessionID)
}

// shellQuote returns value unchanged when it only contains characters safe to
// pass through a shell verbatim; otherwise it single-quotes it. The resume
// command is a display/exec string, so an adversarial session id must not be
// able to inject shell syntax.
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
