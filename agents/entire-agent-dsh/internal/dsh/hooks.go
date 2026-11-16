package dsh

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-dsh/internal/protocol"
)

const bridgePackage = "@sredevopsorg/dsh-entire-bridge"

// hookMarker is the repository-scoped activation marker read by the DSH Host
// plugin. The plugin checks only for its presence, but the same shape is
// written by the existing DSH bridge and stays useful for diagnostics.
type hookMarker struct {
	Version     int      `json:"version"`
	Agent       string   `json:"agent"`
	Bridge      string   `json:"bridge"`
	HookNames   []string `json:"hook_names"`
	InstalledAt string   `json:"installed_at"`
	LocalDev    bool     `json:"local_dev"`
}

func markerPath() string {
	return filepath.Join(protocol.RepoRoot(), ".entire", "dsh-hooks.json")
}

// InstallHooks writes the DSH bridge activation marker. It is idempotent: an
// already valid marker reports zero new hooks, while --force rewrites it.
func (a *Agent) InstallHooks(localDev bool, force bool) (int, error) {
	path := markerPath()
	existing, readErr := os.ReadFile(path) // #nosec G304 -- marker path is anchored at ENTIRE_REPO_ROOT
	if readErr == nil && !force {
		var marker hookMarker
		if json.Unmarshal(existing, &marker) == nil && markerMatches(marker) {
			return 0, nil
		}
	}

	marker := hookMarker{
		Version:     1,
		Agent:       agentName,
		Bridge:      bridgePackage,
		HookNames:   append([]string(nil), hookNames...),
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		LocalDev:    localDev,
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal DSH hook marker: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return 0, fmt.Errorf("write DSH hook marker: %w", err)
	}
	return len(hookNames), nil
}

// markerMatches reports whether the installed marker already advertises the
// current hook set and bridge version. A version bump or a hook addition must
// rewrite the marker without requiring --force.
func markerMatches(marker hookMarker) bool {
	if marker.Version != 1 || marker.Agent != agentName || marker.Bridge != bridgePackage {
		return false
	}
	if len(marker.HookNames) != len(hookNames) {
		return false
	}
	for i := range hookNames {
		if marker.HookNames[i] != hookNames[i] {
			return false
		}
	}
	return true
}

// UninstallHooks removes the activation marker. A missing marker is success.
func (a *Agent) UninstallHooks() error {
	if err := os.Remove(markerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove DSH hook marker: %w", err)
	}
	return nil
}

// AreHooksInstalled reports whether the marker exists. A corrupt marker is
// still treated as installed: the state is unknown and must not be reported as
// "definitely not installed".
func (a *Agent) AreHooksInstalled() bool {
	_, err := os.Stat(markerPath())
	if err == nil {
		return true
	}
	// Unknown state (permissions, I/O) must not read as "no hooks": the
	// protocol requires disable --uninstall to hand the user a manual command
	// rather than claim Entire was fully removed.
	return !errors.Is(err, fs.ErrNotExist)
}

// ParseHook maps one DSH bridge payload to an Entire lifecycle event.
func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if len(strings.TrimSpace(string(input))) == 0 {
		return nil, nil
	}
	var payload protocol.HookInputJSON
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("parse DSH hook payload: %w", err)
	}

	hook := normalizeHookName(hookName)
	if hook == "" {
		hook = normalizeHookName(payload.HookType)
	}
	eventType, ok := eventTypeForHook(hook)
	if !ok {
		return nil, nil
	}

	sessionID := a.GetSessionID(&payload)
	if sessionID == "" {
		return nil, nil
	}

	repoRoot := protocol.RepoRoot()
	dir, err := a.GetSessionDir(repoRoot)
	if err != nil {
		return nil, err
	}
	ref := payload.SessionRef
	if ref == "" {
		ref = a.ResolveSessionFile(dir, sessionID)
	}

	opts := materializeOptions{}
	cwdHint := rawString(payload.RawData, "cwd")
	switch eventType {
	case 3:
		if turn, ok := rawInt(payload.RawData, "turn"); ok {
			opts.WaitTurn = &turn
		}
	case 4:
		opts.WaitCompaction = true
	case 5:
		opts.WaitStable = true
	}
	if _, _, materializeErr := a.materializeTranscript(sessionID, repoRoot, cwdHint, opts); materializeErr == nil {
		ref = a.ResolveSessionFile(dir, sessionID)
	}

	event := &protocol.EventJSON{
		Type:       eventType,
		SessionID:  sessionID,
		SessionRef: ref,
		Timestamp:  normalizeTimestamp(payload.Timestamp),
	}
	event.Prompt = promptFromPayload(&payload)
	event.Model = rawString(payload.RawData, "model")
	if eventType == 6 || eventType == 7 {
		event.SubagentID = rawString(payload.RawData, "subagent_id")
		if event.SubagentID == "" {
			event.SubagentID = rawString(payload.RawData, "subagentId")
		}
		event.SubagentType = rawString(payload.RawData, "subagent_type")
		event.TaskDescription = rawString(payload.RawData, "task_description")
	}
	return event, nil
}

func normalizeHookName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

func eventTypeForHook(hook string) (int, bool) {
	switch hook {
	case "session-start", "agent-spawn":
		return 1, true
	case "turn-start", "user-prompt-submit", "userpromptsubmit":
		return 2, true
	case "turn-end", "stop":
		return 3, true
	case HookCompaction:
		return 4, true
	case HookSessionEnd:
		return 5, true
	case HookSubagentStart:
		return 6, true
	case HookSubagentEnd:
		return 7, true
	default:
		return 0, false
	}
}

func promptFromPayload(payload *protocol.HookInputJSON) string {
	if payload.UserPrompt != "" {
		return payload.UserPrompt
	}
	if prompt := rawString(payload.RawData, "prompt"); prompt != "" {
		return prompt
	}
	return rawString(payload.RawData, "user_prompt")
}

func normalizeTimestamp(value string) string {
	if value != "" {
		// The DSH bridge uses Date.toISOString(), which includes milliseconds,
		// while Entire parses Event.Timestamp with time.RFC3339. Parse the
		// superset and emit the second-precision form the CLI accepts.
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func dshRawData(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	if nested, ok := raw["dsh"].(map[string]any); ok {
		return nested
	}
	return nil
}

func rawString(raw map[string]any, key string) string {
	for _, source := range []map[string]any{raw, dshRawData(raw)} {
		if source == nil {
			continue
		}
		if value, ok := source[key].(string); ok {
			return value
		}
	}
	return ""
}

func rawInt(raw map[string]any, key string) (int, bool) {
	for _, source := range []map[string]any{raw, dshRawData(raw)} {
		if source == nil {
			continue
		}
		switch value := source[key].(type) {
		case float64:
			if value == float64(int(value)) {
				return int(value), true
			}
		case int:
			return value, true
		case json.Number:
			if parsed, err := strconv.Atoi(value.String()); err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}
