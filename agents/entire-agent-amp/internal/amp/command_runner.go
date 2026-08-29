package amp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CommandRunner defines the interface for running `amp` commands, allowing for easier testing and abstraction.
type CommandRunner interface {
	// ExportThread runs `amp threads export <threadID>` and returns the raw
	// export document. It deliberately does not write to disk: the native
	// single-JSON export is an ingestion format only, converted to JSONL by
	// Agent.exportThread before anything is stored (see session_jsonl.go).
	ExportThread(ctx context.Context, threadID string) ([]byte, error)
}

// DefaultCommandRunner is the default implementation of CommandRunner that actually runs the `amp` commands.
type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) ExportThread(ctx context.Context, threadID string) ([]byte, error) {
	if strings.TrimSpace(threadID) == "" {
		return nil, errors.New("thread ID is required")
	}

	cmd := exec.CommandContext(ctx, "amp", "threads", "export", threadID)
	cmd.Env = append(ampEnv(), "PLUGINS=all")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return nil, fmt.Errorf("amp threads export %s: %w: %s", threadID, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("amp threads export %s: %w", threadID, err)
	}
	return out, nil
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
