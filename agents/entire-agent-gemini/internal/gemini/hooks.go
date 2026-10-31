package gemini

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-gemini/internal/protocol"
)

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	raw, err := parseHookInput(input)
	if err != nil {
		return nil, err
	}
	raw.HookEventName = geminiEventName(hookName, raw.HookEventName)
	normalizeRawHook(&raw)

	if err := a.appendSidecar(raw); err != nil {
		return nil, err
	}

	sessionRef := a.sidecarPath(raw.SessionID)
	metadata := hookMetadata(raw)

	switch hookName {
	case HookNameSessionStart:
		return &protocol.EventJSON{
			Type:       1,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Model:      raw.Model,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	case HookNameBeforeAgent:
		if strings.TrimSpace(raw.Prompt) == "" && strings.TrimSpace(raw.UserPrompt) == "" {
			return nil, nil
		}
		prompt := raw.Prompt
		if prompt == "" {
			prompt = raw.UserPrompt
		}
		return &protocol.EventJSON{
			Type:       2,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Prompt:     prompt,
			Model:      raw.Model,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	case HookNameAfterAgent:
		return &protocol.EventJSON{
			Type:            3,
			SessionID:       raw.SessionID,
			SessionRef:      sessionRef,
			Model:           raw.Model,
			Timestamp:       raw.Timestamp,
			ResponseMessage: raw.LastAssistantMessage,
			Metadata:        metadata,
		}, nil
	case HookNameSessionEnd:
		return &protocol.EventJSON{
			Type:       5,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	case HookNamePreCompress:
		return &protocol.EventJSON{
			Type:       4,
			SessionID:  raw.SessionID,
			SessionRef: sessionRef,
			Timestamp:  raw.Timestamp,
			Metadata:   metadata,
		}, nil
	default:
		return nil, nil
	}
}

func (a *Agent) InstallHooks(_ bool, _ bool) (int, error) {
	repoRoot := protocol.RepoRoot()
	settingsDir := filepath.Join(repoRoot, ".gemini")
	settingsPath := filepath.Join(settingsDir, settingsFileName)

	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return 0, fmt.Errorf("create .gemini dir: %w", err)
	}

	existing := map[string]json.RawMessage{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("read settings: %w", err)
	}

	hooksRaw, hasHooks := existing["hooks"]
	hooksMap := map[string]json.RawMessage{}
	if hasHooks {
		_ = json.Unmarshal(hooksRaw, &hooksMap)
	}

	count := 0
	for _, spec := range hookSpecs {
		matchers := parseMatchers(hooksMap[spec.GeminiEvent])
		if hookExists(matchers, spec.Matcher, spec.EntryName) {
			continue
		}
		entry := geminiHookEntry{
			Type:    "command",
			Command: hookCommand(spec.HookName),
			Name:    spec.EntryName,
			Timeout: 10000,
		}
		matchers = upsertMatcher(matchers, spec.Matcher, entry)
		mb, _ := json.Marshal(matchers)
		hooksMap[spec.GeminiEvent] = mb
		count++
	}

	hooksBytes, _ := json.Marshal(hooksMap)
	existing["hooks"] = hooksBytes

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal settings: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return 0, fmt.Errorf("write settings: %w", err)
	}
	return count, nil
}

func (a *Agent) UninstallHooks() error {
	repoRoot := protocol.RepoRoot()
	settingsPath := filepath.Join(repoRoot, ".gemini", settingsFileName)

	data, err := os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	existing := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &existing); err != nil {
		return err
	}

	hooksRaw, hasHooks := existing["hooks"]
	if !hasHooks {
		return nil
	}

	hooksMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(hooksRaw, &hooksMap); err != nil {
		return err
	}

	for _, spec := range hookSpecs {
		matchers := parseMatchers(hooksMap[spec.GeminiEvent])
		filtered := make([]geminiHookMatcher, 0, len(matchers))
		for _, m := range matchers {
			fh := make([]geminiHookEntry, 0, len(m.Hooks))
			for _, h := range m.Hooks {
				if !isEntireHook(h) {
					fh = append(fh, h)
				}
			}
			if len(fh) > 0 {
				m.Hooks = fh
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 0 {
			delete(hooksMap, spec.GeminiEvent)
		} else {
			fb, _ := json.Marshal(filtered)
			hooksMap[spec.GeminiEvent] = fb
		}
	}

	if len(hooksMap) == 0 {
		delete(existing, "hooks")
	} else {
		hb, _ := json.Marshal(hooksMap)
		existing["hooks"] = hb
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(settingsPath, out, 0o644)
}

func (a *Agent) AreHooksInstalled() bool {
	repoRoot := protocol.RepoRoot()
	settingsPath := filepath.Join(repoRoot, ".gemini", settingsFileName)

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}

	existing := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &existing); err != nil {
		return false
	}

	hooksRaw, hasHooks := existing["hooks"]
	if !hasHooks {
		return false
	}

	hooksMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(hooksRaw, &hooksMap); err != nil {
		return false
	}

	for _, spec := range hookSpecs {
		matchers := parseMatchers(hooksMap[spec.GeminiEvent])
		if !hookExists(matchers, spec.Matcher, spec.EntryName) {
			return false
		}
	}
	return true
}

