package kilo

import (
	"context"
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

	cmd := exec.CommandContext(ctx, "kilo", "session", "show", "--format", "json", sessionID)
	cmd.Env = kiloEnv()
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("kilo session show %s: %w: %s", sessionID, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("kilo session show %s: %w", sessionID, err)
	}
	return out, nil
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
