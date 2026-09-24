package vibe

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

// Agent implements the Entire CLI external agent protocol for Mistral Vibe.
type Agent struct{}

// New returns a new Vibe agent instance.
func New() *Agent {
	return &Agent{}
}

// Info returns the agent info response with capabilities and hook names.
func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "mistral-vibe",
		Type:            "Mistral Vibe",
		Description:     "Mistral Vibe - External agent plugin for Entire CLI",
		IsPreview:       true,
		ProtectedDirs:   []string{".vibe"},
		HookNames: []string{
			HookNameSessionStart,
			HookNameUserPromptSubmit,
			HookNamePreToolUse,
			HookNamePostToolUse,
			HookNameTurnEnd,
		},
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			TokenCalculator:    true,
		},
	}
}

// Detect checks whether a .vibe/ directory exists in the repo root.
func (a *Agent) Detect() protocol.DetectResponse {
	repoRoot := protocol.RepoRoot()
	_, err := os.Stat(filepath.Join(repoRoot, ".vibe"))
	return protocol.DetectResponse{Present: err == nil}
}

// GetSessionID extracts the session ID from the hook input JSON. If the input
// contains a session_id it is returned directly; otherwise a placeholder is used.
func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && input.SessionID != "" {
		return input.SessionID
	}
	return "stub-session-000"
}

// GetSessionDir returns the session directory for the given repo path.
func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return protocol.DefaultSessionDir(repoPath), nil
}

// ResolveSessionFile returns the full path to a session file.
func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return protocol.ResolveSessionFile(sessionDir, sessionID)
}

// ReadSession reads a session from disk or returns a new session shell.
func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	sessionID := a.GetSessionID(input)
	repoRoot := protocol.RepoRoot()
	sessionDir, err := a.GetSessionDir(repoRoot)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	sessionRef := a.ResolveSessionFile(sessionDir, sessionID)
	if input != nil && input.SessionRef != "" {
		sessionRef = input.SessionRef
	}

	nativeData, err := os.ReadFile(sessionRef)
	if err != nil && !os.IsNotExist(err) {
		return protocol.AgentSessionJSON{}, fmt.Errorf("failed to read session file: %w", err)
	}

	return protocol.AgentSessionJSON{
		SessionID:     sessionID,
		AgentName:     "mistral-vibe",
		RepoPath:      repoRoot,
		SessionRef:    sessionRef,
		StartTime:     time.Now().UTC().Format(time.RFC3339),
		NativeData:    nativeData,
		ModifiedFiles: []string{},
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

// WriteSession writes the session JSON to the session file on disk.
func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if session.SessionRef == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(session.SessionRef), 0o700); err != nil {
		return err
	}
	return os.WriteFile(session.SessionRef, session.NativeData, 0o600)
}

// ReadTranscript reads raw transcript bytes from the given session ref path.
func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
}

// ChunkTranscript splits content into byte chunks of at most maxSize bytes.
func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, errors.New("max-size must be greater than zero")
	}
	if len(content) == 0 {
		return [][]byte{[]byte{}}, nil
	}

	var chunks [][]byte
	for start := 0; start < len(content); start += maxSize {
		end := start + maxSize
		if end > len(content) {
			end = len(content)
		}
		chunk := make([]byte, end-start)
		copy(chunk, content[start:end])
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// ReassembleTranscript joins previously-chunked transcript pieces back together.
func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, nil), nil
}

// FormatResumeCommand returns the CLI command to resume a Vibe session.
func (a *Agent) FormatResumeCommand(sessionID string) string {
	return fmt.Sprintf("vibe --resume %s", sessionID)
}
