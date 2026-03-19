package kiro

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	prodTrustedCommand  = "sh -c 'entire hooks *"
	localDevCommandBase = "go run ${KIRO_PROJECT_DIR}/cmd/entire/main.go hooks kiro "
	localDevTrustedCmd  = "sh -c 'go run ${KIRO_PROJECT_DIR}/cmd/entire/main.go hooks *"
	prodHookCommandBase = "entire hooks kiro "
	sessionIDFile       = "kiro-active-session"
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

	switch hookName {
	case HookNameAgentSpawn:
		sessionID := a.generateAndCacheSessionID()
		return &protocol.EventJSON{
			Type:      1,
			SessionID: sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil
	case HookNameUserPromptSubmit:
		sessionID := a.readCachedSessionID()
		if sessionID == "" {
			sessionID = a.generateAndCacheSessionID()
		}
		prompt := raw.Prompt
		if prompt == "" {
			prompt = os.Getenv("USER_PROMPT")
		}
		return &protocol.EventJSON{
			Type:      2,
			SessionID: sessionID,
			Prompt:    prompt,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil
	case HookNamePreToolUse, HookNamePostToolUse:
		return nil, nil
	case HookNameStop:
		cwd := raw.CWD
		if cwd == "" {
			cwd = protocol.RepoRoot()
		}
		sessionID := a.readCachedSessionID()
		if sessionID == "" {
			nativeSessionID, err := a.querySessionID(cwd)
			if err == nil && nativeSessionID != "" {
				sessionID = nativeSessionID
			} else {
				sessionID = fallbackStopSessionID()
			}
		}
		sessionRef := a.captureTranscriptForStop(cwd, sessionID)
		a.clearCachedSessionID()
		return &protocol.EventJSON{
			Type:       3,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
		}, nil
	default:
		return nil, nil
	}
}

func (a *Agent) InstallHooks(localDev bool, force bool) (int, error) {
	repoRoot := protocol.RepoRoot()
	if !force && allHooksInstalled(repoRoot, localDev) && trustedCommandsPresent(repoRoot, localDev) {
		return 0, nil
	}

	if err := writeCLIHooks(repoRoot, localDev); err != nil {
		return 0, err
	}
	if err := writeIDEHooks(repoRoot, localDev); err != nil {
		return 0, err
	}
	if err := installTrustedCommands(repoRoot, localDev); err != nil {
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

func writeCLIHooks(repoRoot string, localDev bool) error {
	hooksPath := filepath.Join(repoRoot, ".kiro", hooksDir, hooksFileName)
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o750); err != nil {
		return err
	}
	commandBase := hookCommandBase(localDev)

	file := kiroAgentFile{
		Name: "entire",
		Tools: []string{
			"read", "write", "shell", "grep", "glob",
			"aws", "report", "introspect", "knowledge",
			"thinking", "todo", "delegate",
		},
		Hooks: kiroHooks{
			AgentSpawn:       []kiroHookEntry{{Command: commandBase + HookNameAgentSpawn}},
			UserPromptSubmit: []kiroHookEntry{{Command: commandBase + HookNameUserPromptSubmit}},
			PreToolUse:       []kiroHookEntry{{Command: commandBase + HookNamePreToolUse}},
			PostToolUse:      []kiroHookEntry{{Command: commandBase + HookNamePostToolUse}},
			Stop:             []kiroHookEntry{{Command: commandBase + HookNameStop}},
		},
	}

	data, err := marshalJSON(file)
	if err != nil {
		return err
	}
	return os.WriteFile(hooksPath, data, 0o600)
}

func writeIDEHooks(repoRoot string, localDev bool) error {
	dir := filepath.Join(repoRoot, ".kiro", ideHooksDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	commandBase := hookCommandBase(localDev)

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
				Command: shellWrappedCommand(commandBase + def.CLIVerb),
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

func allHooksInstalled(repoRoot string, localDev bool) bool {
	cliPath := filepath.Join(repoRoot, ".kiro", hooksDir, hooksFileName)
	commandBase := hookCommandBase(localDev)
	if data, err := os.ReadFile(cliPath); err == nil {
		var file kiroAgentFile
		if json.Unmarshal(data, &file) == nil &&
			hookCommandExists(file.Hooks.AgentSpawn, commandBase+HookNameAgentSpawn) &&
			hookCommandExists(file.Hooks.UserPromptSubmit, commandBase+HookNameUserPromptSubmit) &&
			hookCommandExists(file.Hooks.PreToolUse, commandBase+HookNamePreToolUse) &&
			hookCommandExists(file.Hooks.PostToolUse, commandBase+HookNamePostToolUse) &&
			hookCommandExists(file.Hooks.Stop, commandBase+HookNameStop) &&
			allIDEHooksPresent(repoRoot, localDev) {
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

func allIDEHooksPresent(repoRoot string, localDev bool) bool {
	commandBase := hookCommandBase(localDev)
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
		if hook.Then.Command != shellWrappedCommand(commandBase+def.CLIVerb) {
			return false
		}
	}
	return true
}

func trustedCommandsPresent(repoRoot string, localDev bool) bool {
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
	want := trustedCommand(localDev)
	for _, command := range commands {
		if command == want {
			return true
		}
	}
	return false
}

func installTrustedCommands(repoRoot string, localDev bool) error {
	settingsPath := filepath.Join(repoRoot, vscodeSettingsDir, vscodeSettingsFile)

	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}
	commands, err := readTrustedCommands(settings)
	if err != nil {
		return err
	}
	want := trustedCommand(localDev)
	for _, command := range commands {
		if command == want {
			return nil
		}
	}
	commands = append(commands, want)
	raw, err := json.Marshal(commands)
	if err != nil {
		return err
	}
	settings[trustedCommandsKey] = raw
	return writeSettings(settingsPath, settings)
}

func uninstallTrustedCommands(repoRoot string) error {
	settingsPath := filepath.Join(repoRoot, vscodeSettingsDir, vscodeSettingsFile)
	if _, err := os.Stat(settingsPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}
	commands, err := readTrustedCommands(settings)
	if err != nil {
		return err
	}
	filtered := commands[:0]
	for _, command := range commands {
		if command != prodTrustedCommand && command != localDevTrustedCmd {
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

func hookCommandBase(localDev bool) string {
	if localDev {
		return localDevCommandBase
	}
	return prodHookCommandBase
}

// shellWrappedCommand wraps a hook command in "sh -c" with a /dev/null stdin
// redirect. IDEs typically run hook commands directly without a shell, so a
// bare "</dev/null" suffix is passed as a literal argument instead of being
// interpreted as a redirect. Wrapping in sh ensures the redirect works.
// The command content is built from compile-time constants (not user input).
func shellWrappedCommand(cmd string) string {
	return "sh -c '" + cmd + " </dev/null'"
}

func trustedCommand(localDev bool) string {
	if localDev {
		return localDevTrustedCmd
	}
	return prodTrustedCommand
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
	return strings.HasPrefix(hook.Name, "entire-") &&
		(strings.HasPrefix(hook.Then.Command, prodHookCommandBase) ||
			strings.HasPrefix(hook.Then.Command, localDevCommandBase))
}

func (a *Agent) generateAndCacheSessionID() string {
	sessionID := generateSessionID()
	cachePath := a.sessionIDCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err == nil {
		_ = os.WriteFile(cachePath, []byte(sessionID), 0o600)
	}
	return sessionID
}

func (a *Agent) readCachedSessionID() string {
	data, err := os.ReadFile(a.sessionIDCachePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (a *Agent) clearCachedSessionID() {
	_ = os.Remove(a.sessionIDCachePath())
}

func (a *Agent) sessionIDCachePath() string {
	return filepath.Join(protocol.RepoRoot(), ".entire", "tmp", sessionIDFile)
}

func fallbackStopSessionID() string {
	return generateSessionID()
}

func generateSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("kiro-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
