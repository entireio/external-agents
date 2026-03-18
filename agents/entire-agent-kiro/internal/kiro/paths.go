package kiro

import "github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return protocol.DefaultSessionDir(repoPath), nil
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return protocol.ResolveSessionFile(sessionDir, sessionID)
}