// hookCommand builds the shell command that Gemini CLI will execute for each hook event.
// The command calls `entire hooks gemini <hookName>` which the Entire CLI routes to
// `entire-agent-gemini parse-hook --hook <hookName>`.
func hookCommand(hookName string) string {
	entire := "entire"
	if path, err := exec.LookPath("entire"); err == nil && strings.TrimSpace(path) != "" {
		entire = path
	}
	return fmt.Sprintf("sh -c 'if command -v %s >/dev/null 2>&1; then %s hooks gemini %s >/dev/null 2>&1 || true; fi'",
		shellQuote(entire), shellQuote(entire), hookName)
}

func parseMatchers(raw json.RawMessage) []geminiHookMatcher {
	if len(raw) == 0 {
		return nil
	}
	var matchers []geminiHookMatcher
	if err := json.Unmarshal(raw, &matchers); err != nil {
		var single geminiHookMatcher
		if err := json.Unmarshal(raw, &single); err == nil {
			return []geminiHookMatcher{single}
		}
		return nil
	}
	return matchers
}

func hookExists(matchers []geminiHookMatcher, matcher string, entryName string) bool {
	for _, m := range matchers {
		if m.Matcher != matcher && matcher != "" {
			continue
		}
		for _, h := range m.Hooks {
			if h.Type == "command" && h.Name == entryName && isEntireHook(h) {
				return true
			}
		}
	}
	return false
}

func upsertMatcher(matchers []geminiHookMatcher, matcher string, entry geminiHookEntry) []geminiHookMatcher {
	for i := range matchers {
		if matchers[i].Matcher == matcher {
			for j := range matchers[i].Hooks {
				if isEntireHook(matchers[i].Hooks[j]) && matchers[i].Hooks[j].Name == entry.Name {
					matchers[i].Hooks[j] = entry
					return matchers
				}
			}
			matchers[i].Hooks = append(matchers[i].Hooks, entry)
			return matchers
		}
	}
	return append(matchers, geminiHookMatcher{
		Matcher: matcher,
		Hooks:   []geminiHookEntry{entry},
	})
}

func isEntireHook(hook geminiHookEntry) bool {
	if strings.HasPrefix(hook.Name, "entire-") {
		return true
	}
	return strings.Contains(hook.Command, "entire hooks gemini")
}

func parseHookInput(input []byte) (geminiHookInputRaw, error) {
	var raw geminiHookInputRaw
	if len(input) == 0 {
		raw.SessionID = os.Getenv("GEMINI_SESSION_ID")
		raw.CWD = os.Getenv("GEMINI_CWD")
		return raw, nil
	}
	if err := json.Unmarshal(input, &raw); err != nil {
		return raw, fmt.Errorf("parse hook input: %w", err)
	}
	if raw.SessionID == "" {
		raw.SessionID = os.Getenv("GEMINI_SESSION_ID")
	}
	if raw.CWD == "" {
		raw.CWD = os.Getenv("GEMINI_CWD")
	}
	return raw, nil
}

func geminiEventName(hookName string, existing string) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	for _, spec := range hookSpecs {
		if spec.HookName == hookName {
			return spec.GeminiEvent
		}
	}
	return hookName
}

func normalizeRawHook(raw *geminiHookInputRaw) {
	if strings.TrimSpace(raw.SessionID) == "" {
		raw.SessionID = stubSessionID
	}
	if strings.TrimSpace(raw.Timestamp) == "" {
		raw.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(raw.Prompt) == "" {
		raw.Prompt = raw.UserPrompt
	}
}

func hookMetadata(raw geminiHookInputRaw) map[string]string {
	m := map[string]string{
		"agent":           AgentName,
		"hook_event_name": raw.HookEventName,
	}
	if raw.TranscriptPath != "" {
		m["native_transcript_path"] = raw.TranscriptPath
	}
	if raw.CWD != "" {
		m["cwd"] = raw.CWD
	}
	if raw.Reason != "" {
		m["reason"] = raw.Reason
	}
	if raw.Source != "" {
		m["source"] = raw.Source
	}
	if raw.NotificationType != "" {
		m["notification_type"] = raw.NotificationType
	}
	if raw.Message != "" {
		m["message"] = raw.Message
	}
	if raw.Error != "" {
		m["error"] = raw.Error
	}
	if raw.ToolName != "" {
		m["tool_name"] = raw.ToolName
	}
	return m
}

func (a *Agent) sidecarPath(sessionID string) string {
	sessionDir, _ := a.GetSessionDir(protocol.RepoRoot())
	return a.ResolveSessionFile(sessionDir, sessionID)
}

func (a *Agent) appendSidecar(raw geminiHookInputRaw) error {
	sessionDir, _ := a.GetSessionDir(protocol.RepoRoot())
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}
	path := a.ResolveSessionFile(sessionDir, raw.SessionID)

	record := sidecarRecord{
		V:                    1,
		Agent:                AgentName,
		Event:                raw.HookEventName,
		SessionID:            raw.SessionID,
		TS:                   raw.Timestamp,
		CWD:                  raw.CWD,
		NativeTranscriptPath: raw.TranscriptPath,
		Prompt:               raw.Prompt,
		Model:                raw.Model,
		Reason:               raw.Reason,
		Source:               raw.Source,
		ToolName:             raw.ToolName,
		ToolUseID:            raw.ToolUseID,
		ToolInput:            raw.ToolInput,
		ToolResponse:         raw.ToolResponse,
		Error:                raw.Error,
		ErrorDetails:         raw.ErrorDetails,
		LastAssistantMessage: raw.LastAssistantMessage,
		CompactSummary:       raw.CompactSummary,
		NotificationType:     raw.NotificationType,
		Message:              raw.Message,
	}

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
