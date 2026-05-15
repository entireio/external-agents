package windsurf

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-windsurf/internal/protocol"
)

const (
	hooksFile     = "hooks.json"
	hooksDir      = ".windsurf"
	sessionIDFile = "windsurf-active-session"
	osWindows     = "windows"
)

var runtimeGOOS = runtime.GOOS

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	var raw hookInputRaw
	if len(input) > 0 {
		if err := json.Unmarshal(input, &raw); err != nil {
			return nil, err
		}
	}

	sessionID := strings.TrimSpace(raw.TrajectoryID)
	if sessionID == "" {
		sessionID = a.readCachedSessionID()
	}
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	ts := raw.Timestamp
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}

	switch hookName {
	case HookNamePreUserPrompt:
		a.cacheSessionID(sessionID)
		prompt := extractUserPromptFromToolInfo(raw.ToolInfo)
		if prompt == "" {
			prompt = os.Getenv("USER_PROMPT")
		}
		sessionRef := a.resolveSessionRef(sessionID)
		_ = appendTranscriptRecord(sessionRef, transcriptRecord{
			V: transcriptRecordVersion, Type: transcriptTypePrompt, Content: prompt, TS: ts,
		})
		return &protocol.EventJSON{
			Type:      2,
			SessionID: sessionID,
			Prompt:    prompt,
			Timestamp: ts,
		}, nil

	case HookNamePostWriteCode:
		filePath := extractFilePathFromToolInfo(raw.ToolInfo)
		if filePath != "" {
			sessionRef := a.resolveSessionRef(sessionID)
			_ = appendTranscriptRecord(sessionRef, transcriptRecord{
				V: transcriptRecordVersion, Type: transcriptTypeFile, Path: filePath, TS: ts,
			})
		}
		return nil, nil

	case HookNamePostCascadeResponse:
		response := extractResponseFromToolInfo(raw.ToolInfo)
		sessionRef := a.resolveSessionRef(sessionID)
		_ = appendTranscriptRecord(sessionRef, transcriptRecord{
			V: transcriptRecordVersion, Type: transcriptTypeResponse, Content: response, TS: ts,
		})
		return &protocol.EventJSON{
			Type:       3,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Timestamp:  ts,
		}, nil

	default:
		return nil, nil
	}
}

func (a *Agent) InstallHooks(localDev bool, force bool) (int, error) {
	repoRoot := protocol.RepoRoot()
	if !force && allHooksInstalled(repoRoot, localDev) {
		return 0, nil
	}
	if err := writeHooksConfig(repoRoot, localDev); err != nil {
		return 0, err
	}
	return len(defaultHookNames()), nil
}

func (a *Agent) UninstallHooks() error {
	repoRoot := protocol.RepoRoot()
	hooksPath := filepath.Join(repoRoot, hooksDir, hooksFile)

	data, err := os.ReadFile(hooksPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var config windsurfHooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		// Unparseable file — leave it alone rather than deleting unrelated content.
		return nil
	}

	config.Hooks.PreUserPrompt = removeEntireEntries(config.Hooks.PreUserPrompt)
	config.Hooks.PostWriteCode = removeEntireEntries(config.Hooks.PostWriteCode)
	config.Hooks.PostCascadeResponse = removeEntireEntries(config.Hooks.PostCascadeResponse)

	if hooksMapEmpty(config.Hooks) {
		return os.Remove(hooksPath)
	}

	out, err := marshalJSON(config)
	if err != nil {
		return err
	}
	return os.WriteFile(hooksPath, out, 0o600)
}

func (a *Agent) AreHooksInstalled() bool {
	return allHooksInstalled(protocol.RepoRoot(), false)
}

func defaultHookNames() []string {
	return []string{
		HookNamePreUserPrompt,
		HookNamePostWriteCode,
		HookNamePostCascadeResponse,
	}
}

func allHooksInstalled(repoRoot string, localDev bool) bool {
	hooksPath := filepath.Join(repoRoot, hooksDir, hooksFile)
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		return false
	}
	var config windsurfHooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}
	base := hookCommandBase(localDev)
	return hookCommandPresent(config.Hooks.PreUserPrompt, base+HookNamePreUserPrompt) &&
		hookCommandPresent(config.Hooks.PostWriteCode, base+HookNamePostWriteCode) &&
		hookCommandPresent(config.Hooks.PostCascadeResponse, base+HookNamePostCascadeResponse)
}

