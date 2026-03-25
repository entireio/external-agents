package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entireio/external-agents/e2e/agents"
	"github.com/entireio/external-agents/e2e/entire"
)

// RepoState holds the working state for a single test's cloned repository.
type RepoState struct {
	Agent            agents.Agent
	Dir              string
	ArtifactDir      string
	HeadBefore       string
	CheckpointBefore string
	ConsoleLog       *os.File
	session          agents.Session // interactive session, if started via StartSession
	skipArtifacts    bool           // suppresses artifact capture on scenario restart
}

// SetupRepo creates a fresh git repository in a temporary directory, seeds it
// with an initial commit, and runs `entire enable` for the given agent.
// Artifact capture is registered as a cleanup function.
func SetupRepo(t *testing.T, agent agents.Agent) *RepoState {
	t.Helper()

	keepRepos := os.Getenv("E2E_KEEP_REPOS") != ""

	dir, err := os.MkdirTemp("", "e2e-repo-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	if keepRepos {
		t.Logf("E2E_KEEP_REPOS: repo will be preserved at %s", dir)
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}

	// Resolve symlinks (macOS: /var -> /private/var) so paths match
	// what agent CLIs see when they resolve their own CWD.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}

	Git(t, dir, "init")
	Git(t, dir, "config", "user.name", "E2E Test")
	Git(t, dir, "config", "user.email", "e2e@test.local")
	Git(t, dir, "commit", "--allow-empty", "-m", "initial commit")

	// External agents need external_agents enabled in settings before enable.
	if ea, ok := agent.(agents.ExternalAgent); ok && ea.IsExternalAgent() {
		entireDir := filepath.Join(dir, ".entire")
		if err := os.MkdirAll(entireDir, 0o755); err != nil {
			t.Fatalf("create .entire for external agent: %v", err)
		}
		if err := os.WriteFile(filepath.Join(entireDir, "settings.json"),
			[]byte("{\"external_agents\": true}\n"), 0o644); err != nil {
			t.Fatalf("write external_agents setting: %v", err)
		}
	}

	entire.Enable(t, dir, agent.EntireAgent())
	PatchSettings(t, dir, map[string]any{"log_level": "debug"})

	// Create artifact dir eagerly so console.log is written to disk
	// incrementally. Even if the test is killed by a global timeout,
	// partial output survives.
	artDir := artifactDir(t)
	consoleLog, err := os.Create(filepath.Join(artDir, "console.log"))
	if err != nil {
		t.Fatalf("create console.log: %v", err)
	}

	state := &RepoState{
		Agent:            agent,
		Dir:              dir,
		ArtifactDir:      artDir,
		HeadBefore:       GitOutput(t, dir, "rev-parse", "HEAD"),
		CheckpointBefore: GitOutput(t, dir, "rev-parse", "entire/checkpoints/v1"),
		ConsoleLog:       consoleLog,
	}

	t.Cleanup(func() {
		_ = consoleLog.Close()
		if !state.skipArtifacts {
			CaptureArtifacts(t, state)
		}
	})

	return state
}

// ForEachAgent runs fn as a parallel subtest for every registered agent.
// It handles repo setup, concurrency gating, context timeout, and cleanup.
// The timeout is scaled by each agent's TimeoutMultiplier.
func ForEachAgent(t *testing.T, timeout time.Duration, fn func(t *testing.T, s *RepoState, ctx context.Context)) {
	t.Helper()
	t.Parallel()
	all := agents.All()
	if len(all) == 0 {
		t.Skip("no agents registered (check E2E_AGENT filter)")
	}
	for _, agent := range all {
		t.Run(agent.Name(), func(t *testing.T) {
			t.Parallel()
			slotCtx := context.Background()
			if deadline, ok := t.Deadline(); ok {
				var cancel context.CancelFunc
				slotCtx, cancel = context.WithDeadline(slotCtx, deadline)
				defer cancel()
			}
			if err := agents.AcquireSlot(slotCtx, agent); err != nil {
				t.Fatalf("timed out waiting for agent slot: %v", err)
			}
			defer agents.ReleaseSlot(agent)

			scaled := time.Duration(float64(timeout) * agent.TimeoutMultiplier())

			var prevState *RepoState
			for attempt := range maxScenarioRestarts + 1 {
				s := SetupRepo(t, agent)
				ctx, cancel := context.WithTimeout(context.Background(), scaled)

				if prevState != nil {
					prevState.skipArtifacts = true
				}

				restarted := runScenario(t, s, ctx, fn)
				cancel()

				if !restarted {
					return
				}
				prevState = s
				if attempt >= maxScenarioRestarts {
					t.Fatalf("exhausted %d scenario attempts due to transient API errors", maxScenarioRestarts+1)
				}
				t.Logf("transient error, restarting scenario (attempt %d/%d)", attempt+2, maxScenarioRestarts+1)
			}
		})
	}
}

