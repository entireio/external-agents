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

type Agent struct {
	CommandRunner CommandRunner
}

func New() *Agent {
	return &Agent{CommandRunner: &DefaultCommandRunner{}}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "goose",
		Type:            "Goose",
		Description:     "Goose - Block's open-source AI coding agent",
		IsPreview:       true,
		ProtectedDirs:   []string{".agents"},
		ProtectedFiles:  []string{".agents/plugins/entire/hooks/hooks.json"},
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
			CompactTranscript:  true,
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
	if input == nil {
		return ""
	}
	return input.SessionID
}

func (a *Agent) FormatResumeCommand(sessionID string) string {
	// Goose sessions are named YYYYMMDD_N, but the session ID arrives from a
	// hook payload and is never validated before formatting. The formatted
	// string is printed to the user's terminal by the Entire CLI and run
	// verbatim, so a hostile session ID (for example one containing shell
	// metacharacters) would execute arbitrary commands when the user runs
	// the printed command. Refuse to emit the command for IDs outside the
	// expected character set, matching the allowlist used by the kilo and
	// qwen adapters.
	if !isValidResumeSessionID(sessionID) {
		return ""
	}
	return "goose session --resume --session-id " + sessionID
}

// isValidResumeSessionID reports whether sessionID contains only the
// characters a normal goose session name may carry (YYYYMMDD_N style).
// Anything else (shell metacharacters, control characters, path separators)
// makes the resume command unsafe and the formatter refuses to emit it.
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
