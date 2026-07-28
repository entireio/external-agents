// Package devin implements the Entire external agent protocol for Devin CLI
// ("Devin for Terminal", Cognition). Devin uses Claude Code-format lifecycle
// hooks installed in .devin/hooks.v1.json and stores its canonical transcript
// as an ATIF JSON document keyed by session ID. See AGENT.md for the verified
// behavior this integration is built on.
package devin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-devin/internal/protocol"
)

// AgentName is the Entire registry name (derived from entire-agent-devin).
const AgentName = "devin"

// AgentType is the display name stored in checkpoint metadata.
const AgentType = "Devin"

// Agent implements the external agent protocol for Devin CLI.
type Agent struct{}

// New creates a new Devin agent instance.
func New() *Agent {
	return &Agent{}
}

// Info returns agent metadata and declared capabilities.
func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            AgentName,
		Type:            AgentType,
		Description:     "Devin CLI - Cognition's terminal coding agent",
		IsPreview:       true,
		ProtectedDirs:   []string{".devin"},
		HookNames: []string{
			HookNameSessionStart,
			HookNameSessionEnd,
			HookNameStop,
			HookNameUserPromptSubmit,
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

// Detect reports whether Devin CLI is usable in the current environment.
func (a *Agent) Detect() protocol.DetectResponse {
	if _, err := exec.LookPath("devin"); err == nil {
		return protocol.DetectResponse{Present: true}
	}
	if _, err := os.Stat(filepath.Join(protocol.RepoRoot(), ".devin")); err == nil {
		return protocol.DetectResponse{Present: true}
	}
	return protocol.DetectResponse{Present: false}
}

// GetSessionID extracts the session ID from hook input.
func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input == nil {
		return ""
	}
	return input.SessionID
}

// FormatResumeCommand returns the command to resume a Devin session.
func (a *Agent) FormatResumeCommand(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return "devin -c"
	}
	return "devin -r " + sessionID
}

// ReadSession reads a session from Devin's storage (ATIF transcript file).
func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	if input == nil || input.SessionRef == "" {
		return protocol.AgentSessionJSON{}, errors.New("session reference (transcript path) is required")
	}

	data, err := os.ReadFile(input.SessionRef)
	if err != nil {
		return protocol.AgentSessionJSON{}, fmt.Errorf("failed to read transcript: %w", err)
	}

	// Non-fatal when the file isn't ATIF (e.g. protocol round-trip data):
	// the session is still usable without the file list.
	modifiedFiles, _ := extractModifiedFiles(data)
	if modifiedFiles == nil {
		modifiedFiles = []string{}
	}

	return protocol.AgentSessionJSON{
		SessionID:     input.SessionID,
		AgentName:     AgentName,
		RepoPath:      protocol.RepoRoot(),
		SessionRef:    input.SessionRef,
		NativeData:    data,
		ModifiedFiles: modifiedFiles,
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

// WriteSession writes a session back to Devin's transcript location.
//
// Limitation (see AGENT.md): Devin resumes conversations from its SQLite
// store, not this file, so restoring the transcript preserves it for
// explain/analysis but does not rewrite Devin's own conversation memory.
func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if session.AgentName != "" && session.AgentName != AgentName {
		return fmt.Errorf("session belongs to agent %q, not %q", session.AgentName, AgentName)
	}
	if session.SessionRef == "" {
		return errors.New("session reference (transcript path) is required")
	}
	if len(session.NativeData) == 0 {
		return errors.New("session has no native data to write")
	}

	if err := os.MkdirAll(filepath.Dir(session.SessionRef), 0o750); err != nil {
		return fmt.Errorf("failed to create transcript directory: %w", err)
	}
	if err := os.WriteFile(session.SessionRef, session.NativeData, 0o600); err != nil {
		return fmt.Errorf("failed to write transcript: %w", err)
	}
	return nil
}

// ReadTranscript reads the raw ATIF transcript bytes for a session.
func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	data, err := os.ReadFile(sessionRef)
	if err != nil {
		return nil, fmt.Errorf("failed to read transcript: %w", err)
	}
	return data, nil
}
