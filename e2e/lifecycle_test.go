//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/entireio/external-agents/e2e/entire"
	"github.com/entireio/external-agents/e2e/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLifecycle_SinglePromptManualCommit verifies the basic flow:
// agent creates a file → git add + commit → checkpoint exists with trailer.
func TestLifecycle_SinglePromptManualCommit(t *testing.T) {
	testutil.ForEachAgent(t, 2*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		_, err := s.RunPrompt(t, ctx, "Create a file called hello.txt with the content 'hello world'. Do not ask for confirmation.")
		require.NoError(t, err, "prompt failed")

		testutil.AssertFileExists(t, s.Dir, "hello.txt")

		s.Git(t, "add", ".")
		s.Git(t, "commit", "-m", "add hello.txt")

		testutil.WaitForCheckpoint(t, s, 30*time.Second)

		cpID := testutil.AssertHasCheckpointTrailer(t, s.Dir, "HEAD")
		testutil.AssertCheckpointExists(t, s.Dir, cpID)
		testutil.ValidateCheckpointDeep(t, s.Dir, testutil.DeepCheckpointValidation{
			CheckpointID:              cpID,
			ExpectedTranscriptContent: []string{"hello"},
		})
	})
}

// TestLifecycle_MultiplePromptsManualCommit sends two prompts, commits once,
// and verifies the checkpoint covers both files.
func TestLifecycle_MultiplePromptsManualCommit(t *testing.T) {
	testutil.ForEachAgent(t, 3*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		_, err := s.RunPrompt(t, ctx, "Create a file called foo.txt containing 'foo'. Do not ask for confirmation.")
		require.NoError(t, err, "first prompt failed")

		_, err = s.RunPrompt(t, ctx, "Create a file called bar.txt containing 'bar'. Do not ask for confirmation.")
		require.NoError(t, err, "second prompt failed")

		testutil.AssertFileExists(t, s.Dir, "foo.txt")
		testutil.AssertFileExists(t, s.Dir, "bar.txt")

		s.Git(t, "add", ".")
		s.Git(t, "commit", "-m", "add foo and bar")

		testutil.WaitForCheckpoint(t, s, 30*time.Second)
		testutil.AssertCheckpointAdvanced(t, s)
	})
}

// TestLifecycle_DetectAndEnable verifies that `entire enable --agent kiro`
// works when .kiro/ already exists in the repo.
func TestLifecycle_DetectAndEnable(t *testing.T) {
	testutil.ForEachAgent(t, 1*time.Minute, func(t *testing.T, s *testutil.RepoState, _ context.Context) {
		// The repo is already set up with entire enable by ForEachAgent/SetupRepo.
		// Verify .entire/ directory exists.
		_, err := os.Stat(filepath.Join(s.Dir, ".entire"))
		require.NoError(t, err, "expected .entire/ directory after enable")
	})
}

// TestLifecycle_HooksInstalledAfterEnable verifies that hooks are installed
// after running `entire enable`.
func TestLifecycle_HooksInstalledAfterEnable(t *testing.T) {
	testutil.ForEachAgent(t, 1*time.Minute, func(t *testing.T, s *testutil.RepoState, _ context.Context) {
		agentBinName := "entire-agent-" + s.Agent.EntireAgent()
		binPath, ok := AgentBinaries[agentBinName]
		if !ok {
			t.Skipf("%s binary not built", agentBinName)
		}

		assert.True(t, hooksInstalled(t, binPath, s.Dir), "hooks should be installed after entire enable")
	})
}

// TestLifecycle_RewindPreCommit verifies pre-commit shadow branch rewind:
// agent creates file A → snapshot rewind points → agent creates file B → rewind → file A exists, file B gone.
// No commits happen — this tests pure shadow branch rewind points.
func TestLifecycle_RewindPreCommit(t *testing.T) {
	testutil.ForEachAgent(t, 3*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		// Agent creates file A (no commit).
		_, err := s.RunPrompt(t, ctx, "Create a file called alpha.txt with content 'alpha'. Do not commit the file. Do not ask for confirmation.")
		require.NoError(t, err, "first prompt failed")
		testutil.AssertFileExists(t, s.Dir, "alpha.txt")

		// Snapshot rewind points (shadow branch, is_logs_only=false).
		points := entire.RewindList(t, s.Dir)
		require.NotEmpty(t, points, "expected at least one rewind point after first prompt")
		rewindTarget := points[0].ID

		// Agent creates file B (no commit).
		_, err = s.RunPrompt(t, ctx, "Create a file called beta.txt with content 'beta'. Do not commit the file. Do not ask for confirmation.")
		require.NoError(t, err, "second prompt failed")
		testutil.AssertFileExists(t, s.Dir, "beta.txt")

		// Rewind to point after file A was created.
		err = entire.Rewind(t, s.Dir, rewindTarget)
		require.NoError(t, err, "rewind to %s should succeed", rewindTarget)

		// File A should still exist, file B should be gone.
		_, err = os.Stat(filepath.Join(s.Dir, "alpha.txt"))
		assert.NoError(t, err, "alpha.txt should exist after rewind")
		_, err = os.Stat(filepath.Join(s.Dir, "beta.txt"))
		assert.True(t, os.IsNotExist(err), "beta.txt should not exist after rewind")
	})
}

