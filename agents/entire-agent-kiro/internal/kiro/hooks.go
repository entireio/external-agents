package kiro

import (
	"bytes"
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
	hooksFileName      = "entire.json"
	hooksDir           = "agents"
	ideHooksDir        = "hooks"
	ideHookFileSuffix  = ".kiro.hook"
	ideHookVersion     = "1"
	vscodeSettingsDir  = ".vscode"
	vscodeSettingsFile = "settings.json"
	trustedCommandsKey = "kiroAgent.trustedCommands"
	sessionIDFile      = "kiro-active-session"
	activeTurnFile     = "kiro-active-turn"
	toolCallsFile      = "kiro-tool-calls.jsonl"
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
		cwd := raw.CWD
		if cwd == "" {
			cwd = protocol.RepoRoot()
		}
		identity := a.resolveHookSessionIdentity(cwd, raw)
		if identity.entireSessionID == "" {
			identity.entireSessionID = a.generateAndCacheSessionID()
		} else {
			a.commitSessionIdentity(identity)
		}
		// Bind this turn to the resolved IDE chat so post-tool-use and
		// stop hooks read/write the same chat even if the user switches
		// tabs mid-turn (re-resolving by mtime alone could rebind).
		a.writeActiveTurnIDESessionID(identity.ideSessionID)
		prompt := raw.Prompt
		if prompt == "" {
			prompt = os.Getenv("USER_PROMPT")
		}
		return &protocol.EventJSON{
			Type:      2,
			SessionID: identity.entireSessionID,
			Prompt:    prompt,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil
	case HookNamePreToolUse:
		return nil, nil
	case HookNamePostToolUse:
		if raw.ToolName != "" {
			cwd := raw.CWD
			if cwd == "" {
				cwd = protocol.RepoRoot()
			}
			a.appendToolCall(raw.ToolName, raw.ToolInput, a.resolvePostToolUseChatKey(cwd, raw))
		}
		return nil, nil
	case HookNameStop:
		cwd := raw.CWD
		if cwd == "" {
			cwd = protocol.RepoRoot()
		}
		identity := a.resolveStopIdentity(cwd, raw)
		if identity.entireSessionID == "" {
			identity.entireSessionID = fallbackStopSessionID()
		}
		a.commitSessionIdentity(identity)
		sessionRef := a.captureTranscriptForStop(cwd, identity)
		a.clearToolCalls(identity.ideSessionID)
		// Only clear the IDE turn binding when this stop owns it. CLI
		// stops (no ideSessionID) must leave kiro-active-turn alone so a
		// concurrent in-flight IDE turn isn't stranded.
		if identity.ideSessionID != "" {
			a.clearActiveTurnIDESessionID()
		}
		return &protocol.EventJSON{
			Type:       3,
			SessionID:  identity.entireSessionID,
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
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
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
	commands = removeKnownTrustedCommands(commands)
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
		if !isKnownTrustedCommand(command) {
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
		if runtimeGOOS == "windows" {
			return "go run %KIRO_PROJECT_DIR%/cmd/entire/main.go hooks kiro "
		}
		return "go run ${KIRO_PROJECT_DIR}/cmd/entire/main.go hooks kiro "
	}
	return "entire hooks kiro "
}

// shellWrappedCommand wraps a hook command so the IDE-spawned process always
// has a closed stdin. IDEs run hook commands directly without a shell, so a
// bare redirect token is passed as a literal arg instead of being interpreted.
// On POSIX we wrap in `sh -c '<cmd> </dev/null'`; on Windows we wrap in
// `cmd /c "<cmd> <NUL"` so the parent CLI process does not block reading from
// an inherited Kiro IDE stdin pipe that never gets closed.
// The command content is built from compile-time constants (not user input).
func shellWrappedCommand(cmd string) string {
	if runtimeGOOS == "windows" {
		return `cmd /c "` + cmd + ` <NUL"`
	}
	return "sh -c '" + cmd + " </dev/null'"
}

func trustedCommand(localDev bool) string {
	if localDev {
		if runtimeGOOS == "windows" {
			return `cmd /c "go run %KIRO_PROJECT_DIR%/cmd/entire/main.go hooks *`
		}
		return "sh -c 'go run ${KIRO_PROJECT_DIR}/cmd/entire/main.go hooks *"
	}
	if runtimeGOOS == "windows" {
		return `cmd /c "entire hooks *`
	}
	return "sh -c 'entire hooks *"
}

func knownTrustedCommands() []string {
	return []string{
		"sh -c 'entire hooks *",
		"sh -c 'go run ${KIRO_PROJECT_DIR}/cmd/entire/main.go hooks *",
		`cmd /c "entire hooks *`,
		`cmd /c "go run %KIRO_PROJECT_DIR%/cmd/entire/main.go hooks *`,
		"entire hooks *",
		"go run %KIRO_PROJECT_DIR%/cmd/entire/main.go hooks *",
	}
}

func isKnownTrustedCommand(command string) bool {
	for _, known := range knownTrustedCommands() {
		if command == known {
			return true
		}
	}
	return false
}

func removeKnownTrustedCommands(commands []string) []string {
	filtered := commands[:0]
	for _, command := range commands {
		if !isKnownTrustedCommand(command) {
			filtered = append(filtered, command)
		}
	}
	return filtered
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := marshalJSON(settings)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (a *Agent) generateAndCacheSessionID() string {
	sessionID := generateSessionID()
	cachePath := a.sessionIDCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err == nil {
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

func (a *Agent) cacheSessionID(sessionID string) {
	if sessionID == "" {
		return
	}
	cachePath := a.sessionIDCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err == nil {
		_ = os.WriteFile(cachePath, []byte(sessionID), 0o600)
	}
}

// clearToolCalls removes the per-chat tool-calls jsonl file (if any).
// Called defensively at end-of-turn in case enrichment skipped the
// readAndClearToolCalls path. chatKey="" targets the legacy global file
// used by Kiro CLI sessions that have no IDE chat ID.
func (a *Agent) clearToolCalls(chatKey string) {
	_ = os.Remove(a.toolCallsPath(chatKey))
}

// appendToolCall records one post-tool-use entry to the jsonl file for the
// given chat. Per-chat keying prevents one Kiro chat's tool calls from
// being captured by another chat's stop hook in multi-chat workspaces.
func (a *Agent) appendToolCall(name string, input json.RawMessage, chatKey string) {
	call := kiroToolCall{Name: name, Args: input}
	line, err := json.Marshal(call)
	if err != nil {
		return
	}
	path := a.toolCallsPath(chatKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(line, '\n'))
}

// readAndClearToolCalls drains the per-chat tool-calls jsonl into memory
// and removes the file. chatKey="" reads the legacy global file (CLI flow).
func (a *Agent) readAndClearToolCalls(chatKey string) []kiroToolCall {
	path := a.toolCallsPath(chatKey)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_ = os.Remove(path)

	var calls []kiroToolCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var call kiroToolCall
		if err := json.Unmarshal([]byte(line), &call); err == nil {
			calls = append(calls, call)
		}
	}
	return calls
}

// toolCallsPath returns the per-chat tool-calls file. chatKey="" returns
// the legacy global file shared by all Kiro CLI sessions on this repo
// (CLI is single-conversation per-cwd, so global is fine there).
func (a *Agent) toolCallsPath(chatKey string) string {
	dir := filepath.Join(protocol.RepoRoot(), ".entire", "tmp")
	if chatKey == "" {
		return filepath.Join(dir, toolCallsFile)
	}
	return filepath.Join(dir, "kiro-tool-calls-"+sanitizeForFilename(chatKey)+".jsonl")
}

func (a *Agent) sessionIDCachePath() string {
	return filepath.Join(protocol.RepoRoot(), ".entire", "tmp", sessionIDFile)
}

// activeTurnIDECachePath holds the IDE session ID for the in-flight turn
// (set at user-prompt-submit, cleared at stop). It binds the whole turn —
// post-tool-use writes and stop reads — to the chat that fired prompt-submit
// so a tab switch mid-turn cannot rebind to a different chat.
func (a *Agent) activeTurnIDECachePath() string {
	return filepath.Join(protocol.RepoRoot(), ".entire", "tmp", activeTurnFile)
}

func (a *Agent) readActiveTurnIDESessionID() string {
	data, err := os.ReadFile(a.activeTurnIDECachePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (a *Agent) writeActiveTurnIDESessionID(ideSessionID string) {
	path := a.activeTurnIDECachePath()
	if ideSessionID == "" {
		_ = os.Remove(path)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		_ = os.WriteFile(path, []byte(ideSessionID), 0o600)
	}
}

func (a *Agent) clearActiveTurnIDESessionID() {
	_ = os.Remove(a.activeTurnIDECachePath())
}

// resolvePostToolUseChatKey decides which tool-calls jsonl file a
// post-tool-use hook should append to. The key MUST match what the
// corresponding stop hook will read with, or the tool calls get
// stranded in a file the stop never consumes.
//
// Precedence:
//  1. Explicit IDE session ID in payload — write to that IDE chat's file
//     (matches resolveStopIdentity / resolveHookSessionIdentity).
//  2. Explicit CLI conversation_id in payload — write to the global file
//     (key=""), matching CLI stop's `clearToolCalls("")`. We do NOT fall
//     back to the latest IDE chat here; that would silently route CLI
//     tool calls into an IDE-scoped file that CLI stop never clears.
//  3. Active-turn IDE cache — write to that IDE chat's file.
//  4. Most-recently-active IDE chat by mtime — best-effort guess for a
//     stray post-tool-use without prior prompt-submit.
//  5. "" (global file) when no IDE chat exists at all (pure CLI flow).
func (a *Agent) resolvePostToolUseChatKey(cwd string, raw hookInputRaw) string {
	if ideID := rawIDESessionID(raw); ideID != "" {
		return ideID
	}
	if convID := rawConversationID(raw); convID != "" {
		// CLI flow — global tool-calls file (CLI is single-conversation
		// per cwd, and stop will clear with key="").
		return ""
	}
	if id := a.readActiveTurnIDESessionID(); id != "" {
		return id
	}
	if cwd == "" {
		return ""
	}
	id, err := mostRecentlyActiveIDESessionID(cwd)
	if err != nil {
		return ""
	}
	return id
}

// resolveStopIdentity is the stop-hook variant of resolveHookSessionIdentity
// that prefers the IDE session ID bound to the current turn (set at
// user-prompt-submit) over fresh mtime resolution. Without this, a tab
// switch between user-prompt-submit and stop would let the turn finalize
// against a different chat than the one that fired the prompt.
func (a *Agent) resolveStopIdentity(cwd string, raw hookInputRaw) sessionIdentity {
	if ideID := rawIDESessionID(raw); ideID != "" {
		return sessionIdentity{
			entireSessionID: ideID,
			ideSessionID:    ideID,
			conversationID:  rawConversationID(raw),
		}
	}
	// Explicit CLI conversation_id in the payload must beat the IDE
	// active-turn cache. The cache is repo-global, so a concurrent IDE
	// turn could otherwise hijack a CLI stop and route it through the
	// IDE transcript path.
	if convID := rawConversationID(raw); convID != "" {
		return sessionIdentity{
			entireSessionID: convID,
			conversationID:  convID,
		}
	}
	if turnIDE := a.readActiveTurnIDESessionID(); turnIDE != "" {
		// Intentionally leave conversationID empty. We have no proof
		// that any CLI conversation in this repo belongs to this IDE
		// turn, and if ensureIDETranscript later degrades the CLI
		// fallback would otherwise capture an unrelated CLI transcript
		// under the IDE session's file.
		return sessionIdentity{
			entireSessionID: turnIDE,
			ideSessionID:    turnIDE,
		}
	}
	return a.resolveHookSessionIdentity(cwd, raw)
}

// sessionIdentity is the resolved session state for a single hook invocation.
//   - entireSessionID is the stable Entire-side ID emitted on EventJSON. For
//     IDE-backed chats it equals ideSessionID, which keeps each Kiro chat
//     mapped to a deterministic, lifetime-stable Entire session without
//     needing per-chat state files.
//   - ideSessionID is the Kiro IDE session UUID for the current chat, used
//     for transcript file lookup and per-chat trim-offset bucketing. Empty
//     when no IDE chat is in play (CLI-only sessions).
//   - conversationID is the Kiro CLI conversation_id, used for SQLite
//     transcript queries. Empty when no CLI session is in play.
type sessionIdentity struct {
	entireSessionID string
	ideSessionID    string
	conversationID  string
}

// resolveHookSessionIdentity decides the (entireSessionID, ideSessionID,
// conversationID) tuple for the current hook event.
//
// Precedence:
//  1. Explicit `session_id` / `sessionId` / `chatSessionId` / `conversation_id`
//     in the raw hook payload — Kiro is telling us directly.
//  2. Most-recently-active IDE chat detected via mtime of `<id>.json` files
//     in the workspace-sessions directory. The IDE writes the user prompt /
//     assistant response to the active chat's transcript file before firing
//     the hook, so the freshest mtime points at the chat that fired. We use
//     that IDE session UUID as both the Kiro and Entire session identifiers
//     so each chat is keyed deterministically — no global cache that could
//     overwrite another chat's state, and no rekey needed.
//  3. Repo-global `kiro-active-session` cache (CLI flow), then CLI
//     conversation_id, then empty (caller mints a UUID).
func (a *Agent) resolveHookSessionIdentity(cwd string, raw hookInputRaw) sessionIdentity {
	identity := sessionIdentity{}

	// Explicit IDE session ID in the payload wins for IDE flows.
	if ideID := rawIDESessionID(raw); ideID != "" {
		identity.entireSessionID = ideID
		identity.ideSessionID = ideID
		identity.conversationID = rawConversationID(raw)
		return identity
	}

	// Explicit CLI conversation_id in the payload tells us this is a CLI
	// turn — short-circuit before IDE mtime discovery so a repo that
	// happens to have IDE workspace data alongside CLI usage doesn't
	// accidentally checkpoint an unrelated IDE chat. ideSessionID stays
	// empty so captureTranscriptForStop skips IDE transcript capture.
	if convID := rawConversationID(raw); convID != "" {
		identity.entireSessionID = convID
		identity.conversationID = convID
		return identity
	}

	// No payload signals — fall back to disk discovery. CLI conversation
	// queried from SQLite goes only into conversationID; mtime-detected
	// IDE chat sets both entireSessionID and ideSessionID.
	if cwd != "" {
		if cid, err := a.querySessionID(cwd); err == nil {
			identity.conversationID = cid
		}
	}

	if cwd != "" {
		if activeIDE, err := mostRecentlyActiveIDESessionID(cwd); err == nil && activeIDE != "" {
			identity.entireSessionID = activeIDE
			identity.ideSessionID = activeIDE
			return identity
		}
	}

	if cached := a.readCachedSessionID(); cached != "" {
		identity.entireSessionID = cached
		return identity
	}

	if identity.conversationID != "" {
		identity.entireSessionID = identity.conversationID
	}
	return identity
}

// commitSessionIdentity persists the resolved identity to the legacy
// repo-global cache so subsequent hooks in the CLI (non-IDE) flow can reuse
// it. IDE-backed identities don't need persistence — they're re-derived
// from the IDE workspace-sessions directory each time, which is the only
// way to correctly handle multi-chat workspaces without per-tab signals.
func (a *Agent) commitSessionIdentity(identity sessionIdentity) {
	if identity.ideSessionID != "" {
		// Per-chat IDE state is derived from disk; no cache write needed.
		// Doing nothing here also avoids overwriting the CLI cache from an
		// IDE turn (which would corrupt a concurrent Kiro CLI session).
		return
	}
	a.cacheSessionID(identity.entireSessionID)
}

// rawIDESessionID returns the IDE session UUID supplied in the hook
// payload, if any. CLI's `conversation_id` is intentionally NOT
// considered here: mistaking it for an IDE session ID would cause the
// stop hook to attempt IDE transcript capture against a non-existent
// `<conversation_id>.json` and then silently fall back to the latest IDE
// session, checkpointing an unrelated chat.
func rawIDESessionID(raw hookInputRaw) string {
	for _, value := range []string{
		raw.SessionID,
		raw.SessionIDAlt,
		raw.ChatSessionID,
	} {
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return ""
}

// rawConversationID returns only the CLI `conversation_id` field. It does
// NOT fall back to `chatSessionId`, which references a Kiro IDE
// execution log and would point ensureCachedTranscript at a wrong
// SQLite row.
func rawConversationID(raw hookInputRaw) string {
	return strings.TrimSpace(raw.ConversationID)
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
