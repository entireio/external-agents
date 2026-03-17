---
name: entire-external-agent
description: >
  Run all four external agent binary phases sequentially: research, scaffold,
  implement, and validate. For individual phases, use /entire-external-agent:research,
  /entire-external-agent:scaffold, /entire-external-agent:implement, or
  /entire-external-agent:validate.
  Use when the user says "build external agent", "create agent binary",
  "external agent plugin", or wants to run the full pipeline end-to-end.
---

# External Agent Binary — Full Pipeline

Build a standalone external agent binary that implements the Entire CLI's external agent protocol. Parameters are collected once and reused across all phases.

## Parameters

Collect these before starting (ask the user if not provided):

| Parameter | Description | How to derive |
|-----------|-------------|---------------|
| `AGENT_NAME` | Human-readable name (e.g., "Windsurf") | User provides |
| `AGENT_SLUG` | Binary suffix: `entire-agent-<AGENT_SLUG>` (kebab-case) | Kebab-case of agent name |
| `LANGUAGE` | Implementation language (Go, Python, TypeScript, Rust) | User provides; default Go |
| `PROJECT_DIR` | Where to create the project | Default: `./entire-agent-<AGENT_SLUG>` |
| `CAPABILITIES` | Which optional capabilities to implement | Derived from research phase |

## Standalone Detection

If `docs/architecture/external-agent-protocol.md` is not found in the current repo, this plugin is being used standalone. In that case:

1. Ask the user: "I can't find the protocol spec. Please provide one of: (a) URL to the spec, (b) file path, or (c) path to a clone of the Entire CLI repo."
2. Store the provided location and pass it to each phase as `PROTOCOL_SPEC_LOCATION`.
3. Each phase will read the spec from that location instead of the default repo path.

## Pipeline

Run these four phases in order. Each phase builds on the previous phase's output.

### Phase 1: Research

Discover the target agent's hook mechanism, transcript format, session management, and configuration. Map native concepts to protocol subcommands. Produces `<PROJECT_DIR>/AGENT.md`.

Read and follow the research procedure from `.claude/skills/entire-external-agent/researcher.md`.

**Expected output:** `<PROJECT_DIR>/AGENT.md` — agent research one-pager with protocol mapping.

**Commit gate:** After the research phase completes, use `/commit` to commit all files.

**Gate:** If the agent lacks any mechanism for lifecycle hooks or session management, discuss with the user before proceeding. Some agents may only support a subset of the protocol.

### Phase 2: Scaffold

Generate the project skeleton with compilable/runnable stubs for every required subcommand and each declared capability. The binary compiles and responds to `info` immediately.

Read and follow the scaffold procedure from `.claude/skills/entire-external-agent/scaffolder.md`.

**Expected output:** Complete project directory at `<PROJECT_DIR>` with `Makefile`, stub handlers, and a binary that compiles and responds to `info`.

**Commit gate:** After the scaffold compiles and `info` returns valid JSON, use `/commit` to commit all files.

### Phase 3: Implement

Replace stubs with real logic, working through subcommands in dependency order. Each subcommand is tested manually against the protocol spec.

Read and follow the implement procedure from `.claude/skills/entire-external-agent/implementer.md`.

**Expected output:** Fully implemented binary where all declared subcommands return real data.

**Commit gate:** After each tier of subcommands is implemented and manually tested, use `/commit` to commit.

### Phase 4: Validate

Run the full conformance test suite against the binary. Produces a PASS/FAIL report for every subcommand.

Read and follow the validate procedure from `.claude/skills/entire-external-agent/validator.md`.

**Expected output:** Conformance report with PASS/FAIL per subcommand and overall verdict.

**Commit gate:** After all conformance tests pass, use `/commit` to commit any final fixes.

## Final Summary

After all four phases, summarize:
- Agent name and binary name
- Language used
- Capabilities declared
- Conformance test results (PASS/FAIL count)
- Installation instructions (`go install`, `pip install`, etc.)
- Any remaining gaps or TODOs