func runScenario(t *testing.T, s *RepoState, ctx context.Context, fn func(t *testing.T, s *RepoState, ctx context.Context)) (restarted bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(errScenarioRestart); ok {
				restarted = true
				return
			}
			panic(r)
		}
	}()
	fn(t, s, ctx)
	return false
}

const maxScenarioRestarts = 2

type errScenarioRestart struct {
	msg string
}

// RunPrompt runs an agent prompt, logs the command and output to ConsoleLog,
// and returns the result. If the agent reports a transient API error, it
// panics with errScenarioRestart to trigger a full scenario restart.
func (s *RepoState) RunPrompt(t *testing.T, ctx context.Context, prompt string, opts ...agents.Option) (agents.Output, error) {
	t.Helper()
	out, err := s.Agent.RunPrompt(ctx, s.Dir, prompt, opts...)
	s.logPromptResult(out)

	if err != nil && s.Agent.IsTransientError(out, err) {
		errMsg := fmt.Sprintf("transient API error (stderr: %s)", strings.TrimSpace(out.Stderr))
		t.Logf("%s — restarting scenario", errMsg)
		_, _ = fmt.Fprintf(s.ConsoleLog, "> [transient] %s — restarting scenario\n", errMsg)
		panic(errScenarioRestart{msg: errMsg})
	}

	return out, err
}

func (s *RepoState) logPromptResult(out agents.Output) {
	_, _ = s.ConsoleLog.WriteString("> " + out.Command + "\n")
	_, _ = s.ConsoleLog.WriteString("stdout:\n" + out.Stdout + "\n")
	_, _ = s.ConsoleLog.WriteString("stderr:\n" + out.Stderr + "\n")
}

// Git runs a git command in the repo and logs it to ConsoleLog.
func (s *RepoState) Git(t *testing.T, args ...string) {
	t.Helper()
	_, _ = s.ConsoleLog.WriteString("> git " + strings.Join(args, " ") + "\n")
	Git(t, s.Dir, args...)
}

// GitOutput runs a git command and returns its trimmed stdout.
func (s *RepoState) GitOutput(t *testing.T, args ...string) string {
	t.Helper()
	return GitOutput(t, s.Dir, args...)
}

// StartSession starts an interactive session and registers it for pane
// capture in artifacts. Returns nil if the agent does not support interactive
// mode. The session is closed automatically during test cleanup.
func (s *RepoState) StartSession(t *testing.T, ctx context.Context) agents.Session {
	t.Helper()
	session, err := s.Agent.StartSession(ctx, s.Dir)
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if session == nil {
		return nil
	}
	s.session = session
	return session
}

// WaitFor waits for a pattern in the interactive session's pane and logs the
// pane content to ConsoleLog after the wait completes.
func (s *RepoState) WaitFor(t *testing.T, session agents.Session, pattern string, timeout time.Duration) {
	t.Helper()
	content, err := session.WaitFor(pattern, timeout)
	_, _ = fmt.Fprintf(s.ConsoleLog, "> pane after WaitFor(%q):\n%s\n", pattern, content)
	if err != nil {
		t.Fatalf("WaitFor(%q): %v", pattern, err)
	}
}

// IsExternalAgent returns true if the agent implements the ExternalAgent
// interface and reports itself as external.
func (s *RepoState) IsExternalAgent() bool {
	ea, ok := s.Agent.(agents.ExternalAgent)
	return ok && ea.IsExternalAgent()
}

// Send sends input to an interactive session and logs it to ConsoleLog.
func (s *RepoState) Send(t *testing.T, session agents.Session, input string) {
	t.Helper()
	_, _ = s.ConsoleLog.WriteString("> send: " + input + "\n")
	if err := session.Send(input); err != nil {
		t.Fatalf("send failed: %v", err)
	}
}

