package vibe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-mistral-vibe/internal/protocol"
)

const (
	vibeConfigFile        = "config.toml"
	vibeConfigDir         = ".vibe"
	prodHookCommandBase   = "entire hooks mistral-vibe "
	localDevCommandBase   = "go run ./cmd/entire/main.go hooks mistral-vibe "
	hookMarker            = "entire hooks mistral-vibe"
)

// ParseHook parses a Vibe hook JSON payload and maps it to a protocol EventJSON.
// Returns nil for hooks that do not produce protocol events (pre/post tool use).
func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	var payload VibeHookPayload
	if len(input) > 0 {
		if err := json.Unmarshal(input, &payload); err != nil {
			return nil, err
		}
	}

	switch hookName {
	case HookNameSessionStart:
		sessionID := payload.SessionID
		if sessionID == "" {
			sessionID = fmt.Sprintf("vibe-%d", time.Now().UnixNano())
		}
		return &protocol.EventJSON{
			Type:      1, // SessionStart
			SessionID: sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil

	case HookNameUserPromptSubmit:
		sessionID := payload.SessionID
		if sessionID == "" {
			sessionID = fmt.Sprintf("vibe-%d", time.Now().UnixNano())
		}
		return &protocol.EventJSON{
			Type:      2, // TurnStart
			SessionID: sessionID,
			Prompt:    payload.Prompt,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil

	case HookNameTurnEnd:
		sessionID := payload.SessionID
		if sessionID == "" {
			sessionID = fmt.Sprintf("vibe-%d", time.Now().UnixNano())
		}
		// Construct session_ref from the Vibe session log directory.
		sessionRef := ""
		if sessionID != "" {
			home, err := os.UserHomeDir()
			if err == nil {
				sessionRef = filepath.Join(home, ".vibe", "logs", "session",
					fmt.Sprintf("session_%s", sessionID[:min(8, len(sessionID))]),
					"messages.jsonl")
			}
		}
		return &protocol.EventJSON{
			Type:       3, // TurnEnd
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
		}, nil

	case HookNamePreToolUse, HookNamePostToolUse:
		// Pre/post tool use hooks do not produce protocol events.
		return nil, nil

	default:
		return nil, nil
	}
}

// InstallHooks writes Vibe hook configuration entries to .vibe/config.toml
// that point to the Entire CLI hook handler.
func (a *Agent) InstallHooks(localDev bool, force bool) (int, error) {
	repoRoot := protocol.RepoRoot()

	if !force && a.AreHooksInstalled() {
		return 0, nil
	}

	configDir := filepath.Join(repoRoot, vibeConfigDir)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return 0, fmt.Errorf("failed to create .vibe directory: %w", err)
	}

	commandBase := prodHookCommandBase
	if localDev {
		commandBase = localDevCommandBase
	}

	hookEntries := []struct {
		nativeName  string
		protocolName string
	}{
		{VibeNativeSessionStart, HookNameSessionStart},
		{VibeNativeUserPromptSubmit, HookNameUserPromptSubmit},
		{VibeNativePreToolUse, HookNamePreToolUse},
		{VibeNativePostToolUse, HookNamePostToolUse},
		{VibeNativeTurnEnd, HookNameTurnEnd},
	}

	var tomlLines []string
	tomlLines = append(tomlLines, "# Entire CLI hook configuration")
	tomlLines = append(tomlLines, "# Managed by entire-agent-mistral-vibe")
	tomlLines = append(tomlLines, "")

	for _, hook := range hookEntries {
		tomlLines = append(tomlLines, fmt.Sprintf("[[hooks.%s]]", hook.nativeName))
		tomlLines = append(tomlLines, fmt.Sprintf(`command = "%s%s"`, commandBase, hook.protocolName))
		tomlLines = append(tomlLines, "")
	}

	configPath := filepath.Join(configDir, vibeConfigFile)
	content := strings.Join(tomlLines, "\n")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		return 0, fmt.Errorf("failed to write config.toml: %w", err)
	}

	return len(hookEntries), nil
}

// UninstallHooks removes the Entire CLI hook entries from .vibe/config.toml.
func (a *Agent) UninstallHooks() error {
	repoRoot := protocol.RepoRoot()
	configPath := filepath.Join(repoRoot, vibeConfigDir, vibeConfigFile)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Filter out lines containing the hook marker.
	var filteredLines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, hookMarker) {
			continue
		}
		// Also skip comment lines managed by this agent.
		if strings.Contains(line, "Managed by entire-agent-mistral-vibe") {
			continue
		}
		if strings.Contains(line, "Entire CLI hook configuration") {
			continue
		}
		filteredLines = append(filteredLines, line)
	}

	// If only whitespace/empty lines remain, remove the file entirely.
	remaining := strings.TrimSpace(strings.Join(filteredLines, "\n"))
	if remaining == "" || remaining == "[hooks]" {
		return os.Remove(configPath)
	}

	return os.WriteFile(configPath, []byte(strings.Join(filteredLines, "\n")), 0o600)
}

// AreHooksInstalled checks whether .vibe/config.toml contains Entire CLI hook entries.
func (a *Agent) AreHooksInstalled() bool {
	repoRoot := protocol.RepoRoot()
	configPath := filepath.Join(repoRoot, vibeConfigDir, vibeConfigFile)

	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(data), hookMarker)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
