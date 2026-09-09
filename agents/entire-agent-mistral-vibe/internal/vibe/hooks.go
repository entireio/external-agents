package vibe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	managedHookBlockStart = "# BEGIN ENTIRE MISTRAL VIBE HOOKS"
	managedHookBlockEnd   = "# END ENTIRE MISTRAL VIBE HOOKS"
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

	sessionID := payload.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("vibe-%d", time.Now().UnixNano())
	}

	switch hookName {
	case HookNameSessionStart:
		return &protocol.EventJSON{
			Type:      EventTypeSessionStart,
			SessionID: sessionID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil

	case HookNameUserPromptSubmit:
		return &protocol.EventJSON{
			Type:      EventTypeTurnStart,
			SessionID: sessionID,
			Prompt:    payload.Prompt,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}, nil

	case HookNameTurnEnd:
		sessionRef := a.cacheTranscriptForTurnEnd(sessionID)
		return &protocol.EventJSON{
			Type:       EventTypeTurnEnd,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
		}, nil

	case HookNamePreToolUse, HookNamePostToolUse:
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

	configPath := filepath.Join(configDir, vibeConfigFile)
	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return 0, fmt.Errorf("failed to read config.toml: %w", err)
	}

	content := mergeHookConfig(string(existing), renderManagedHookBlock(commandBase))
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		return 0, fmt.Errorf("failed to write config.toml: %w", err)
	}

	// Trust the repo directory so Vibe reads the project-level config.
	if err := trustDirectory(repoRoot); err != nil {
		// Non-fatal: hooks are installed even if trust fails.
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to trust directory: %v\n", err)
	}

	return len(managedHookEntries()), nil
}

// trustDirectory adds the given path to Vibe's trusted folders list
// (~/.vibe/trusted_folders.toml) so that project-level .vibe/config.toml
// is read by Vibe when running in that directory.
func trustDirectory(dir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	trustFile := filepath.Join(home, ".vibe", "trusted_folders.toml")

	// Resolve symlinks to match Vibe's normalization.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolved = dir
	}
	absDir, err := filepath.Abs(resolved)
	if err != nil {
		absDir = resolved
	}

	// Read existing trusted folders file.
	data, err := os.ReadFile(trustFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	content := string(data)

	trusted, foundTrusted, err := readTOMLStringArray(content, "trusted")
	if err != nil {
		return err
	}
	for _, path := range trusted {
		if path == absDir {
			return nil
		}
	}
	if !foundTrusted {
		trusted = nil
	}
	trusted = append(trusted, absDir)

	untrusted, foundUntrusted, err := readTOMLStringArray(content, "untrusted")
	if err != nil {
		return err
	}
	if !foundUntrusted {
		untrusted = nil
	}

	updated := upsertTOMLStringArray(content, "trusted", trusted)
	updated = upsertTOMLStringArray(updated, "untrusted", untrusted)

	if strings.TrimSpace(updated) == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(trustFile), 0o700); err != nil {
		return err
	}
	return os.WriteFile(trustFile, []byte(updated), 0o600)
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

	remaining := removeManagedHookConfig(string(data))
	if remaining == "" || remaining == "[hooks]" {
		return os.Remove(configPath)
	}

	return os.WriteFile(configPath, []byte(remaining), 0o600)
}

// AreHooksInstalled checks whether .vibe/config.toml contains Entire CLI hook entries.
func (a *Agent) AreHooksInstalled() bool {
	repoRoot := protocol.RepoRoot()
	configPath := filepath.Join(repoRoot, vibeConfigDir, vibeConfigFile)

	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	content := string(data)
	return strings.Contains(content, managedHookBlockStart) || strings.Contains(content, hookMarker)
}

// cacheTranscriptForTurnEnd copies the Vibe native transcript into
// .entire/tmp/{sessionID}.json so the entire CLI has a local session file
// for persistence. If the native log cannot be found, a placeholder is written.
func (a *Agent) cacheTranscriptForTurnEnd(sessionID string) string {
	repoRoot := protocol.RepoRoot()
	sessionDir, err := a.GetSessionDir(repoRoot)
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return ""
	}
	cachePath := a.ResolveSessionFile(sessionDir, sessionID)

	// Try to copy from the native Vibe session log.
	if nativeRef := findVibeSessionRef(sessionID); nativeRef != "" {
		data, err := os.ReadFile(nativeRef)
		if err == nil {
			if err := os.WriteFile(cachePath, data, 0o600); err == nil {
				return cachePath
			}
		}
	}

	// Fallback: write a placeholder so the session file exists.
	if err := os.WriteFile(cachePath, []byte("{}"), 0o600); err != nil {
		return ""
	}
	return cachePath
}

