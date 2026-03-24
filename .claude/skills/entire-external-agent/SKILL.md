---
name: entire-external-agent
description: >
  Run all three external agent binary phases sequentially: research, write-tests,
  and implement using black-box-first TDD across protocol compliance, lifecycle
  integration, and unit tests. Accepts an optional argument to run a single phase:
  research, write-tests, or implement.
---

# External Agent Binary — Full Pipeline

Build a standalone external agent binary that implements the Entire CLI external agent protocol.

The current test split is:

1. **Protocol compliance** lives in `external-agents-tests`.
2. **Lifecycle integration** lives in this repo's `e2e/` harness.
3. **Unit tests** live in each agent module.

Do not add new generic protocol tests under this repo's `e2e/` directory.

## Parameters

Collect these before starting if the user did not provide them:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `AGENT_NAME` | Human-readable name (for example, `Windsurf`) | User-provided |
| `AGENT_SLUG` | Binary suffix for `entire-agent-<slug>` | Kebab-case of `AGENT_NAME` |
| `LANGUAGE` | Implementation language | `Go` |
| `PROJECT_DIR` | Agent directory to create or edit | `./agents/entire-agent-<slug>` |
| `ENTIRE_BIN` | Path to the Entire CLI binary for lifecycle testing | `entire` from `PATH` or `E2E_ENTIRE_BIN` |

## Phase Selection

- `/entire-external-agent research` runs only Phase 1.
- `/entire-external-agent write-tests` runs only Phase 2.
- `/entire-external-agent implement` runs only Phase 3.
- `/entire-external-agent` runs all three phases in order.

If a single phase is requested, still collect the shared parameters first.

## Protocol Spec

Use the protocol specification at:
`https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md`

If the user gives a different spec location explicitly, use that instead.

## Core Rule: Black-Box-First TDD

1. **Protocol compliance is the contract.** The binary must pass the shared `external-agents-tests` suite.
2. **Lifecycle tests prove real integration.** The repo-local `e2e/` harness covers the Entire + real-agent workflow and stays separate from generic protocol checks.
3. **Unit tests are written last.** After protocol and lifecycle behavior are working, add unit tests to lock down parsing, hooks, and file handling.
4. **Watch failures before fixing them.** Run the failing test first so you know what behavior the code must satisfy.
5. **Keep the fix scoped.** Implement only the behavior needed for the current failure, then rerun.

## Pipeline

### Phase 1: Research

Discover the target agent's hook mechanism, transcript format, session layout, CLI entrypoints, and lifecycle prerequisites. Produce `<PROJECT_DIR>/AGENT.md` with the protocol mapping and any real-CLI requirements needed for lifecycle tests.

Use `.claude/skills/entire-external-agent/research.md`.

Expected output:
- `<PROJECT_DIR>/AGENT.md`

### Phase 2: Write Tests

Scaffold the binary and the test surfaces you will need:

- agent module structure under `<PROJECT_DIR>`
- protocol compliance expectations compatible with `external-agents-tests`
- lifecycle adapter wiring in this repo's `e2e/` harness
- optional compliance fixtures if the agent benefits from stronger black-box detect or transcript assertions

Use `.claude/skills/entire-external-agent/write-tests.md`.

Expected output:
- compiling binary scaffold
- any needed lifecycle adapter files under `e2e/agents/`
- optional fixture file paths documented in `<PROJECT_DIR>/AGENT.md` or `README.md`

### Phase 3: Implement

Implement until:

- the binary passes protocol compliance
- lifecycle tests pass when the required CLIs are available
- unit tests cover the important internal behaviors

Use `.claude/skills/entire-external-agent/implement.md`.

Expected output:
- fully working binary
- passing unit tests
- passing protocol compliance
- passing lifecycle integration where dependencies are available

## Final Summary

At the end, summarize:

- agent name and binary name
- implementation language
- declared capabilities
- protocol compliance status
- lifecycle test status
- unit test coverage
- installation instructions
- any remaining gaps
