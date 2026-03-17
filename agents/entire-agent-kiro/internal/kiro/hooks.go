package kiro

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-kiro/internal/protocol"
)

const (
	hooksFileName       = "entire.json"
	hooksDir            = "agents"
	ideHooksDir         = "hooks"
	ideHookFileSuffix   = ".kiro.hook"
	ideHookVersion      = "1"
	vscodeSettingsDir   = ".vscode"
	vscodeSettingsFile  = "settings.json"
	trustedCommandsKey  = "kiroAgent.trustedCommands"
	prodTrustedCommand  = "entire hooks *"
	prodHookCommandBase = "entire hooks kiro "
)

type ideHookDef struct {
	Filename    string
	TriggerType string
	CLIVerb     string
}

var ideHookDefs = []ideHookDef{
	{Filename: "entire-prompt-submit", TriggerType: "promptSubmit", CLIVerb: HookNameUserPromptSubmit},
	{Filename: "entire-stop", TriggerType: "agentStop", CLIVerb: HookNameStop},
	{Filename: "entire-pre-tool-use", TriggerType: "preToolUse", CLIVerb: HookNamePreToolUse},
	{Filename: "entire-post-tool-use", TriggerType: "postToolUse", CLIVerb: HookNamePostToolUse},
}

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

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	repoRoot := protocol.RepoRoot()
	if !force && allHooksInstalled(repoRoot) && trustedCommandsPresent(repoRoot) {
		return 0, nil
	}

	if err := writeCLIHooks(repoRoot); err != nil {
		return 0, err
	}
	if err := writeIDEHooks(repoRoot); err != nil {
		return 0, err
	}
	if err := installTrustedCommands(repoRoot); err != nil {
		return 0, err
	}

	return len(defaultHookNames()) + len(ideHookDefs), nil
}

