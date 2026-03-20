//go:build e2e

package e2e

import (
	"context"
	"os"
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

		runner := &AgentRunner{
			BinaryPath: binPath,
			Env: []string{
				"ENTIRE_REPO_ROOT=" + s.Dir,
				"HOME=" + os.Getenv("HOME"),
				"PATH=" + os.Getenv("PATH"),
				"LANG=en_US.UTF-8",
			},
		}

		var resp struct {
			Installed bool `json:"installed"`
		}
		runner.RunJSON(t, &resp, "", "are-hooks-installed")

		assert.True(t, resp.Installed, "hooks should be installed after entire enable")
	})
}

// TestLifecycle_RewindPreCommit verifies the rewind flow before a commit:
// create file A → commit → snapshot rewind points → create file B → rewind → file A exists, file B gone.
func TestLifecycle_RewindPreCommit(t *testing.T) {
	testutil.ForEachAgent(t, 3*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		_, err := s.RunPrompt(t, ctx, "Create a file called alpha.txt with content 'alpha'. Do not ask for confirmation.")
		require.NoError(t, err, "first prompt failed")
		testutil.AssertFileExists(t, s.Dir, "alpha.txt")

		s.Git(t, "add", ".")
		s.Git(t, "commit", "-m", "add alpha")
		testutil.WaitForCheckpoint(t, s, 30*time.Second)

		points := entire.RewindList(t, s.Dir)
		require.NotEmpty(t, points, "expected at least one rewind point after first commit")
		rewindTarget := points[0].ID

		_, err = s.RunPrompt(t, ctx, "Create a file called beta.txt with content 'beta'. Do not ask for confirmation.")
		require.NoError(t, err, "second prompt failed")
		testutil.AssertFileExists(t, s.Dir, "beta.txt")

		err = entire.Rewind(t, s.Dir, rewindTarget)
		require.NoError(t, err, "rewind failed")

		_, err = os.Stat(filepath.Join(s.Dir, "alpha.txt"))
		assert.NoError(t, err, "alpha.txt should exist after rewind")
		_, err = os.Stat(filepath.Join(s.Dir, "beta.txt"))
		assert.True(t, os.IsNotExist(err), "beta.txt should not exist after rewind")
	})
}

// TestLifecycle_RewindAfterCommit verifies that rewind points are available
// after a git commit and that rewinding restores the correct state.
func TestLifecycle_RewindAfterCommit(t *testing.T) {
	testutil.ForEachAgent(t, 4*time.Minute, func(t *testing.T, s *testutil.RepoState, ctx context.Context) {
		_, err := s.RunPrompt(t, ctx, "Create a file called first.txt with content 'first'. Do not ask for confirmation.")
		require.NoError(t, err, "first prompt failed")
		s.Git(t, "add", ".")
		s.Git(t, "commit", "-m", "add first.txt")
		testutil.WaitForCheckpoint(t, s, 30*time.Second)

		pointsAfterFirst := entire.RewindList(t, s.Dir)
		require.NotEmpty(t, pointsAfterFirst, "expected rewind points after first commit")

		_, err = s.RunPrompt(t, ctx, "Create a file called second.txt with content 'second'. Do not ask for confirmation.")
		require.NoError(t, err, "second prompt failed")
		s.Git(t, "add", ".")
		s.Git(t, "commit", "-m", "add second.txt")
		testutil.WaitForCheckpoint(t, s, 30*time.Second)

		pointsAfterSecond := entire.RewindList(t, s.Dir)
		assert.Greater(t, len(pointsAfterSecond), len(pointsAfterFirst),
			"expected more rewind points after second commit")

		err = entire.Rewind(t, s.Dir, pointsAfterFirst[0].ID)
		require.NoError(t, err, "rewind failed")

		_, err = os.Stat(filepath.Join(s.Dir, "first.txt"))
		assert.NoError(t, err, "first.txt should exist after rewind")
		_, err = os.Stat(filepath.Join(s.Dir, "second.txt"))
		assert.True(t, os.IsNotExist(err), "second.txt should not exist after rewind")
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
