# Write-Tests Procedure

Scaffold the external agent binary and create a self-contained E2E test harness. The harness exercises the full human workflow: `entire enable`, real agent invocation, hook firing, and checkpoint validation. Tests are expected to fail — they define the spec for the implement phase.

## Prerequisites

Ensure the following are available:
- `AGENT_NAME`, `AGENT_SLUG`, `LANGUAGE`, `PROJECT_DIR` — from orchestrator or user
- `<PROJECT_DIR>/AGENT.md` — research one-pager with protocol mapping and E2E test prerequisites

## Step 1: Scaffold the Binary

Generate the project structure with compilable stubs. This is a condensed version of scaffolding — enough to get a binary that compiles and returns valid `info` JSON.

### Read source material at runtime

**Do not use static templates.** Read the following files at runtime to generate code that matches the current protocol version:

1. Read `https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md` — subcommand specs, JSON schemas, capabilities
2. Read `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/types.go` — JSON response struct definitions
3. Read `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/external.go` — how the CLI calls each subcommand
4. Read `<PROJECT_DIR>/AGENT.md` — agent-specific decisions (capabilities, hook format, transcript location)

### Generate project structure (Go)

```
<PROJECT_DIR>/
  go.mod                    # Module: github.com/<user>/entire-agent-<slug>
  main.go                   # Subcommand dispatch switch
  cmd/
    info.go                 # Required: info subcommand
    detect.go               # Required: detect subcommand
    session.go              # Required: session subcommands
    transcript.go           # Required: transcript subcommands
    resume.go               # Required: format-resume-command
    hooks.go                # Capability: hooks (if declared)
    analyzer.go             # Capability: transcript_analyzer (if declared)
    [other capabilities]
  internal/
    types.go                # Response types from external/types.go
    protocol.go             # Env var helpers, constants
  AGENT.md                  # Research one-pager (already exists)
  README.md                 # Usage, installation, development
  Makefile                  # build, install, test, test:e2e
```

**Only create capability files for capabilities declared in AGENT.md.**

Each subcommand handler should:
1. Parse arguments from `os.Args` or the language's arg parser
2. Read stdin if required
3. Return valid JSON matching the exact schema from `types.go`
4. Use placeholder values (realistic but clearly fake, e.g., `session_id: "stub-session-000"`)

### Verify the scaffold

1. **Compiles without errors:** `make build`
2. **`info` returns valid JSON:** `./entire-agent-<slug> info | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin), indent=2))"`
3. **Unknown subcommand exits non-zero:** `./entire-agent-<slug> bogus; echo "exit: $?"`

### Commit the scaffold

Create a git commit for the scaffolded project.

## Step 2: Read CLI E2E Infrastructure as Reference

Read the Entire CLI's E2E test infrastructure to understand the patterns we need to adapt for the self-contained harness. These are in the Entire CLI repo (or fetch from GitHub if not available locally):

