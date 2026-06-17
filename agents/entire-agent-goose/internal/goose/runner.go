package goose

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const exportTimeout = 30 * time.Second

// CommandRunner abstracts the goose CLI invocations so tests can stub them.
type CommandRunner interface {
	// ExportSession runs `goose session export --session-id <id> --format json`
	// and writes the result to outPath.
	ExportSession(ctx context.Context, sessionID, outPath string) error
}

type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) ExportSession(ctx context.Context, sessionID, outPath string) error {
	cmd := exec.CommandContext(ctx, "goose", "session", "export",
		"--session-id", sessionID, "--format", "json", "-o", outPath)
	// Goose resolves its data dir from the environment; pass it through so
	// GOOSE_PATH_ROOT-based test sandboxes work.
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("goose session export %s: %w: %s", sessionID, err, string(out))
	}
	return nil
}