// findVibeSessionRef finds the messages.jsonl file for a Vibe session by
// globbing the session log directory for matching session folders.
func findVibeSessionRef(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	prefix := sessionID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	logDir := filepath.Join(home, ".vibe", "logs", "session")
	pattern := filepath.Join(logDir, fmt.Sprintf("session_*_%s", prefix))
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	// Use the most recently modified match.
	best := matches[0]
	var bestMtime int64
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.ModTime().UnixNano() > bestMtime {
			bestMtime = info.ModTime().UnixNano()
			best = m
		}
	}
	return filepath.Join(best, "messages.jsonl")
}

func managedHookEntries() []struct {
	nativeName   string
	protocolName string
} {
	return []struct {
		nativeName   string
		protocolName string
	}{
		{VibeNativeSessionStart, HookNameSessionStart},
		{VibeNativeUserPromptSubmit, HookNameUserPromptSubmit},
		{VibeNativePreToolUse, HookNamePreToolUse},
		{VibeNativePostToolUse, HookNamePostToolUse},
		{VibeNativeTurnEnd, HookNameTurnEnd},
	}
}

func renderManagedHookBlock(commandBase string) string {
	lines := []string{
		managedHookBlockStart,
		"# Entire CLI hook configuration",
		"# Managed by entire-agent-mistral-vibe",
	}
	for _, hook := range managedHookEntries() {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("[[hooks.%s]]", hook.nativeName))
		lines = append(lines, fmt.Sprintf(`command = "%s%s"`, commandBase, hook.protocolName))
	}
	lines = append(lines, "", managedHookBlockEnd)
	return strings.Join(lines, "\n")
}

func mergeHookConfig(existing string, managedBlock string) string {
	cleaned := removeManagedHookConfig(existing)
	if cleaned == "" {
		return managedBlock + "\n"
	}
	return managedBlock + "\n" + cleaned
}

func removeManagedHookConfig(content string) string {
	content = removeManagedHookBlock(content)
	content = removeLegacyManagedHookConfig(content)
	return strings.TrimLeft(content, "\n")
}

func removeManagedHookBlock(content string) string {
	for {
		start := strings.Index(content, managedHookBlockStart)
		if start == -1 {
			return content
		}
		end := strings.Index(content[start:], managedHookBlockEnd)
		if end == -1 {
			return content
		}
		end += start + len(managedHookBlockEnd)
		if end < len(content) && content[end] == '\n' {
			end++
		}
		content = content[:start] + content[end:]
	}
}

func removeLegacyManagedHookConfig(content string) string {
	lines := strings.Split(content, "\n")
	var kept []string

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "Managed by entire-agent-mistral-vibe") ||
			strings.Contains(line, "Entire CLI hook configuration") {
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "[[hooks.") {
			j := i + 1
			managed := false
			for j < len(lines) {
				nextTrimmed := strings.TrimSpace(lines[j])
				if strings.HasPrefix(nextTrimmed, "[[hooks.") {
					break
				}
				if strings.Contains(lines[j], hookMarker) {
					managed = true
				}
				j++
			}
			if managed {
				i = j
				for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
					i++
				}
				continue
			}
			kept = append(kept, lines[i:j]...)
			i = j
			continue
		}
		kept = append(kept, line)
		i++
	}

	result := strings.Join(kept, "\n")
	if result == "" {
		return ""
	}
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

func readTOMLStringArray(content string, key string) ([]string, bool, error) {
	re := regexp.MustCompile(`(?ms)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*\[(.*?)\]`)
	match := re.FindStringSubmatch(content)
	if match == nil {
		return nil, false, nil
	}
	itemRE := regexp.MustCompile(`"(?:\\.|[^"])*"|'(?:[^'\\]|\\.)*'`)
	rawItems := itemRE.FindAllString(match[1], -1)
	values := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		parsed, err := parseTOMLStringLiteral(raw)
		if err != nil {
			return nil, false, err
		}
		values = append(values, parsed)
	}
	return values, true, nil
}

func parseTOMLStringLiteral(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if raw[0] == '"' {
		return strconv.Unquote(raw)
	}
	if raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], nil
	}
	return "", fmt.Errorf("unsupported TOML string literal: %s", raw)
}

func upsertTOMLStringArray(content string, key string, values []string) string {
	formatted := formatTOMLStringArray(key, values)
	re := regexp.MustCompile(`(?ms)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*\[(.*?)\]\n?`)
	if re.MatchString(content) {
		return re.ReplaceAllString(content, formatted)
	}
	if strings.TrimSpace(content) == "" {
		return formatted
	}
	if strings.HasSuffix(content, "\n") {
		return content + formatted
	}
	return content + "\n" + formatted
}

func formatTOMLStringArray(key string, values []string) string {
	var b strings.Builder
	b.WriteString(key)
	b.WriteString(" = [\n")
	for _, value := range values {
		b.WriteString(fmt.Sprintf("    %q,\n", value))
	}
	b.WriteString("]\n")
	return b.String()
}
