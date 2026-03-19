//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// RewindPoint represents a single entry from `entire rewind --list`.
type RewindPoint struct {
	ID              string `json:"id"`
	Message         string `json:"message"`
	IsLogsOnly      bool   `json:"is_logs_only"`
	CondensationID  string `json:"condensation_id"`
}

// EntireBinPath returns the path to the entire binary.
// It checks E2E_ENTIRE_BIN first, then falls back to looking in PATH.
func EntireBinPath() string {
	if p := os.Getenv("E2E_ENTIRE_BIN"); p != "" {
		return p
	}
	return "entire"
}

// EntireAvailable checks whether the entire binary can be found.
func EntireAvailable() bool {
	_, err := exec.LookPath(EntireBinPath())
	return err == nil
}

// EntireEnable runs `entire enable --agent <agent> --telemetry=false` in dir.
func EntireEnable(t *testing.T, dir string, agent string, env []string) {
	t.Helper()
	out := EntireRun(t, dir, env, "enable", "--agent", agent, "--telemetry=false")
	t.Logf("entire enable: %s", out)
}

// EntireDisable runs `entire disable` in dir.
func EntireDisable(t *testing.T, dir string, env []string) {
	t.Helper()
	out := EntireRun(t, dir, env, "disable")
	t.Logf("entire disable: %s", out)
}

// EntireRewindList parses the JSON output of `entire rewind --list`.
func EntireRewindList(t *testing.T, dir string, env []string) []RewindPoint {
	t.Helper()
	out := EntireRun(t, dir, env, "rewind", "--list")

	var points []RewindPoint
	if err := json.Unmarshal([]byte(out), &points); err != nil {
		t.Fatalf("parse rewind --list JSON: %v\noutput: %s", err, out)
	}
	return points
}

// EntireRewind runs `entire rewind --to <id>`.
func EntireRewind(t *testing.T, dir string, env []string, id string) {
	t.Helper()
	out := EntireRun(t, dir, env, "rewind", "--to", id)
	t.Logf("entire rewind --to %s: %s", id, out)
}

// EntireRun executes the entire binary with the given args in dir. It fails
// the test on non-zero exit.
func EntireRun(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	out, err := EntireRunErr(dir, env, args...)
	if err != nil {
		t.Fatalf("entire %s failed: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return out
}

// EntireRunErr executes the entire binary and returns stdout+stderr combined.
// It does not fail the test on error — useful for expected-failure cases.
func EntireRunErr(dir string, env []string, args ...string) (string, error) {
	bin := EntireBinPath()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w", bin, strings.Join(args, " "), err)
	}
	return string(out), nil
}
