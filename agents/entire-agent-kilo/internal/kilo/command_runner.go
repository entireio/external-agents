package kilo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CommandRunner runs `kilo` CLI commands for transcript refresh. The default
// implementation shells out to the binary. Tests substitute their own runner.
type CommandRunner interface {
	// ExportSession fetches the Kilo session JSON for sessionID and writes it
	// to outputPath. Returns the output path on success.
	ExportSession(ctx context.Context, sessionID string, outputPath string) (string, error)
}

type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) ExportSession(ctx context.Context, sessionID string, outputPath string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("session id is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return "", errors.New("output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "kilo", "session", "show", "--format", "json", sessionID)
	cmd.Env = kiloEnv()
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("kilo session show %s: %w: %s", sessionID, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("kilo session show %s: %w", sessionID, err)
	}
	if err := os.WriteFile(outputPath, out, 0o600); err != nil {
		return "", fmt.Errorf("write exported session: %w", err)
	}

	return outputPath, nil
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
