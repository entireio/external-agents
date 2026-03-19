# Write-Tests Procedure

Scaffold the external agent binary and add E2E tests to the shared repo-root `e2e/` harness. The harness auto-discovers all agents and exercises each one via protocol subcommands and full lifecycle integration (entire enable, agent invocation, checkpoint validation). Tests are expected to fail — they define the spec for the implement phase.

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

## Step 2: Read the Shared E2E Harness

This repo already has a shared E2E harness at the repo root `e2e/` directory. Read these files to understand the patterns you must follow:

1. `e2e/setup_test.go` — `TestMain` entry point: auto-discovers agents in `agents/`, builds binaries, adds them to PATH
2. `e2e/testenv.go` — `TestEnv`: isolated filesystem environment with `AgentRunner`, `WriteFile`, `ReadFile`, `GitInit` helpers
3. `e2e/harness.go` — `AgentRunner`: executes agent subcommands via `Run`, `RunJSON`, `MustSucceed`, `MustFail`
4. `e2e/fixtures.go` — Test input builders: `HookInput`, `ParseHookInput`, `KiroTranscript` (with `AddPrompt`, `AddResponse`, `AddPromptWithFileEdit`)
5. `e2e/entire.go` — CLI wrappers: `EntireEnable`, `EntireDisable`, `EntireRewindList`, `EntireRewind`, `EntireRunErr`
6. `e2e/lifecycle.go` — `LifecycleEnv`: full lifecycle environment (git repo + `entire enable` + `WaitForCheckpoint` + `GetCheckpointTrailer`)
7. `e2e/kiro_test.go` — Example subcommand tests (identity, sessions, hooks, transcript analysis)
8. `e2e/kiro_lifecycle_test.go` — Example lifecycle tests (single/multi prompt, detect+enable, rewind, session persistence)

**Key patterns to follow:**
- All E2E files use `//go:build e2e` build tag and `package e2e`
- `TestMain` auto-discovers agents by scanning `agents/entire-agent-*` directories for `cmd/<name>/main.go`
- `NewTestEnv(t, "entire-agent-<slug>")` creates an isolated env with the built agent binary
- `NewLifecycleEnv(t, "<slug>")` creates a full git repo with `entire enable` already run
- Subcommand tests use `t.Parallel()` and `AgentRunner.RunJSON` for structured assertions
- Lifecycle tests call `requireEntire(t)` and `requireKiroCLI(t)` (or equivalent) to skip/fail gracefully
- `WaitForCheckpoint` polls until the `entire/checkpoints/v1` branch appears
- Fixture builders (e.g. `KiroTranscript`) use the fluent pattern for easy test data construction

## Step 3: Add Tests to the Shared E2E Harness

Tests go in the existing `e2e/` directory at the repo root. The harness already provides all infrastructure — you only need to add test files and (optionally) agent-specific fixture builders.

### How auto-discovery works

`TestMain` in `e2e/setup_test.go` scans `agents/entire-agent-*` for directories with `cmd/<name>/main.go`, builds each binary, and stores them in `agentBinaries`. Your new agent is discovered automatically once the scaffold from Step 1 compiles.

### Create `e2e/<slug>_test.go` — Subcommand Tests

These exercise each protocol subcommand directly. Follow the pattern in `e2e/kiro_test.go`:

```go
//go:build e2e

package e2e

import "testing"

// --- Identity ---

func Test<Name>_Info(t *testing.T) {
    t.Parallel()
    env := NewTestEnv(t, "entire-agent-<slug>")
    // Use env.Runner.RunJSON to decode the info response
    // Assert protocol_version, name, capabilities, etc.
}

func Test<Name>_Detect_Present(t *testing.T) {
    t.Parallel()
    env := NewTestEnv(t, "entire-agent-<slug>")
    // Create the agent's marker directory (e.g. .<slug>/)
    // Assert detect returns present: true
}

func Test<Name>_Detect_Absent(t *testing.T) {
    t.Parallel()
    env := NewTestEnv(t, "entire-agent-<slug>")
    // Assert detect returns present: false (no marker directory)
}

// --- Sessions ---
// Test get-session-id, get-session-dir, resolve-session-file, write+read-session

// --- Hooks ---
// Test parse-hook for each hook type, install-hooks, uninstall-hooks, are-hooks-installed

// --- Transcript ---
// Test read-transcript, chunk+reassemble-transcript round-trip

// --- Transcript Analysis (if capability declared) ---
// Test get-transcript-position, extract-modified-files, extract-prompts, extract-summary
```

**Key patterns:**
- Use `NewTestEnv(t, "entire-agent-<slug>")` for isolated environments
- Create convenience constructors like `NewKiroTestEnv` if multiple tests share setup
- Use `env.Runner.RunJSON` for structured output, `MustSucceed`/`MustFail` for exit code checks
- All subcommand tests use `t.Parallel()` for speed

### Create `e2e/<slug>_lifecycle_test.go` — Lifecycle Tests

