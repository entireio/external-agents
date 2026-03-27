---
name: write-tests
description: >
  Phase 2: Scaffold the external agent binary and add the correct testing hooks:
  protocol compliance through external-agents-tests and lifecycle integration
  through this repo's e2e harness.
---

# Write-Tests Procedure

Scaffold the external agent binary and wire it into the current testing split.

Do not add new generic protocol tests under this repo's `e2e/` directory.

## Prerequisites

Ensure these are available:

- `AGENT_NAME`
- `AGENT_SLUG`
- `LANGUAGE`
- `PROJECT_DIR`
- `<PROJECT_DIR>/AGENT.md`

## Step 1: Scaffold the Binary

Create a compilable binary that already exposes the protocol subcommands with valid JSON shapes.

Read at runtime:

1. `https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md`
2. `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/types.go`
3. `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/external.go`
4. `<PROJECT_DIR>/AGENT.md`

For Go agents, prefer this shape:

```text
<PROJECT_DIR>/
  go.mod
  mise.toml
  README.md
  AGENT.md
  cmd/
    entire-agent-<slug>/
      main.go
  internal/
    protocol/
    <agent>/
```

The scaffold must:

- build successfully
- return valid `info` JSON
- exit non-zero for an unknown subcommand

## Step 2: Prepare Protocol Compliance

The generic protocol suite lives in `external-agents-tests`.

Your job in this repo is to make the agent easy to validate there:

1. Keep the binary layout compatible with a simple build command:
   `go build -o entire-agent-<slug> ./cmd/entire-agent-<slug>`
2. If stronger black-box assertions are useful, add an optional fixture file under the agent module, for example:
   `<PROJECT_DIR>/testdata/compliance.json`
3. Document any required fixture paths in `<PROJECT_DIR>/README.md` and `<PROJECT_DIR>/AGENT.md`

If a sibling checkout exists at `../external-agents-tests`, you can validate the scaffold locally with:

```bash
cd ../external-agents-tests
AGENT_BINARY=/abs/path/to/entire-agent-<slug> go test -v -count=1 ./...
```

At this stage the tests are expected to fail. The goal is just to confirm the harness reaches the binary.

## Step 3: Wire the Agent into Lifecycle Tests

Lifecycle integration remains in this repo.

Read these files before editing:

1. `e2e/setup_test.go`
2. `e2e/build.go`
3. `e2e/lifecycle_test.go`
4. `e2e/agents/agent.go`
5. `e2e/agents/kiro.go`
6. `e2e/testutil/repo.go`
7. `e2e/entire/entire.go`

Then:

1. Add `e2e/agents/<slug>.go` implementing the `Agent` interface.
2. Register the agent in `init()` and set a concurrency gate.
3. Implement `RunPrompt`, `StartSession`, `PromptPattern`, timeout multiplier, and any external-agent marker behavior needed by `SetupRepo`.
4. Reuse the shared lifecycle scenarios in `e2e/lifecycle_test.go`. Add new lifecycle tests only if the new agent needs behavior that is not already covered.

## Step 4: Verify the Scaffolding

Run these checks:

```bash
cd <PROJECT_DIR>
mise run build
./entire-agent-<slug> info
go test ./...

cd /path/to/repo/e2e
go test -c -tags=e2e
```

If `../external-agents-tests` exists, also run one failing compliance pass against the built binary.

## Step 5: Commit Gate

Create a commit once:

- the binary compiles
- `info` returns valid JSON
- lifecycle harness compiles
- the compliance suite can invoke the binary, even if assertions still fail

## Output Checklist

Summarize:

- files created under `<PROJECT_DIR>`
- lifecycle adapter files added or updated under `e2e/`
- optional compliance fixture paths
- commands run and their status
