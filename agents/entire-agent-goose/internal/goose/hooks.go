package goose

import (
	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// ParseHook converts a raw goose hook payload (HookContext JSON on stdin)
// into a normalized Entire event. Implemented in Phase 3.
func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	return nil, nil
}

// InstallHooks writes the Entire plugin into the repo's project-scope goose
// plugin directory (.agents/plugins/entire/hooks/hooks.json). Implemented in
// Phase 3.
func (a *Agent) InstallHooks(localDev bool, force bool) (int, error) {
	return 0, nil
}

func (a *Agent) UninstallHooks() error {
	return nil
}

func (a *Agent) AreHooksInstalled() bool {
	return false
}
