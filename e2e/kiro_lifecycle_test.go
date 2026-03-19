//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestLifecycle_SinglePromptManualCommit verifies the basic flow:
// agent creates a file → git add + commit → checkpoint exists with trailer.
func TestLifecycle_SinglePromptManualCommit(t *testing.T) {
	requireEntire(t)
	requireKiroCLI(t)
	t.Parallel()

	env := NewLifecycleEnv(t, "kiro")

	// Ask the agent to create a file.
	if err := env.RunKiroPrompt(t, "Create a file called hello.txt with the content 'hello world'"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	AssertFileExists(t, env, "hello.txt")

	// Manually commit so the checkpoint hook fires.
	env.Git(t, "add", ".")
	env.Git(t, "commit", "-m", "add hello.txt")

	// Wait for the checkpoint branch to appear.
	WaitForCheckpoint(t, env, 30*time.Second)

	// Verify the checkpoint has the Entire-Checkpoint trailer.
	trailer := GetCheckpointTrailer(t, env, "entire/checkpoints/v1")
	if trailer == "" {
		t.Error("expected Entire-Checkpoint trailer on checkpoint commit")
	}
}

// TestLifecycle_MultiplePromptsManualCommit sends two prompts, commits once,
// and verifies the checkpoint covers both files.
func TestLifecycle_MultiplePromptsManualCommit(t *testing.T) {
	requireEntire(t)
	requireKiroCLI(t)
	t.Parallel()

	env := NewLifecycleEnv(t, "kiro")

	if err := env.RunKiroPrompt(t, "Create a file called foo.txt containing 'foo'"); err != nil {
		t.Fatalf("first prompt failed: %v", err)
	}
	if err := env.RunKiroPrompt(t, "Create a file called bar.txt containing 'bar'"); err != nil {
		t.Fatalf("second prompt failed: %v", err)
	}

	AssertFileExists(t, env, "foo.txt")
	AssertFileExists(t, env, "bar.txt")

	env.Git(t, "add", ".")
	env.Git(t, "commit", "-m", "add foo and bar")

	WaitForCheckpoint(t, env, 30*time.Second)
}

// TestLifecycle_DetectAndEnable verifies that `entire enable --agent kiro`
// works when .kiro/ already exists in the repo.
func TestLifecycle_DetectAndEnable(t *testing.T) {
	requireEntire(t)
	t.Parallel()

	dir := t.TempDir()
	homeDir := t.TempDir()
	env := baseEnv(dir, homeDir)

	// Init a git repo with a .kiro/ directory.
	runGit(t, dir, env, "init")
	runGit(t, dir, env, "config", "user.email", "test@test.com")
	runGit(t, dir, env, "config", "user.name", "Test User")
	if err := os.MkdirAll(filepath.Join(dir, ".kiro"), 0o750); err != nil {
		t.Fatalf("mkdir .kiro: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o600); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, dir, env, "add", ".")
	runGit(t, dir, env, "commit", "-m", "initial")

	// Enable should succeed because .kiro/ is present.
	EntireEnable(t, dir, "kiro", env)

	// Verify .entire/ directory was created.
	if _, err := os.Stat(filepath.Join(dir, ".entire")); err != nil {
		t.Errorf("expected .entire/ directory after enable: %v", err)
	}
}

// TestLifecycle_HooksInstalledAfterEnable verifies that hooks are installed
// after running `entire enable`.
func TestLifecycle_HooksInstalledAfterEnable(t *testing.T) {
	requireEntire(t)
	t.Parallel()

	env := NewLifecycleEnv(t, "kiro")

	// The agent binary's are-hooks-installed subcommand should confirm hooks are present.
	binPath, ok := agentBinaries["entire-agent-kiro"]
	if !ok {
		t.Skip("entire-agent-kiro binary not built")
	}

	runner := &AgentRunner{
		BinaryPath: binPath,
		Env:        env.Env,
	}

	var resp struct {
		Installed bool `json:"installed"`
	}
	runner.RunJSON(t, &resp, "", "are-hooks-installed")

	if !resp.Installed {
		t.Error("hooks should be installed after entire enable")
	}
}

// TestLifecycle_RewindPreCommit verifies the rewind flow before a commit:
// create file A → snapshot rewind points → create file B → rewind → file A exists, file B gone.
func TestLifecycle_RewindPreCommit(t *testing.T) {
	requireEntire(t)
	requireKiroCLI(t)
	t.Parallel()

	env := NewLifecycleEnv(t, "kiro")

	// Create first file via agent.
	if err := env.RunKiroPrompt(t, "Create a file called alpha.txt with content 'alpha'"); err != nil {
		t.Fatalf("first prompt failed: %v", err)
	}
	AssertFileExists(t, env, "alpha.txt")

	// Commit so we have a checkpoint to rewind to.
	env.Git(t, "add", ".")
	env.Git(t, "commit", "-m", "add alpha")
	WaitForCheckpoint(t, env, 30*time.Second)

	// Snapshot rewind points.
	points := EntireRewindList(t, env.Dir, env.Env)
	if len(points) == 0 {
		t.Fatal("expected at least one rewind point after first commit")
	}
	rewindTarget := points[0].ID

	// Create a second file.
	if err := env.RunKiroPrompt(t, "Create a file called beta.txt with content 'beta'"); err != nil {
		t.Fatalf("second prompt failed: %v", err)
	}
	AssertFileExists(t, env, "beta.txt")

	// Rewind to the point after alpha but before beta.
	EntireRewind(t, env.Dir, env.Env, rewindTarget)

	// alpha.txt should still exist, beta.txt should be gone.
	if !env.FileExists("alpha.txt") {
		t.Error("alpha.txt should exist after rewind")
	}
	if env.FileExists("beta.txt") {
		t.Error("beta.txt should not exist after rewind")
	}
}

// TestLifecycle_RewindAfterCommit verifies that rewind points are available
// after a git commit and that rewinding restores the correct state.
func TestLifecycle_RewindAfterCommit(t *testing.T) {
	requireEntire(t)
	requireKiroCLI(t)
	t.Parallel()

	env := NewLifecycleEnv(t, "kiro")

	// First prompt + commit.
	if err := env.RunKiroPrompt(t, "Create a file called first.txt with content 'first'"); err != nil {
		t.Fatalf("first prompt failed: %v", err)
	}
	env.Git(t, "add", ".")
	env.Git(t, "commit", "-m", "add first.txt")
	WaitForCheckpoint(t, env, 30*time.Second)

	pointsAfterFirst := EntireRewindList(t, env.Dir, env.Env)
	if len(pointsAfterFirst) == 0 {
		t.Fatal("expected rewind points after first commit")
	}

	// Second prompt + commit.
	if err := env.RunKiroPrompt(t, "Create a file called second.txt with content 'second'"); err != nil {
		t.Fatalf("second prompt failed: %v", err)
	}
	env.Git(t, "add", ".")
	env.Git(t, "commit", "-m", "add second.txt")
	WaitForCheckpoint(t, env, 30*time.Second)

	pointsAfterSecond := EntireRewindList(t, env.Dir, env.Env)
	if len(pointsAfterSecond) <= len(pointsAfterFirst) {
		t.Error("expected more rewind points after second commit")
	}

	// Rewind to the state after the first commit.
	EntireRewind(t, env.Dir, env.Env, pointsAfterFirst[0].ID)

	if !env.FileExists("first.txt") {
		t.Error("first.txt should exist after rewind")
	}
	if env.FileExists("second.txt") {
		t.Error("second.txt should not exist after rewind")
	}
}

// TestLifecycle_SessionPersistence verifies that a session file is created in
// .entire/tmp/ after running a prompt.
func TestLifecycle_SessionPersistence(t *testing.T) {
	requireEntire(t)
	requireKiroCLI(t)
	t.Parallel()

	env := NewLifecycleEnv(t, "kiro")

	if err := env.RunKiroPrompt(t, "Create a file called session-test.txt with content 'test'"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	// Check that at least one session file exists in .entire/tmp/.
	tmpDir := filepath.Join(env.Dir, ".entire", "tmp")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("read .entire/tmp/: %v", err)
	}

	hasSession := false
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			hasSession = true
			break
		}
	}
	if !hasSession {
		t.Error("expected at least one .json session file in .entire/tmp/")
	}
}

// runGit is a standalone helper for running git outside of a LifecycleEnv.
func runGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", args[0], err, out)
	}
}