// TestLifecycle_RewindAfterCommit verifies that pre-commit shadow branch
// rewind points become invalid after a user commit. Rewinding to an old
// shadow branch ID should fail because condensation converts them to logs-only.
func TestLifecycle_RewindAfterCommit(t *testing.T) {
	testutil.ForEachAgent(t, 3*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		// Agent creates a file (no commit).
		_, err := s.RunPrompt(t, ctx, "Create a file called first.txt with content 'first'. Do not commit the file. Do not ask for confirmation.")
		require.NoError(t, err, "prompt failed")
		testutil.AssertFileExists(t, s.Dir, "first.txt")

		// Get pre-commit rewind points and find a non-logs-only shadow point.
		pointsBefore := entire.RewindList(t, s.Dir)
		require.NotEmpty(t, pointsBefore, "expected rewind points before commit")

		var shadowPoint *entire.RewindPoint
		for i := range pointsBefore {
			if !pointsBefore[i].IsLogsOnly {
				shadowPoint = &pointsBefore[i]
				break
			}
		}
		require.NotNil(t, shadowPoint, "expected at least one non-logs-only shadow branch rewind point")
		oldID := shadowPoint.ID

		// User commits the file — triggers condensation.
		s.Git(t, "add", ".")
		s.Git(t, "commit", "-m", "add first.txt")
		testutil.WaitForCheckpoint(t, s, 30*time.Second)

		// Wait for condensation to convert old shadow point to logs-only.
		// Condensation runs asynchronously after checkpoint advancement,
		// so we poll the rewind list instead of checking once.
		deadline := time.Now().Add(30 * time.Second)
		condensed := false
		for time.Now().Before(deadline) {
			pointsAfter := entire.RewindList(t, s.Dir)
			found := false
			for _, p := range pointsAfter {
				if p.ID == oldID && !p.IsLogsOnly {
					found = true
					break
				}
			}
			if !found {
				condensed = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		assert.True(t, condensed, "old shadow branch rewind point %s should no longer be listed as non-logs-only within 30s", oldID)

		// Attempting to rewind to the old shadow branch ID should fail.
		err = entire.Rewind(t, s.Dir, oldID)
		assert.Error(t, err, "rewind to old shadow branch ID should fail after commit")

		// Working directory should be unchanged — file still committed.
		testutil.AssertFileExists(t, s.Dir, "first.txt")
		testutil.AssertNoShadowBranches(t, s.Dir)
	})
}

// TestLifecycle_SessionPersistence verifies that a session file is created in
// .entire/tmp/ after running a prompt.
func TestLifecycle_SessionPersistence(t *testing.T) {
	testutil.ForEachAgent(t, 2*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		_, err := s.RunPrompt(t, ctx, "Create a file called session-test.txt with content 'test'. Do not ask for confirmation.")
		require.NoError(t, err, "prompt failed")

		tmpDir := filepath.Join(s.Dir, ".entire", "tmp")
		entries, err := os.ReadDir(tmpDir)
		require.NoError(t, err, "read .entire/tmp/")

		hasSession := false
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".json" {
				hasSession = true
				break
			}
		}
		assert.True(t, hasSession, "expected at least one .json session file in .entire/tmp/")
	})
}

// TestLifecycle_InteractiveSession verifies that an interactive tmux session
// can send prompts and receive responses.
func TestLifecycle_InteractiveSession(t *testing.T) {
	testutil.ForEachAgent(t, 3*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		session := s.StartSession(t, ctx)
		if session == nil {
			t.Skip("agent does not support interactive sessions")
		}

		s.Send(t, session, "Create a file called interactive.txt with 'hello'. Do not ask for confirmation.")
		s.WaitFor(t, session, s.Agent.PromptPattern(), 60*time.Second)

		testutil.WaitForFileExists(t, s.Dir, "interactive.txt", 10*time.Second)

		s.Git(t, "add", ".")
		s.Git(t, "commit", "-m", "interactive test")
		testutil.WaitForCheckpoint(t, s, 30*time.Second)
	})
}

func hooksInstalled(t *testing.T, binPath, repoRoot string) bool {
	t.Helper()

	cmd := exec.Command(binPath, "are-hooks-installed")
	cmd.Env = append(os.Environ(),
		"ENTIRE_REPO_ROOT="+repoRoot,
		"LANG=en_US.UTF-8",
	)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("%s are-hooks-installed failed: %v\nstdout: %s\nstderr: %s", binPath, err, out, exitErr.Stderr)
		}
		t.Fatalf("%s are-hooks-installed failed: %v\nstdout: %s", binPath, err, out)
	}

	var resp struct {
		Installed bool `json:"installed"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("parse are-hooks-installed response: %v\nraw output: %s", err, out)
	}

	return resp.Installed
}