// PatchSettings merges extra keys into .entire/settings.json.
func PatchSettings(t *testing.T, dir string, extra map[string]any) {
	t.Helper()
	path := filepath.Join(dir, ".entire", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	for k, v := range extra {
		settings[k] = v
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

// Git runs a git command in the given directory and fails the test on error.
func Git(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "ENTIRE_TEST_TTY=0")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// GitOutput runs a git command in the given directory, returns its trimmed
// stdout, and fails the test on error.
func GitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	out, err := cmd.Output()
	if err != nil {
		var stderr string
		ee := &exec.ExitError{}
		if errors.As(err, &ee) {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, stderr)
	}

	return strings.TrimSpace(string(out))
}

// NewCheckpointCommits returns the SHAs of commits added to the
// entire/checkpoints/v1 branch since the test was set up, oldest first.
func NewCheckpointCommits(t *testing.T, s *RepoState) []string {
	t.Helper()

	log := GitOutput(t, s.Dir, "log", "--reverse", "--format=%H", s.CheckpointBefore+"..entire/checkpoints/v1")
	if log == "" {
		return nil
	}
	return strings.Split(log, "\n")
}

// CheckpointIDs lists all checkpoint IDs from the tree at the tip of the
// checkpoint branch.
func CheckpointIDs(t *testing.T, dir string) []string {
	t.Helper()
	out := gitOutputSafe(dir, "ls-tree", "-r", "--name-only", "entire/checkpoints/v1")
	if out == "" {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, "/")
		if len(parts) == 3 && parts[2] == "metadata.json" {
			id := parts[0] + parts[1]
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// ReadCheckpointMetadata reads checkpoint-level metadata.json from the
// checkpoint branch for the given checkpoint ID.
func ReadCheckpointMetadata(t *testing.T, dir string, checkpointID string) CheckpointMetadata {
	t.Helper()

	path := CheckpointPath(checkpointID) + "/metadata.json"
	blob := "entire/checkpoints/v1:" + path

	raw := GitOutput(t, dir, "show", blob)

	var meta CheckpointMetadata
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		t.Fatalf("unmarshal checkpoint metadata from %s: %v", blob, err)
	}

	return meta
}

// ReadSessionMetadata reads a session's metadata.json from the checkpoint
// branch for the given checkpoint ID and session index.
func ReadSessionMetadata(t *testing.T, dir string, checkpointID string, sessionIndex int) SessionMetadata {
	t.Helper()

	path := fmt.Sprintf("%s/%d/metadata.json", CheckpointPath(checkpointID), sessionIndex)
	blob := "entire/checkpoints/v1:" + path

	raw := GitOutput(t, dir, "show", blob)

	var meta SessionMetadata
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		t.Fatalf("unmarshal session metadata from %s: %v", blob, err)
	}

	return meta
}

// SetupBareRemote creates a bare git repo, adds it as "origin", and pushes
// the initial commit. Returns the bare repo path.
func SetupBareRemote(t *testing.T, s *RepoState) string {
	t.Helper()

	var bareDir string
	if os.Getenv("E2E_KEEP_REPOS") != "" {
		var err error
		bareDir, err = os.MkdirTemp("", "e2e-bare-*")
		if err != nil {
			t.Fatalf("create bare remote dir: %v", err)
		}
		t.Logf("E2E_KEEP_REPOS: bare remote will be preserved at %s", bareDir)
	} else {
		bareDir = t.TempDir()
	}

	Git(t, bareDir, "init", "--bare")
	Git(t, s.Dir, "remote", "add", "origin", bareDir)
	Git(t, s.Dir, "push", "-u", "origin", "HEAD")
	return bareDir
}

// GitOutputErr runs a git command and returns (output, error) without
// failing the test.
func GitOutputErr(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// GetCheckpointTrailer extracts the Entire-Checkpoint trailer value from a
// code commit. Returns the trimmed trailer value, or an empty string if the
// trailer is not present.
func GetCheckpointTrailer(t *testing.T, dir string, ref string) string {
	t.Helper()

	return GitOutput(t, dir, "log", "-1", "--format=%(trailers:key=Entire-Checkpoint,valueonly)", ref)
}
