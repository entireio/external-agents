package amp

import (
	"context"
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
		return "", fmt.Errorf("thread ID is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return "", fmt.Errorf("output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "amp", "threads", "export", threadID)
	cmd.Env = append(os.Environ(), "PLUGINS=all")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("amp threads export %s: %w: %s", threadID, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("amp threads export %s: %w", threadID, err)
	}
	if err := os.WriteFile(outputPath, out, 0o600); err != nil {
		return "", fmt.Errorf("write exported transcript: %w", err)
	}

	return outputPath, nil
}
