package zcode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-zcode/internal/protocol"
)

// Hook names follow Entire's kebab-case external-agent convention. They map
// to ZCode's native hook events:
//
//	session-start ← SessionStart (source: startup/resume/clear)
//	compaction    ← SessionStart (source: compact) — installed as a separate
//	                matcher so ZCode routes it to its own hook name
//	turn-start    ← UserPromptSubmit
//	turn-end      ← Stop
const (
	HookNameSessionStart = "session-start"
	HookNameTurnStart    = "turn-start"
	HookNameTurnEnd      = "turn-end"
	HookNameCompaction   = "compaction"
)

// hookStatusMarker identifies our hook entries inside ZCode's user config.
// ZCode drops unknown keys on its own rewrites of config.json, so entries are
// recognized structurally (args referencing `hooks zcode <name>`) as well.
const hookStatusMarker = "entire-agent-zcode"

// hookSpec describes one hook entry to install.
type hookSpec struct {
	event   string // ZCode native event name
	hook    string // Entire hook name passed to `entire hooks zcode <hook>`
	matcher string // regex matched against the event's match value ("" = all)
}

var hookSpecs = []hookSpec{
	{event: "SessionStart", hook: HookNameSessionStart, matcher: ""},
	// "compact" alone would also match on the matcher for session-start
	// installed above firing with source=compact; an anchored regex keeps the
	// two routes disjoint.
	{event: "SessionStart", hook: HookNameCompaction, matcher: "^compact$"},
	{event: "UserPromptSubmit", hook: HookNameTurnStart},
	{event: "Stop", hook: HookNameTurnEnd},
}

