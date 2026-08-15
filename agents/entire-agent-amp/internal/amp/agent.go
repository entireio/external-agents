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
	// Reject session IDs that cannot be passed to the shell safely. The
	// formatted string is printed to the user's terminal by the Entire CLI
	// and run verbatim, so a hostile session ID (for example one containing
	// shell metacharacters) would execute arbitrary commands when the user
	// runs the printed command. Validation mirrors the allowlist used by
	// the kilo and qwen adapters.
	if !isValidResumeSessionID(sessionID) {
		return ""
	}
	return "PLUGINS=all amp threads continue " + sessionID
}

// isValidResumeSessionID reports whether sessionID contains only the
// characters a normal amp thread/session identifier may carry. Anything
// else (shell metacharacters, control characters, path separators) makes
// the resume command unsafe and the formatter refuses to emit it.
func isValidResumeSessionID(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	for _, r := range sessionID {
		if !isSafeResumeRune(r) {
			return false
		}
	}
	return true
}

func isSafeResumeRune(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == ':' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}
