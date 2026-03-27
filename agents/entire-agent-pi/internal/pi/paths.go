package pi

import "github.com/entireio/external-agents/agents/entire-agent-pi/internal/protocol"

func (a *Agent) GetSessionDir(repoPath string) (string, error) {
	return protocol.DefaultSessionDir(repoPath), nil
}

func (a *Agent) ResolveSessionFile(sessionDir, sessionID string) string {
	return protocol.ResolveSessionFile(sessionDir, sessionID)
}
