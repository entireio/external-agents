---
name: implement
description: >
  Phase 3: Implement the external agent binary using protocol compliance first,
  lifecycle integration second, and unit tests last.
---

# Implement Procedure

Implement the agent with black-box-first TDD.

The order is:

1. protocol compliance against `external-agents-tests`
2. lifecycle integration in this repo's `e2e/` harness
3. unit tests in the agent module

## Prerequisites

Ensure the following are available:

- `AGENT_NAME`
- `AGENT_SLUG`
- `PROJECT_DIR`
- `<PROJECT_DIR>/AGENT.md`
- compiling scaffold from the write-tests phase

## Step 1: Read Before Coding

Read:

1. the protocol spec
2. the current agent code
3. `<PROJECT_DIR>/AGENT.md`
4. the lifecycle adapter in `e2e/agents/<slug>.go` if it already exists

## Step 2: Establish the First Failing Compliance Run

Build the binary and run the shared compliance suite first using the `external-agents-tests` CLI (installed via `mise install`):

```bash
cd <PROJECT_DIR>
mise run build
external-agents-tests verify ./entire-agent-<slug>
```

Do not start by adding new protocol tests to this repo.

## Step 3: Fix Compliance Failures Incrementally

For each failing compliance assertion:

1. rerun `external-agents-tests verify ./entire-agent-<slug>`
2. inspect the exact subcommand behavior
3. implement the minimum fix
4. rerun until it passes

Areas the compliance suite typically drives:

- `info` and capability declarations
- `detect`
- session helpers
- transcript chunking and reassembly
- session read/write behavior
- hooks capability
- transcript analysis capability

## Step 4: Run Lifecycle Tests

Once protocol compliance is in good shape, validate the real integration path:

```bash
cd /path/to/repo
E2E_AGENT=<slug> mise run test-e2e
```

These tests require:

- the Entire CLI
- the real agent CLI on `PATH`
- `tmux` for interactive scenarios

If those dependencies are not available, note the gap explicitly and continue with the protocol and unit-test work.

## Step 5: Fix Lifecycle Failures Incrementally

Use lifecycle failures to refine:

- the agent CLI adapter in `e2e/agents/<slug>.go`
- prompt execution details
- hook installation behavior after `entire enable`
- rewind and checkpoint interactions
- interactive session handling

Keep protocol fixes in the agent binary itself. Keep real-CLI orchestration fixes in the lifecycle adapter.

## Step 6: Add Unit Tests Last

After the behavior is working end to end, add unit tests in the agent module for:

- hook parsing
- transcript parsing
- config file read-modify-write behavior
- session file handling
- protocol handlers

Prefer using real payloads or fixtures captured during compliance and lifecycle runs.

## Step 7: Final Validation

Run:

```bash
cd <PROJECT_DIR>
mise run test
external-agents-tests verify ./entire-agent-<slug>

cd /path/to/repo
E2E_AGENT=<slug> mise run test-e2e
```

## Output Checklist

Summarize:

- compliance status
- lifecycle status
- unit-test status
- any dependencies you could not satisfy locally
- remaining gaps or TODOs