func (a *Agent) UninstallHooks() error {
	repoRoot := protocol.RepoRoot()
	cliPath := filepath.Join(repoRoot, ".kiro", hooksDir, hooksFileName)
	if err := os.Remove(cliPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, def := range ideHookDefs {
		path := filepath.Join(repoRoot, ".kiro", ideHooksDir, def.Filename+ideHookFileSuffix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := uninstallTrustedCommands(repoRoot); err != nil {
		return err
	}
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

func defaultHookNames() []string {
	return []string{
		HookNameAgentSpawn,
		HookNameUserPromptSubmit,
		HookNamePreToolUse,
		HookNamePostToolUse,
		HookNameStop,
	}
}

func writeCLIHooks(repoRoot string) error {
	hooksPath := filepath.Join(repoRoot, ".kiro", hooksDir, hooksFileName)
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o750); err != nil {
		return err
	}

	file := kiroAgentFile{
		Name: "entire",
		Tools: []string{
			"read", "write", "shell", "grep", "glob",
			"aws", "report", "introspect", "knowledge",
			"thinking", "todo", "delegate",
		},
		Hooks: kiroHooks{
			AgentSpawn:       []kiroHookEntry{{Command: prodHookCommandBase + HookNameAgentSpawn}},
			UserPromptSubmit: []kiroHookEntry{{Command: prodHookCommandBase + HookNameUserPromptSubmit}},
			PreToolUse:       []kiroHookEntry{{Command: prodHookCommandBase + HookNamePreToolUse}},
			PostToolUse:      []kiroHookEntry{{Command: prodHookCommandBase + HookNamePostToolUse}},
			Stop:             []kiroHookEntry{{Command: prodHookCommandBase + HookNameStop}},
		},
	}

	data, err := marshalJSON(file)
	if err != nil {
		return err
	}
	return os.WriteFile(hooksPath, data, 0o600)
}

func writeIDEHooks(repoRoot string) error {
	dir := filepath.Join(repoRoot, ".kiro", ideHooksDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	for _, def := range ideHookDefs {
		hook := kiroIDEHookFile{
			Enabled:     true,
			Name:        def.Filename,
			Description: "Entire CLI " + def.TriggerType + " hook",
			Version:     ideHookVersion,
			When: kiroIDEHookWhen{
				Type: def.TriggerType,
			},
			Then: kiroIDEHookThen{
				Type:    "runCommand",
				Command: prodHookCommandBase + def.CLIVerb,
			},
		}
		data, err := marshalJSON(hook)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, def.Filename+ideHookFileSuffix)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
	}

	return nil
}

func allHooksInstalled(repoRoot string) bool {
	cliPath := filepath.Join(repoRoot, ".kiro", hooksDir, hooksFileName)
	if data, err := os.ReadFile(cliPath); err == nil {
		var file kiroAgentFile
		if json.Unmarshal(data, &file) == nil &&
			hookCommandExists(file.Hooks.AgentSpawn, prodHookCommandBase+HookNameAgentSpawn) &&
			hookCommandExists(file.Hooks.UserPromptSubmit, prodHookCommandBase+HookNameUserPromptSubmit) &&
			hookCommandExists(file.Hooks.PreToolUse, prodHookCommandBase+HookNamePreToolUse) &&
			hookCommandExists(file.Hooks.PostToolUse, prodHookCommandBase+HookNamePostToolUse) &&
			hookCommandExists(file.Hooks.Stop, prodHookCommandBase+HookNameStop) &&
			allIDEHooksPresent(repoRoot) {
			return true
		}
	}
	return false
}

func hookCommandExists(entries []kiroHookEntry, command string) bool {
	for _, entry := range entries {
		if entry.Command == command {
			return true
		}
	}
	return false
}

func allIDEHooksPresent(repoRoot string) bool {
	for _, def := range ideHookDefs {
		path := filepath.Join(repoRoot, ".kiro", ideHooksDir, def.Filename+ideHookFileSuffix)
		data, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		var hook kiroIDEHookFile
		if err := json.Unmarshal(data, &hook); err != nil {
			return false
		}
		if hook.Then.Command != prodHookCommandBase+def.CLIVerb {
			return false
		}
	}
	return true
}

func trustedCommandsPresent(repoRoot string) bool {
	settingsPath := filepath.Join(repoRoot, vscodeSettingsDir, vscodeSettingsFile)
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return false
	}
	raw, ok := settings[trustedCommandsKey]
	if !ok {
		return false
	}
	var commands []string
	if err := json.Unmarshal(raw, &commands); err != nil {
		return false
	}
	for _, command := range commands {
		if command == prodTrustedCommand {
			return true
		}
	}
	return false
}

func installTrustedCommands(repoRoot string) error {
	settingsPath := filepath.Join(repoRoot, vscodeSettingsDir, vscodeSettingsFile)

	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}
	commands, err := readTrustedCommands(settings)
	if err != nil {
		return err
	}
	for _, command := range commands {
		if command == prodTrustedCommand {
			return writeSettings(settingsPath, settings)
		}
	}
	commands = append(commands, prodTrustedCommand)
	raw, err := json.Marshal(commands)
	if err != nil {
		return err
	}
	settings[trustedCommandsKey] = raw
	return writeSettings(settingsPath, settings)
}

func uninstallTrustedCommands(repoRoot string) error {
	settingsPath := filepath.Join(repoRoot, vscodeSettingsDir, vscodeSettingsFile)
	settings, err := readSettings(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	commands, err := readTrustedCommands(settings)
	if err != nil {
		return err
	}
	filtered := commands[:0]
	for _, command := range commands {
		if command != prodTrustedCommand {
			filtered = append(filtered, command)
		}
	}
	if len(filtered) == 0 {
		delete(settings, trustedCommandsKey)
	} else {
		raw, err := json.Marshal(filtered)
		if err != nil {
			return err
		}
		settings[trustedCommandsKey] = raw
	}
	return writeSettings(settingsPath, settings)
}

func readSettings(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]json.RawMessage{}, nil
		}
		return nil, err
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	if settings == nil {
		settings = map[string]json.RawMessage{}
	}
	return settings, nil
}

func readTrustedCommands(settings map[string]json.RawMessage) ([]string, error) {
	raw, ok := settings[trustedCommandsKey]
	if !ok {
		return []string{}, nil
	}
	var commands []string
	if err := json.Unmarshal(raw, &commands); err != nil {
		return nil, err
	}
	return commands, nil
}

func writeSettings(path string, settings map[string]json.RawMessage) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := marshalJSON(settings)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func marshalJSON(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func isEntireIDEHook(hook kiroIDEHookFile) bool {
	return strings.HasPrefix(hook.Name, "entire-") && strings.HasPrefix(hook.Then.Command, prodHookCommandBase)
}
