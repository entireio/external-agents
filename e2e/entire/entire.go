package entire

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// BinPath returns the path to the entire binary.
// It checks E2E_ENTIRE_BIN first, then falls back to looking in PATH.
func BinPath() string {
	if p := os.Getenv("E2E_ENTIRE_BIN"); p != "" {
		return p
	}
	if p, err := exec.LookPath("entire"); err == nil {
		return p
	}
	return "entire"
}

// RewindPoint represents a single entry from `entire rewind --list`.
type RewindPoint struct {
	ID             string `json:"id"`
	Message        string `json:"message"`
	MetadataDir    string `json:"metadata_dir"`
	Date           string `json:"date"`
	IsLogsOnly     bool   `json:"is_logs_only"`
	CondensationID string `json:"condensation_id"`
	SessionID      string `json:"session_id"`
}

// Enable runs `entire enable` for the given agent with telemetry disabled.
func Enable(t *testing.T, dir, agent string) {
	t.Helper()
	run(t, dir, "enable", "--agent", agent, "--telemetry=false")
}

// Disable runs `entire disable` in the given directory.
func Disable(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "disable")
}

// RewindList runs `entire rewind --list` and parses the JSON output.
func RewindList(t *testing.T, dir string) []RewindPoint {
	t.Helper()
	out := run(t, dir, "rewind", "--list")

	var points []RewindPoint
	if err := json.Unmarshal([]byte(out), &points); err != nil {
		t.Fatalf("parse rewind list: %v\nraw output: %s", err, out)
	}
	return points
}

// Rewind runs `entire rewind --to <id>`. Returns an error instead of
// failing the test, since callers may test failure cases.
func Rewind(t *testing.T, dir, id string) error {
	t.Helper()
	return runErr(dir, "rewind", "--to", id)
}

// RewindLogsOnly runs `entire rewind --to <id> --logs-only`.
func RewindLogsOnly(t *testing.T, dir, id string) error {
	t.Helper()
	return runErr(dir, "rewind", "--to", id, "--logs-only")
}

// run executes an `entire` subcommand in dir and fails the test on error.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(BinPath(), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ENTIRE_TEST_TTY=0")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("entire %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// runErr executes an `entire` subcommand in dir and returns any error.
func runErr(dir string, args ...string) error {
	cmd := exec.Command(BinPath(), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ENTIRE_TEST_TTY=0")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return &ExecError{
			Args:   args,
			Err:    err,
			Output: string(out),
		}
	}
	return nil
}

// ExecError wraps an entire CLI execution failure with its output.
type ExecError struct {
	Args   []string
	Err    error
	Output string
}

func (e *ExecError) Error() string {
	return "entire " + strings.Join(e.Args, " ") + ": " + e.Err.Error() + "\n" + e.Output
}

func (e *ExecError) Unwrap() error {
	return e.Err
}
