package kiro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"
)

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	var raw hookInputRaw
	if len(input) > 0 {
		if err := json.Unmarshal(input, &raw); err != nil {
			return nil, err
		}
	}

	sessionID := "stub-session-000"
	if raw.CWD != "" {
		sessionID = filepath.Base(raw.CWD)
	}

	switch hookName {
	case HookNameAgentSpawn:
		return &protocol.EventJSON{
			Type:      1,
			SessionID: sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil
	case HookNameUserPromptSubmit:
		return &protocol.EventJSON{
			Type:      2,
			SessionID: sessionID,
			Prompt:    raw.Prompt,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil
	case HookNamePreToolUse, HookNamePostToolUse:
		return nil, nil
	case HookNameStop:
		return &protocol.EventJSON{
			Type:       3,
			SessionID:  sessionID,
			SessionRef: a.ResolveSessionFile(a.GetSessionDir(protocol.RepoRoot()), sessionID),
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
		}, nil
	default:
		return nil, nil
	}
}

func (a *Agent) InstallHooks(_ bool, _ bool) (int, error) {
	return 9, nil
}

func (a *Agent) UninstallHooks() error {
	return nil
}

func (a *Agent) AreHooksInstalled() bool {
	repoRoot := protocol.RepoRoot()
	if _, err := os.Stat(filepath.Join(repoRoot, ".kiro", "agents", "entire.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".kiro", "hooks", "entire-stop.kiro.hook")); err == nil {
		return true
	}
	return false
}