1. `e2e/agents/agent.go` — Agent interface pattern (Output struct, Session interface, ExternalAgent interface)
2. `e2e/agents/roger_roger.go` — External agent runner example (RunPrompt, StartSession, IsExternalAgent)
3. `e2e/tests/external_agent_test.go` — External agent test scenarios (the actual test patterns we'll adapt)
4. `e2e/testutil/repo.go` — SetupRepo pattern (temp dir, git init, entire enable, external_agents setting)
5. `e2e/testutil/assertions.go` — Assertion helpers (WaitForCheckpoint, AssertCheckpointAdvanced, ValidateCheckpointDeep)
6. `e2e/entire/entire.go` — CLI wrapper pattern (BinPath, Enable, RewindList, Rewind)

**Key patterns to adapt:**
- `SetupRepo` creates a temp git repo, writes `.entire/settings.json` with `external_agents: true`, runs `entire enable`
- `ExternalAgent` interface marks agents discovered via the external agent protocol
- `ForEachAgent` runs each test per registered agent with timeout scaling and artifact capture
- `WaitForCheckpoint` polls until checkpoint branch advances (post-commit hook is async)
- `ValidateCheckpointDeep` checks transcript content, content hash, and prompt extraction

## Step 3: Create E2E Test Harness

Create a self-contained `e2e/` directory in `<PROJECT_DIR>` with its own Go module. This avoids circular dependencies with the agent binary module.

### `e2e/go.mod`

```go
module <module-path>/e2e

go 1.23

require (
    github.com/stretchr/testify v1.9.0
)
```

Run `cd e2e && go mod tidy` after creating.

### `e2e/harness.go` — SetupRepo

Adapted from CLI's `e2e/testutil/repo.go`. Creates a fresh git repo and enables the agent.

```go
//go:build e2e

package e2e

// RepoState holds working state for a single test repo.
type RepoState struct {
    AgentSlug        string
    Dir              string
    ArtifactDir      string
    HeadBefore       string
    CheckpointBefore string
}

// SetupRepo creates a fresh git repository, enables the agent, and returns state.
//
// Steps:
// 1. Create temp dir (use os.MkdirTemp, NOT t.TempDir — too nested for agents)
// 2. Resolve symlinks (macOS: /var -> /private/var)
// 3. git init, seed commit, user config
// 4. Write .entire/settings.json with {"external_agents": true}
// 5. Run entire enable --agent <slug>
// 6. Patch settings for debug logging
// 7. Record HEAD and checkpoint refs
// 8. Register cleanup (artifact capture + repo removal)
// 9. Return *RepoState
func SetupRepo(t *testing.T, agentSlug string) *RepoState {
    // Implementation follows the CLI's SetupRepo pattern
}
```

### `e2e/entire.go` — CLI Wrapper

Adapted from CLI's `e2e/entire/entire.go`. Wraps Entire CLI invocations.

```go
//go:build e2e

package e2e

// EntireBin returns the path to the entire binary.
// Checks E2E_ENTIRE_BIN env var, falls back to "entire" from PATH.
func EntireBin() string {}

// Enable runs `entire enable --agent <name>` in dir.
func Enable(t *testing.T, dir, agent string) {}

// RewindList runs `entire rewind --list` and parses JSON output.
func RewindList(t *testing.T, dir string) []RewindPoint {}

// Rewind runs `entire rewind --to <id>`.
func Rewind(t *testing.T, dir, id string) error {}
```

### `e2e/assertions.go` — Checkpoint Assertions

Adapted from CLI's `e2e/testutil/assertions.go`. Provides checkpoint and metadata validation.

```go
//go:build e2e

package e2e

// WaitForCheckpoint polls until the checkpoint branch advances, or fails after timeout.
func WaitForCheckpoint(t *testing.T, s *RepoState, timeout time.Duration) {}

// AssertCheckpointAdvanced asserts the checkpoint branch moved forward.
func AssertCheckpointAdvanced(t *testing.T, s *RepoState) {}

// AssertHasCheckpointTrailer returns the checkpoint ID from the commit trailer.
func AssertHasCheckpointTrailer(t *testing.T, dir, ref string) string {}

// AssertCheckpointExists asserts the checkpoint metadata directory exists.
func AssertCheckpointExists(t *testing.T, dir, cpID string) {}

// AssertCheckpointMetadataComplete validates all required metadata fields.
func AssertCheckpointMetadataComplete(t *testing.T, dir, cpID string) {}

// ValidateCheckpointDeep performs comprehensive checkpoint validation.
func ValidateCheckpointDeep(t *testing.T, dir string, opts DeepOpts) {}

// DeepOpts configures deep checkpoint validation.
type DeepOpts struct {
    CheckpointID    string
    Strategy        string
    FilesTouched    []string
    ExpectedPrompts []string
}
```

### `e2e/agent.go` — Agent Runner

Uses AGENT.md's "E2E Test Prerequisites" section to invoke the agent binary.

```go
//go:build e2e

package e2e

// RunPrompt executes a non-interactive prompt against the agent binary.
// Uses the command and flags from AGENT.md's E2E prerequisites.
func RunPrompt(ctx context.Context, dir, prompt string) (Output, error) {}

// StartSession launches an interactive tmux-based session (if supported).
// Returns nil if the agent doesn't support interactive mode.
func StartSession(ctx context.Context, dir string) (*TmuxSession, error) {}

// Output captures command execution results.
type Output struct {
    Command  string
    Stdout   string
    Stderr   string
    ExitCode int
}
```

### `e2e/e2e_test.go` — Test Scenarios

Adapted from CLI's `e2e/tests/external_agent_test.go`. These are the spec that drives the implement phase.

```go
//go:build e2e

package e2e

import (
    "context"
    "testing"
    "time"
)

// TestHookInstallAndDetect verifies that `entire enable` succeeds and hooks
// are properly installed for the agent.
func TestHookInstallAndDetect(t *testing.T) {
    s := SetupRepo(t, agentSlug)
    // Verify enable succeeded (SetupRepo calls it)
    // Verify hooks are installed by checking agent config
    // Verify detect returns present: true
}

// TestSingleSessionManualCommit exercises the full agent lifecycle:
// start session -> agent creates file -> user commits -> checkpoint created.
func TestSingleSessionManualCommit(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    s := SetupRepo(t, agentSlug)

    // Run a prompt that creates a file
    // Assert the file exists
    // git add . && git commit
    // AssertNewCommits
    // WaitForCheckpoint
    // AssertCheckpointAdvanced
    // AssertHasCheckpointTrailer -> cpID
    // AssertCheckpointExists(cpID)
    // AssertCheckpointMetadataComplete(cpID)
}

// TestCheckpointDeepValidation verifies transcript content, content hash,
// and prompt extraction are correctly captured.
func TestCheckpointDeepValidation(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    s := SetupRepo(t, agentSlug)

    // Run a prompt
    // git add . && git commit
    // WaitForCheckpoint
    // ValidateCheckpointDeep with expected prompts and files
}

// TestMultipleTurnsManualCommit handles two sequential prompts, user commits once.
func TestMultipleTurnsManualCommit(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
    defer cancel()

    s := SetupRepo(t, agentSlug)

    // Run first prompt (creates file A)
    // Run second prompt (creates file B)
    // git add . && git commit
    // WaitForCheckpoint
    // AssertCheckpointMetadataComplete
    // Both files should appear in checkpoint
}

// TestSessionMetadata verifies checkpoint session metadata identifies the agent.
func TestSessionMetadata(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    s := SetupRepo(t, agentSlug)

    // Run a prompt
    // git add . && git commit
    // WaitForCheckpoint
    // Read session metadata
    // Assert agent field is set and matches
    // Assert session_id is non-empty
}

// TestInteractiveSession exercises tmux-based interactive multi-step sessions.
// Skip if the agent doesn't support interactive mode.
func TestInteractiveSession(t *testing.T) {
    // Check AGENT.md for interactive support
    // If not supported, t.Skip
    // StartSession
    // Send prompt, WaitFor response
    // Send second prompt, WaitFor response
    // Close session
    // git add . && git commit
    // WaitForCheckpoint
}

// TestRewind verifies rewind functionality works after a checkpoint.
func TestRewind(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    s := SetupRepo(t, agentSlug)

    // Run a prompt that creates a file
    // git add . && git commit
    // WaitForCheckpoint
    // Get checkpoint ID
    // Rewind to checkpoint
    // Verify state is restored
}
```

### Key conventions for test scenarios

- **Build tag**: All E2E files must have `//go:build e2e` as the first line
- **Package**: All files in `e2e/` use `package e2e`
- **Agent slug**: Hardcode the agent slug (from AGENT.md) as a package-level constant
- **Timeouts**: Use `context.WithTimeout` per test; scale by AGENT.md's timeout multiplier
- **Prompts**: Write prompts inline — include "Do not ask for confirmation" for agents that stall
- **Assertions**: Use the harness assertion helpers, not raw git commands
- **CLI operations**: Use `e2e.Enable`, `e2e.RewindList`, `e2e.Rewind` — never raw `exec.Command`
- **No t.Parallel()**: Tests share state through the agent binary; run sequentially
- **Transient errors**: Implement retry logic in `RunPrompt` using AGENT.md's transient error patterns

## Step 4: Add Makefile Targets

Add e2e test targets to the project Makefile:

```makefile
BINARY := entire-agent-<AGENT_SLUG>

.PHONY: build install test test\:e2e test\:e2e\:run clean

build:
	<language-specific build command>

install: build
	cp $(BINARY) $(GOPATH)/bin/ || cp $(BINARY) /usr/local/bin/

test:
	<language-specific unit test command>

test\:e2e: build install
	cd e2e && go test -tags=e2e -v -timeout 30m ./...

test\:e2e\:run: build install
	cd e2e && go test -tags=e2e -v -timeout 30m -run $(TEST) ./...

clean:
	rm -f $(BINARY)
```

The `test:e2e` target builds and installs the agent binary first (so it's on PATH), then runs the e2e tests.

## Step 5: Verify Harness Compiles

Run:
```bash
cd e2e && go test -c -tags=e2e
```

This must succeed (compiles the test binary). Tests are expected to fail when executed — they define the spec for the implement phase.

If the harness doesn't compile, fix issues before proceeding.

## Step 6: Commit

Create a git commit for the e2e test harness.

## Output

Summarize what was created:
- Project structure (files created, capabilities declared)
- E2E test harness (number of test scenarios, what they exercise)
- Confirmation that binary compiles and `info` returns valid JSON
- Confirmation that e2e harness compiles (`go test -c -tags=e2e`)
- Note that all e2e tests are expected to fail — the implement phase will make them pass
- Commands to run: `make test:e2e:run TEST=TestHookInstallAndDetect`