These exercise the full integration. Follow the pattern in `e2e/kiro_lifecycle_test.go`:

```go
//go:build e2e

package e2e

import "testing"

func TestLifecycle_<Name>_SinglePromptManualCommit(t *testing.T) {
    requireEntire(t)
    // require<Name>CLI(t)  — add a similar helper for your agent's CLI
    t.Parallel()

    env := NewLifecycleEnv(t, "<slug>")

    // Run a prompt that creates a file
    // Assert the file exists
    // git add + commit
    // WaitForCheckpoint
    // Verify checkpoint trailer
}
```

**Key patterns:**
- Call `requireEntire(t)` (and a `require<Name>CLI(t)` helper) at the top — these skip the test gracefully when dependencies are missing
- Use `NewLifecycleEnv(t, "<slug>")` which handles git init, seed commit, `.entire/settings.json`, and `entire enable`
- Add a `Run<Name>Prompt` method on `LifecycleEnv` using the command from AGENT.md's E2E prerequisites

### Add agent-specific fixture builders (if needed)

If the agent has a custom transcript format, add a builder to `e2e/fixtures.go` following the `KiroTranscript` pattern:

```go
type <Name>Transcript struct { /* ... */ }
func New<Name>Transcript(id string) *<Name>Transcript { /* ... */ }
func (t *<Name>Transcript) AddPrompt(prompt string) *<Name>Transcript { /* ... */ }
func (t *<Name>Transcript) JSON(t *testing.T) string { /* ... */ }
```

### Add agent-specific environment helpers (if needed)

If the agent needs custom setup (e.g., Kiro needs `.kiro/` and `.entire/tmp/`), add a convenience constructor to `e2e/testenv.go`:

```go
func New<Name>TestEnv(t *testing.T) *TestEnv {
    t.Helper()
    te := NewTestEnv(t, "entire-agent-<slug>")
    te.MkdirAll(".<slug>")
    te.MkdirAll(".entire/tmp")
    return te
}
```

### Key conventions for test scenarios

- **Build tag**: All E2E files must have `//go:build e2e` as the first line
- **Package**: All files in `e2e/` use `package e2e`
- **Naming**: Subcommand tests: `Test<Name>_<Subcommand>`. Lifecycle tests: `TestLifecycle_<Name>_<Scenario>`
- **Timeouts**: Lifecycle tests use `WaitForCheckpoint(t, env, 30*time.Second)` for checkpoint polling
- **Prompts**: Write prompts inline — include "Do not ask for confirmation" for agents that stall
- **Assertions**: Use harness helpers (`AssertFileExists`, `GetCheckpointTrailer`), not raw git commands
- **CLI operations**: Use `EntireEnable`, `EntireRewindList`, `EntireRewind` — never raw `exec.Command`
- **Parallelism**: Subcommand tests use `t.Parallel()`. Lifecycle tests use `t.Parallel()` per test (each gets its own temp repo)
- **Graceful skipping**: Lifecycle tests call `requireEntire(t)` to skip when the entire CLI isn't available

## Step 4: Add Makefile Targets

### Agent-level Makefile (`<PROJECT_DIR>/Makefile`)

Add `build`, `test`, and `clean` targets for the agent binary:

```makefile
BINARY := entire-agent-<AGENT_SLUG>

.PHONY: build test clean

build:
	go build -o $(BINARY) ./cmd/entire-agent-<AGENT_SLUG>

test:
	go test ./...

clean:
	rm -f $(BINARY)
```

### Repo-root Makefile

The repo-root `Makefile` already handles E2E test execution. Verify it includes:

```makefile
test-e2e:
	cd e2e && go test -tags=e2e -v -count=1 ./...

test-e2e-lifecycle:
	cd e2e && E2E_REQUIRE_LIFECYCLE=1 go test -tags=e2e -v -count=1 -run TestLifecycle ./...

test-unit:
	@for dir in agents/entire-agent-*/; do \
		echo "Testing $$dir..."; \
		cd $$dir && go test ./... && cd ../..; \
	done

test-all: test-unit test-e2e
```

The `test-e2e` target builds all agents automatically via `TestMain` — no need to build/install first.

## Step 5: Verify Tests Compile

Run from the repo root:
```bash
cd e2e && go test -c -tags=e2e
```

This must succeed (compiles the test binary including the new agent's tests). Tests are expected to fail when executed — they define the spec for the implement phase.

If the harness doesn't compile, fix issues before proceeding.

## Step 6: Commit

Create a git commit for the new E2E tests and the scaffolded binary.

## Output

Summarize what was created:
- Project structure (files created, capabilities declared)
- E2E tests added (number of subcommand tests and lifecycle tests, what they exercise)
- Confirmation that binary compiles and `info` returns valid JSON
- Confirmation that E2E harness compiles with new tests (`go test -c -tags=e2e`)
- Note that all E2E tests are expected to fail — the implement phase will make them pass
- Commands to run: `make test-e2e` (all tests) or `cd e2e && go test -tags=e2e -v -run Test<Name>_Info ./...` (single test)