// zcodeHookPayload is the stdin JSON ZCode sends to hooks. Common fields
// plus per-event extras (source, prompt, last_assistant_message).
type zcodeHookPayload struct {
	SessionID            string          `json:"session_id"`
	TranscriptPath       string          `json:"transcript_path"`
	Cwd                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	Source               string          `json:"source"`
	Model                string          `json:"model"`
	Prompt               string          `json:"prompt"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	ToolName             string          `json:"tool_name"`
	ToolUseID            string          `json:"tool_use_id"`
	ToolInput            json.RawMessage `json:"tool_input"`
}

func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if len(input) == 0 {
		return nil, nil
	}
	var raw zcodeHookPayload
	if err := json.Unmarshal(input, &raw); err != nil {
		return nil, fmt.Errorf("parse %s payload: %w", hookName, err)
	}
	if raw.SessionID == "" {
		return nil, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	switch hookName {
	case HookNameSessionStart:
		// source=compact means compaction; ZCode also fires SessionStart on
		// resume/clear which are still session starts for Entire's purposes.
		eventType := 1
		if raw.Source == "compact" {
			eventType = 4
		}
		return &protocol.EventJSON{
			Type:       eventType,
			SessionID:  raw.SessionID,
			SessionRef: transcriptPath(raw.SessionID),
			Model:      raw.Model,
			Timestamp:  now,
		}, nil

	case HookNameCompaction:
		return &protocol.EventJSON{
			Type:       4,
			SessionID:  raw.SessionID,
			SessionRef: transcriptPath(raw.SessionID),
			Timestamp:  now,
		}, nil

	case HookNameTurnStart:
		return &protocol.EventJSON{
			Type:       2,
			SessionID:  raw.SessionID,
			SessionRef: transcriptPath(raw.SessionID),
			Prompt:     raw.Prompt,
			Timestamp:  now,
		}, nil

	case HookNameTurnEnd:
		return &protocol.EventJSON{
			Type:            3,
			SessionID:       raw.SessionID,
			SessionRef:      transcriptPath(raw.SessionID),
			ResponseMessage: raw.LastAssistantMessage,
			Timestamp:       now,
		}, nil

	default:
		return nil, nil
	}
}

// loadConfig reads ZCode's user config.json into a generic map so every
// unrelated key survives the round-trip. Missing file → empty config.
func loadConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return map[string]any{}, nil
	}
	config := map[string]any{}
	if err := json.Unmarshal([]byte(trimmed), &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return config, nil
}

func saveConfig(path string, config map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, append(data, '\n'), 0o600)
}

func asObject(v any) map[string]any {
	obj, _ := v.(map[string]any)
	return obj
}

func asArray(v any) []any {
	arr, _ := v.([]any)
	return arr
}

// entireHookCommand builds the `type: "process"` hook that invokes Entire.
// localDev reroutes through `go run` on the Entire checkout.
func entireHookCommand(localDev bool, hook string) map[string]any {
	if localDev {
		return map[string]any{
			"type":          "command",
			"command":       `go run "$(git rev-parse --show-toplevel)"/cmd/entire/main.go hooks zcode ` + hook,
			"statusMessage": hookStatusMarker + ": " + hook,
		}
	}
	return map[string]any{
		"type":          "process",
		"command":       "entire",
		"args":          []any{"hooks", "zcode", hook},
		"statusMessage": hookStatusMarker + ": " + hook,
	}
}

// isOurGroup reports whether a matcher group from config.json was installed
// by us. The structural check (args `hooks zcode <hook>`) survives ZCode
// dropping the statusMessage key.
func isOurGroup(group any, hook string) bool {
	obj := asObject(group)
	if obj == nil {
		return false
	}
	for _, h := range asArray(obj["hooks"]) {
		hookObj := asObject(h)
		if hookObj == nil {
			continue
		}
		args := asArray(hookObj["args"])
		if len(args) == 3 && args[0] == "hooks" && args[1] == "zcode" && args[2] == hook {
			return true
		}
		// statusMessage fallback for shell-style (local-dev) entries: match on
		// both the marker and the specific hook name so sibling hooks installed
		// under the same marker are not confused for each other.
		if status, ok := hookObj["statusMessage"].(string); ok &&
			strings.Contains(status, hookStatusMarker) && strings.Contains(status, hook) {
			return true
		}
	}
	return false
}

func (a *Agent) InstallHooks(localDev bool, force bool) (int, error) {
	path := configPath()
	config, err := loadConfig(path)
	if err != nil {
		return 0, err
	}

	hooks := asObject(config["hooks"])
	if hooks == nil {
		hooks = map[string]any{}
		config["hooks"] = hooks
	}
	// Configuration-file hooks are disabled unless enabled=true.
	hooks["enabled"] = true
	events := asObject(hooks["events"])
	if events == nil {
		events = map[string]any{}
		hooks["events"] = events
	}

	installed := 0
	for _, spec := range hookSpecs {
		groups := asArray(events[spec.event])
		present := false
		for _, group := range groups {
			if isOurGroup(group, spec.hook) {
				present = true
				break
			}
		}
		if present && !force {
			continue
		}
		entry := map[string]any{
			"hooks": []any{entireHookCommand(localDev, spec.hook)},
		}
		if spec.matcher != "" {
			entry["matcher"] = spec.matcher
		}
		// With --force, replace our stale entry instead of stacking duplicates.
		if force && present {
			replaced := groups[:0]
			for _, group := range groups {
				if !isOurGroup(group, spec.hook) {
					replaced = append(replaced, group)
				}
			}
			groups = replaced
		}
		events[spec.event] = append(groups, entry)
		installed++
	}

	if installed == 0 {
		return 0, nil
	}
	if err := saveConfig(path, config); err != nil {
		return 0, fmt.Errorf("write config: %w", err)
	}
	return installed, nil
}

func (a *Agent) UninstallHooks() error {
	path := configPath()
	config, err := loadConfig(path)
	if err != nil {
		return err
	}
	hooks := asObject(config["hooks"])
	if hooks == nil {
		return nil
	}
	events := asObject(hooks["events"])
	if events == nil {
		return nil
	}

	changed := false
	for _, spec := range hookSpecs {
		groups, ok := events[spec.event].([]any)
		if !ok {
			continue
		}
		kept := groups[:0]
		for _, group := range groups {
			if isOurGroup(group, spec.hook) {
				changed = true
				continue
			}
			kept = append(kept, group)
		}
		if len(kept) == 0 {
			delete(events, spec.event)
		} else {
			events[spec.event] = kept
		}
	}
	if !changed {
		return nil
	}
	if len(events) == 0 {
		delete(hooks, "events")
		if enabled, ok := hooks["enabled"].(bool); ok && enabled {
			hooks["enabled"] = false
		}
	}
	return saveConfig(path, config)
}

func (a *Agent) AreHooksInstalled() bool {
	config, err := loadConfig(configPath())
	if err != nil {
		return false
	}
	hooks := asObject(config["hooks"])
	if hooks == nil {
		return false
	}
	events := asObject(hooks["events"])
	if events == nil {
		return false
	}
	for _, spec := range hookSpecs {
		found := false
		for _, group := range asArray(events[spec.event]) {
			if isOurGroup(group, spec.hook) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
