package kilo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CommandRunner runs `kilo` CLI commands for transcript refresh. The default
// implementation shells out to the binary. Tests substitute their own runner.
type CommandRunner interface {
	// ExportSession returns the Kilo session JSON for sessionID.
	ExportSession(ctx context.Context, sessionID string) ([]byte, error)
}

type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) ExportSession(ctx context.Context, sessionID string) ([]byte, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session id is required")
	}

	// Kilo's native session blob is spelled differently across versions:
	// `kilo export [id]` on 7.8.x, `kilo session show --format json <id>`
	// where that subcommand exists. An unknown subcommand prints help and
	// still exits 0, so the output is validated rather than trusted: only a
	// payload that actually parses as a session export is accepted.
	candidates := [][]string{
		{"export", sessionID},
		{"session", "show", "--format", "json", sessionID},
	}
	var failures []string
	for _, args := range candidates {
		cmd := exec.CommandContext(ctx, "kilo", args...)
		cmd.Env = kiloEnv()
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
				failures = append(failures, fmt.Sprintf("kilo %s: %s", strings.Join(args, " "),
					strings.TrimSpace(string(exitErr.Stderr))))
				continue
			}
			failures = append(failures, fmt.Sprintf("kilo %s: %v", strings.Join(args, " "), err))
			continue
		}
		if !isSessionExport(out) {
			failures = append(failures, fmt.Sprintf("kilo %s: output is not a session export (%d bytes)",
				strings.Join(args, " "), len(out)))
			continue
		}
		return out, nil
	}
	return nil, fmt.Errorf("cannot export kilo session %s: %s", sessionID, strings.Join(failures, "; "))
}

// isSessionExport reports whether raw looks like Kilo's native session export:
// a JSON object with a non-empty `messages` array. Anything else — help text,
// an error page, an empty object — is a failed attempt, not a session.
func isSessionExport(raw []byte) bool {
	var probe struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return len(probe.Messages) > 0
}

// kiloEnv returns the parent environment without Bun bootstrap variables.
//
// The `kilo` CLI is a Bun-compiled native binary distributed alongside Kilo's
// plugin runtime. When invoked from inside a Kilo plugin (which itself runs
// on Bun), variables like `BUN_BE_BUN` leak into the child process and make
// the compiled binary re-interpret its argv as a Bun script invocation.
// Removing the Bun bootstrap variables forces `kilo` to execute its compiled
// entrypoint instead.
func kiloEnv() []string {
	parent := os.Environ()
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		key, _, _ := strings.Cut(kv, "=")
		if isBunEnvVar(key) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isBunEnvVar(key string) bool {
	switch key {
	case "BUN_BE_BUN", "BUN_INTERNAL_IPC_FD", "BUN_INSPECT", "BUN_INSPECT_NOTIFY", "BUN_INSPECT_CONNECT_TO":
		return true
	}
	return strings.HasPrefix(key, "BUN_DEBUG_")
}
