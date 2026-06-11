package goose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entireio/external-agents/agents/entire-agent-goose/internal/protocol"
)

// Plugin layout: goose discovers project-scope plugins from
// <project-root>/.agents/plugins/ (enabled by default once present) and runs
// hooks declared in <plugin>/hooks/hooks.json. See AGENT.md.
const (
	pluginRelDir   = ".agents/plugins/entire"
	hooksRelFile   = ".agents/plugins/entire/hooks/hooks.json"
	hookCommand    = "entire hooks goose "
	hookTimeoutSec = 60
)

// gooseHookPayload is the HookContext JSON goose writes to hook stdin
// (snake_case, verified against goose 1.37.0 captures).
type gooseHookPayload struct {
	Event          string          `json:"event"`
	SessionID      string          `json:"session_id"`
	MatcherContext string          `json:"matcher_context"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolInput      json.RawMessage `json:"tool_input,omitempty"`
	Message        string          `json:"message,omitempty"`
	WorkingDir     string          `json:"working_dir,omitempty"`
}

// nativeHookEvents maps Entire hook verbs to goose hooks.json event names.
// Order is stable for deterministic hooks.json output.
var nativeHookEvents = []struct {
	Verb  string
	Event string
}{
	{HookNameSessionStart, "SessionStart"},
	{HookNameUserPromptSubmit, "UserPromptSubmit"},
	{HookNameStop, "Stop"},
	{HookNameSessionEnd, "SessionEnd"},
}

// ParseHook converts a raw goose hook payload into a normalized Entire event.
// Goose payloads carry no timestamp or transcript reference, so events are
// stamped at receipt and the transcript path is derived from the session ID.
func (a *Agent) ParseHook(hookName string, input []byte) (*protocol.EventJSON, error) {
	if len(bytes.TrimSpace(input)) == 0 {
		return nil, nil
	}

	var payload gooseHookPayload
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, fmt.Errorf("parse hook payload: %w", err)
	}
	if payload.SessionID == "" {
		return nil, nil
	}

	sessionID := payload.SessionID
	sessionRef := transcriptPath(sessionID)
	now := time.Now().UTC().Format(time.RFC3339)

	switch hookName {
	case HookNameSessionStart:
		// Best effort: the session row exists by SessionStart, but a failed
		// export must not block the session from starting.
		_ = a.exportSession(sessionID, sessionRef)
		return &protocol.EventJSON{
			Type:       1,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Model:      modelFromSessionRef(sessionRef),
			Timestamp:  now,
		}, nil

	case HookNameUserPromptSubmit:
		return &protocol.EventJSON{
			Type:       2,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Prompt:     payload.Message,
			Model:      modelFromSessionRef(sessionRef),
			Timestamp:  now,
		}, nil

	case HookNameStop:
		// Refresh the transcript for checkpointing, but best-effort: Entire
		// calls prepare-transcript before reading it, which re-exports.
		_ = a.exportSession(sessionID, sessionRef)
		return &protocol.EventJSON{
			Type:       3,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Model:      modelFromSessionRef(sessionRef),
			Timestamp:  now,
		}, nil

	case HookNameSessionEnd:
		_ = a.exportSession(sessionID, sessionRef)
		return &protocol.EventJSON{
			Type:       5,
			SessionID:  sessionID,
			SessionRef: sessionRef,
			Timestamp:  now,
		}, nil

	default:
		return nil, nil
	}
}

func (a *Agent) exportSession(sessionID, sessionRef string) error {
	if sessionID == "" || sessionRef == "" {
		return errors.New("session id and session ref are required")
	}
	if err := os.MkdirAll(filepath.Dir(sessionRef), 0o750); err != nil {
		return err
	}
	runner := a.CommandRunner
	if runner == nil {
		runner = &DefaultCommandRunner{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
	defer cancel()
	return runner.ExportSession(ctx, sessionID, sessionRef)
}

// hooksFileContent renders the goose plugin hooks.json forwarding native
// lifecycle events to the Entire CLI. Hook commands read the HookContext
// JSON from stdin and must exit 0; goose treats Stop as a blocking hook.
func hooksFileContent() ([]byte, error) {
	type hookAction struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	type hookRule struct {
		Hooks []hookAction `json:"hooks"`
	}
	rules := map[string][]hookRule{}
	for _, h := range nativeHookEvents {
		rules[h.Event] = []hookRule{{
			Hooks: []hookAction{{
				Type:    "command",
				Command: hookCommand + h.Verb,
				Timeout: hookTimeoutSec,
			}},
		}}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"hooks": rules}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	if !force && a.AreHooksInstalled() {
		return 0, nil
	}
	repoRoot := protocol.RepoRoot()
	hooksPath := filepath.Join(repoRoot, hooksRelFile)
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o750); err != nil {
		return 0, err
	}
	content, err := hooksFileContent()
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(hooksPath, content, 0o600); err != nil {
		return 0, err
	}
	return len(nativeHookEvents), nil
}

func (a *Agent) UninstallHooks() error {
	repoRoot := protocol.RepoRoot()
	hooksPath := filepath.Join(repoRoot, hooksRelFile)
	if err := os.Remove(hooksPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Remove the now-empty plugin directories; leave them if the user added
	// other files to the plugin.
	_ = os.Remove(filepath.Dir(hooksPath))
	_ = os.Remove(filepath.Join(repoRoot, pluginRelDir))
	return nil
}

func (a *Agent) AreHooksInstalled() bool {
	data, err := os.ReadFile(filepath.Join(protocol.RepoRoot(), hooksRelFile))
	if err != nil {
		return false
	}
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return false
	}
	for _, h := range nativeHookEvents {
		found := false
		for _, rule := range file.Hooks[h.Event] {
			for _, action := range rule.Hooks {
				if action.Command == hookCommand+h.Verb {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
