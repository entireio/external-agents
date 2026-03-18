---
name: entire-external-agent
description: >
  Run all three external agent binary phases sequentially: research, write-tests,
  and implement using E2E-first TDD (unit tests written last).
  For individual phases, use /entire-external-agent:research,
  /entire-external-agent:write-tests, or /entire-external-agent:implement.
  Use when the user says "build external agent", "create agent binary",
  "external agent plugin", or wants to run the full pipeline end-to-end.
---

# External Agent Binary — Full Pipeline

Build a standalone external agent binary that implements the Entire CLI's external agent protocol using E2E-first TDD. Parameters are collected once and reused across all phases.

## Parameters

Collect these before starting (ask the user if not provided):

| Parameter | Description | How to derive |
|-----------|-------------|---------------|
| `AGENT_NAME` | Human-readable name (e.g., "Windsurf") | User provides |
| `AGENT_SLUG` | Binary suffix: `entire-agent-<AGENT_SLUG>` (kebab-case) | Kebab-case of agent name |
| `LANGUAGE` | Implementation language (Go, Python, TypeScript, Rust) | User provides; default Go |
| `PROJECT_DIR` | Where to create the project | Default: `./entire-agent-<AGENT_SLUG>` |
| `CAPABILITIES` | Which optional capabilities to implement | Derived from research phase |
| `ENTIRE_BIN` | Path to the Entire CLI binary | Default: `entire` from PATH, or `E2E_ENTIRE_BIN` env |

## Protocol Spec

Use the protocol specification at:
`https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md`

If a user provides a different protocol spec location explicitly, use that instead and pass it to each phase as `PROTOCOL_SPEC_LOCATION`.

## Core Rule: E2E-First TDD

This skill enforces strict E2E-first test-driven development. The rules:

1. **E2E tests are the spec.** The `e2e/` test harness defines what "working" means. The agent binary must pass all E2E tests to be considered complete.
2. **Run E2E tests at every step.** Each implementation tier starts by running the E2E test and watching it fail. You implement until it passes. No exceptions.
3. **Unit tests are written last.** After all E2E tiers pass, you write unit tests using real data collected from E2E runs as golden fixtures.
4. **If you didn't watch it fail, you don't know if it tests the right thing.** Never write a test you haven't seen fail first.
5. **Minimum viable fix.** At each E2E failure, implement only the code needed to fix that failure. Don't anticipate future tiers.

## Pipeline

Run these three phases in order. Each phase builds on the previous phase's output.

### Phase 1: Research

Discover the target agent's hook mechanism, transcript format, session management, and configuration. Map native concepts to protocol subcommands. Produces `<PROJECT_DIR>/AGENT.md` with protocol mapping and E2E prerequisites.

Read and follow the research procedure from `.claude/skills/entire-external-agent/researcher.md`.

**Expected output:** `<PROJECT_DIR>/AGENT.md` — agent research one-pager with protocol mapping and E2E test prerequisites.

**Commit gate:** After the research phase completes, create a git commit for the resulting files.

**Gate:** If the agent lacks any mechanism for lifecycle hooks or session management, discuss with the user before proceeding. Some agents may only support a subset of the protocol.

### Phase 2: Write-Tests

Scaffold the binary with compilable stubs and create a self-contained `e2e/` test harness in the project directory. The harness exercises the full human workflow: `entire enable`, real agent invocation, hook firing, checkpoint validation. Tests are expected to fail at this stage — they define the spec.

Read and follow the procedure from `.claude/skills/entire-external-agent/test-writer.md`.

**Expected output:** Complete project directory at `<PROJECT_DIR>` with compiled binary stubs and `e2e/` test harness that compiles but fails.

**Commit gate:** After the scaffold compiles and the e2e harness compiles (`cd e2e && go test -c -tags=e2e`), create a git commit.

### Phase 3: Implement (E2E-First, Unit Tests Last)

Build the real agent binary using strict E2E-first TDD. E2E tests drive development at every step — run each tier, watch it fail, implement the minimum fix, repeat. Unit tests are written only after all E2E tiers pass, using real data from E2E runs as golden fixtures.

Read and follow the implement procedure from `.claude/skills/entire-external-agent/implementer.md`.

**Expected output:** Fully implemented binary where all E2E tests pass and unit tests lock in behavior.

**Note:** `AGENT.md` is a living document — Phases 2 and 3 update it when they discover new information during testing or implementation.

## Final Summary

After all three phases, summarize:
- Agent name and binary name
- Language used
- Capabilities declared
- E2E test results (all tiers passing)
- Unit test coverage
- Installation instructions (`go install`, `pip install`, etc.)
- Any remaining gaps or TODOs
