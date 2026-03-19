//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// LifecycleEnv provides a full-lifecycle test environment: a git repo with
// `entire enable` already run, ready for agent prompts and checkpoint assertions.
type LifecycleEnv struct {
	Dir     string   // repo root (temp dir)
	HomeDir string   // isolated HOME
	Env     []string // environment variables for subprocesses
	Agent   string   // agent name (e.g. "kiro")
}

// NewLifecycleEnv creates a temp git repo, writes an initial commit, and runs
// `entire enable --agent <agentName>`. It also writes .entire/settings.json
// with external_agents enabled.
func NewLifecycleEnv(t *testing.T, agentName string) *LifecycleEnv {
	t.Helper()

	dir := t.TempDir()
	homeDir := t.TempDir()

	env := lifecycleEnv(dir, homeDir)

	le := &LifecycleEnv{
		Dir:     dir,
		HomeDir: homeDir,
		Env:     env,
		Agent:   agentName,
	}

	// Initialize git repo with a seed commit.
	le.Git(t, "init")
	le.Git(t, "config", "user.email", "test@test.com")
	le.Git(t, "config", "user.name", "Test User")
	le.WriteFile(t, "README.md", "# test repo\n")
	le.Git(t, "add", ".")
	le.Git(t, "commit", "-m", "initial commit")

	// Write .entire/settings.json with external_agents enabled.
	le.MkdirAll(t, ".entire")
	le.WriteFile(t, ".entire/settings.json", `{"external_agents":true}`)

	// Run entire enable.
	EntireEnable(t, dir, agentName, env)

	return le
}

// Git runs a git command in the repo dir; fails the test on error.
func (le *LifecycleEnv) Git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = le.Dir
	cmd.Env = le.Env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// GitOutput runs a git command and returns its combined output.
func (le *LifecycleEnv) GitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = le.Dir
	cmd.Env = le.Env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// RunKiroPrompt runs `kiro-cli-chat chat --no-interactive --trust-all-tools --agent entire <prompt>`.
func (le *LifecycleEnv) RunKiroPrompt(t *testing.T, prompt string) error {
	t.Helper()
	bin, err := exec.LookPath("kiro-cli-chat")
	if err != nil {
		t.Fatalf("kiro-cli-chat not in PATH: %v", err)
	}
	cmd := exec.Command(bin, "chat", "--no-interactive", "--trust-all-tools", "--agent", "entire", prompt)
	cmd.Dir = le.Dir
	cmd.Env = le.Env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("kiro-cli-chat output:\n%s", out)
		return fmt.Errorf("kiro-cli-chat: %w", err)
	}
	t.Logf("kiro-cli-chat output:\n%s", out)
	return nil
}

// FileExists checks whether relPath exists in the repo dir.
func (le *LifecycleEnv) FileExists(relPath string) bool {
	_, err := os.Stat(filepath.Join(le.Dir, relPath))
	return err == nil
}

// WriteFile writes content to relPath inside the repo dir.
func (le *LifecycleEnv) WriteFile(t *testing.T, relPath, content string) {
	t.Helper()
	abs := filepath.Join(le.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

// ReadFile reads a file from relPath inside the repo dir.
func (le *LifecycleEnv) ReadFile(t *testing.T, relPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(le.Dir, relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return string(data)
}

// MkdirAll creates a directory (and parents) relative to the repo root.
func (le *LifecycleEnv) MkdirAll(t *testing.T, relPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(le.Dir, relPath), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", relPath, err)
	}
}

// AbsPath returns the absolute path for a relative path in the repo.
func (le *LifecycleEnv) AbsPath(relPath string) string {
	return filepath.Join(le.Dir, relPath)
}

// WaitForCheckpoint polls for a checkpoint on the entire/checkpoints/v1 branch.
// It checks for new commits on that branch within the given timeout.
func WaitForCheckpoint(t *testing.T, le *LifecycleEnv, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("git", "rev-parse", "--verify", "entire/checkpoints/v1")
		cmd.Dir = le.Dir
		cmd.Env = le.Env
		if err := cmd.Run(); err == nil {
			t.Log("checkpoint branch found")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("timed out waiting for checkpoint branch entire/checkpoints/v1")
}

// GetCheckpointTrailer extracts the Entire-Checkpoint trailer from a commit ref.
func GetCheckpointTrailer(t *testing.T, le *LifecycleEnv, ref string) string {
	t.Helper()
	out := le.GitOutput(t, "log", "-1", "--format=%B", ref)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Entire-Checkpoint:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Entire-Checkpoint:"))
		}
	}
	return ""
}

// AssertFileExists fails the test if no file matches relPath in the repo.
func AssertFileExists(t *testing.T, le *LifecycleEnv, relPath string) {
	t.Helper()
	if !le.FileExists(relPath) {
		t.Errorf("expected file %s to exist", relPath)
	}
}

// requireEntire fails or skips the test if the entire binary is not available.
// When E2E_REQUIRE_LIFECYCLE=1 (set by `make test-e2e-lifecycle`), it fails
// instead of skipping so you know immediately that your environment is missing deps.
func requireEntire(t *testing.T) {
	t.Helper()
	if !EntireAvailable() {
		const msg = "entire binary not available (set E2E_ENTIRE_BIN or install entire)"
		if os.Getenv("E2E_REQUIRE_LIFECYCLE") == "1" {
			t.Fatal(msg)
		}
		t.Skip("skipping: " + msg)
	}
}

// requireKiroCLI fails or skips the test if kiro-cli-chat is not in PATH.
// When E2E_REQUIRE_LIFECYCLE=1, it fails instead of skipping.
func requireKiroCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("kiro-cli-chat"); err != nil {
		const msg = "kiro-cli-chat not in PATH"
		if os.Getenv("E2E_REQUIRE_LIFECYCLE") == "1" {
			t.Fatal(msg)
		}
		t.Skip("skipping: " + msg)
	}
}

// lifecycleEnv builds the environment variable slice for lifecycle tests.
// It includes the current PATH so that `entire` and `kiro-cli-chat` can be found.
func lifecycleEnv(repoRoot, homeDir string) []string {
	return []string{
		fmt.Sprintf("ENTIRE_REPO_ROOT=%s", repoRoot),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
		"LANG=en_US.UTF-8",
	}
}
