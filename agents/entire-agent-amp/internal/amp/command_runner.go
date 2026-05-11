package amp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CommandRunner defines the interface for running `amp` commands, allowing for easier testing and abstraction.
type CommandRunner interface {
	// ExportThread runs the `amp threads export <threadID>` command for the given thread ID, stores the output at the specified outputPath and returns the path to the exported transcript file.
	ExportThread(ctx context.Context, threadID string, outputPath string) (string, error)
}

// DefaultCommandRunner is the default implementation of CommandRunner that actually runs the `amp` commands.
type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) ExportThread(ctx context.Context, threadID string, outputPath string) (string, error) {
	if strings.TrimSpace(threadID) == "" {
		return "", errors.New("thread ID is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return "", errors.New("output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "amp", "threads", "export", threadID)
	cmd.Env = append(ampEnv(), "PLUGINS=all")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", fmt.Errorf("amp threads export %s: %w: %s", threadID, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("amp threads export %s: %w", threadID, err)
	}
	if err := os.WriteFile(outputPath, out, 0o600); err != nil {
		return "", fmt.Errorf("write exported transcript: %w", err)
	}

	return outputPath, nil
}

// ampEnv returns the parent environment with Bun-specific variables stripped.
//
// The `amp` CLI is distributed as a Bun-compiled native binary. When invoked
// from inside an Amp plugin (which itself runs on Bun), variables like
// `BUN_BE_BUN` leak into the child process and make the compiled binary
// re-interpret its argv as a Bun script invocation. The visible symptom is
// `amp threads export <id>` failing with `error: Script not found "threads"`.
// Removing the Bun bootstrap variables forces `amp` to execute its compiled
// entrypoint instead.
func ampEnv() []string {
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