func hookCommandPresent(entries []windsurfHookEntry, command string) bool {
	for _, e := range entries {
		if e.Command == command || e.PowerShell == command {
			return true
		}
	}
	return false
}

// upsertEntireEntry adds the Entire command entry if it is not already present.
func upsertEntireEntry(entries []windsurfHookEntry, command string) []windsurfHookEntry {
	if hookCommandPresent(entries, command) {
		return entries
	}
	return append(entries, hookEntry(command))
}

// removeEntireEntries strips all entries whose command starts with an Entire hooks prefix.
func removeEntireEntries(entries []windsurfHookEntry) []windsurfHookEntry {
	var out []windsurfHookEntry
	for _, e := range entries {
		cmd := e.Command
		if cmd == "" {
			cmd = e.PowerShell
		}
		if !strings.HasPrefix(cmd, "entire hooks windsurf ") &&
			!strings.Contains(cmd, "/cmd/entire/main.go hooks windsurf ") {
			out = append(out, e)
		}
	}
	return out
}

func hooksMapEmpty(m windsurfHookMap) bool {
	return len(m.PreUserPrompt) == 0 && len(m.PostWriteCode) == 0 && len(m.PostCascadeResponse) == 0
}

func writeHooksConfig(repoRoot string, localDev bool) error {
	hooksPath := filepath.Join(repoRoot, hooksDir, hooksFile)
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
		return err
	}

	// Read any existing config so we preserve unrelated user hooks.
	var config windsurfHooksConfig
	if data, err := os.ReadFile(hooksPath); err == nil {
		_ = json.Unmarshal(data, &config)
	}

	base := hookCommandBase(localDev)
	config.Hooks.PreUserPrompt = upsertEntireEntry(config.Hooks.PreUserPrompt, base+HookNamePreUserPrompt)
	config.Hooks.PostWriteCode = upsertEntireEntry(config.Hooks.PostWriteCode, base+HookNamePostWriteCode)
	config.Hooks.PostCascadeResponse = upsertEntireEntry(config.Hooks.PostCascadeResponse, base+HookNamePostCascadeResponse)

	data, err := marshalJSON(config)
	if err != nil {
		return err
	}
	return os.WriteFile(hooksPath, data, 0o600)
}

func hookEntry(command string) windsurfHookEntry {
	if runtimeGOOS == osWindows {
		return windsurfHookEntry{PowerShell: command}
	}
	return windsurfHookEntry{Command: command}
}

func hookCommandBase(localDev bool) string {
	if localDev {
		if runtimeGOOS == osWindows {
			return "go run %WINDSURF_PROJECT_DIR%/cmd/entire/main.go hooks windsurf "
		}
		return "go run ${WINDSURF_PROJECT_DIR}/cmd/entire/main.go hooks windsurf "
	}
	return "entire hooks windsurf "
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

func (a *Agent) resolveSessionRef(sessionID string) string {
	repoRoot := protocol.RepoRoot()
	dir, _ := a.GetSessionDir(repoRoot)
	return a.ResolveSessionFile(dir, sessionID)
}

func (a *Agent) sessionIDCachePath() string {
	return filepath.Join(protocol.RepoRoot(), ".entire", "tmp", sessionIDFile)
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

func (a *Agent) readCachedSessionID() string {
	data, err := os.ReadFile(a.sessionIDCachePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func appendTranscriptRecord(path string, rec transcriptRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(data, '\n'))
	return err
}

func extractUserPromptFromToolInfo(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var info toolInfoPreUserPrompt
	if err := json.Unmarshal(raw, &info); err == nil {
		return strings.TrimSpace(info.UserPrompt)
	}
	return ""
}

func extractFilePathFromToolInfo(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var info toolInfoPostWriteCode
	if err := json.Unmarshal(raw, &info); err == nil {
		return strings.TrimSpace(info.FilePath)
	}
	return ""
}

func extractResponseFromToolInfo(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var info toolInfoPostCascadeResponse
	if err := json.Unmarshal(raw, &info); err == nil {
		return strings.TrimSpace(info.Response)
	}
	return ""
}

func generateSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("windsurf-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
