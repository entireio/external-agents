package kiro

import "github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"

func (a *Agent) GetSessionDir(repoPath string) string {
	return protocol.DefaultSessionDir(repoPath)
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return protocol.ResolveSessionFile(sessionDir, sessionID)
}
