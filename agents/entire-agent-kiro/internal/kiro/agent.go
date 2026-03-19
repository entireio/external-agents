package kiro

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"
)

type Agent struct{}

func New() *Agent {
	return &Agent{}
}

func (a *Agent) Info() protocol.InfoResponse {
	return protocol.InfoResponse{
		ProtocolVersion: protocol.ProtocolVersion,
		Name:            "kiro",
		Type:            "Kiro",
		Description:     "Kiro - External agent plugin for Entire CLI",
		IsPreview:       true,
		ProtectedDirs:   []string{".kiro"},
		HookNames: []string{
			HookNameAgentSpawn,
			HookNameUserPromptSubmit,
			HookNamePreToolUse,
			HookNamePostToolUse,
			HookNameStop,
		},
		Capabilities: protocol.DeclaredCapabilities{
			Hooks:              true,
			TranscriptAnalyzer: true,
			UsesTerminal:       true,
		},
	}
}

func (a *Agent) Detect() protocol.DetectResponse {
	repoRoot := protocol.RepoRoot()
	_, err := os.Stat(filepath.Join(repoRoot, ".kiro"))
	return protocol.DetectResponse{Present: err == nil}
}

func (a *Agent) GetSessionID(input *protocol.HookInputJSON) string {
	if input != nil && input.SessionID != "" {
		return input.SessionID
	}
	return "stub-session-000"
}

func (a *Agent) ReadSession(input *protocol.HookInputJSON) (protocol.AgentSessionJSON, error) {
	sessionID := a.GetSessionID(input)
	repoRoot := protocol.RepoRoot()
	sessionDir, err := a.GetSessionDir(repoRoot)
	if err != nil {
		return protocol.AgentSessionJSON{}, err
	}
	sessionRef := ""
	if input != nil && input.SessionRef != "" {
		sessionRef = input.SessionRef
	} else {
		sessionRef = a.ResolveSessionFile(sessionDir, sessionID)
	}

	var nativeData []byte
	if sessionRef != "" {
		data, err := os.ReadFile(sessionRef)
		if err != nil {
			return protocol.AgentSessionJSON{}, fmt.Errorf("failed to read transcript: %w", err)
		}
		nativeData = data
	}

	return protocol.AgentSessionJSON{
		SessionID:     sessionID,
		AgentName:     "kiro",
		RepoPath:      repoRoot,
		SessionRef:    sessionRef,
		StartTime:     time.Now().UTC().Format(time.RFC3339),
		NativeData:    nativeData,
		ModifiedFiles: []string{},
		NewFiles:      []string{},
		DeletedFiles:  []string{},
	}, nil
}

func (a *Agent) WriteSession(session protocol.AgentSessionJSON) error {
	if session.SessionRef == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(session.SessionRef), 0o750); err != nil {
		return err
	}
	return os.WriteFile(session.SessionRef, session.NativeData, 0o600)
}

func (a *Agent) FormatResumeCommand(_ string) string {
	return "kiro-cli chat --resume"
}
