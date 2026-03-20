# External Agents for Entire CLI

This repository contains standalone external agent binaries that extend the [Entire CLI](https://github.com/entireio/cli) with support for additional AI coding agents.

## What Are External Agents?

External agents are standalone binaries (named `entire-agent-<name>`) that teach Entire CLI how to work with AI coding agents it doesn't natively support. When an external agent is installed on your `PATH`, Entire discovers it automatically and gains the ability to:

- **Create checkpoints** during AI coding sessions so you can rewind mistakes
- **Capture transcripts** of what the AI agent did and why
- **Install hooks** so the AI agent's lifecycle events (start, stop, commit) flow through Entire

External agents communicate with Entire CLI via subcommands that accept and return JSON over stdin/stdout. See the [external agent protocol spec](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md) for the full interface.

## Available Agents

| Agent | Directory | Status |
|-------|-----------|--------|
| [Kiro](agents/entire-agent-kiro/) | `agents/entire-agent-kiro/` | Implemented — hooks + transcript analysis |

See each agent's own README for setup and usage instructions.

## Building a New External Agent

This repo includes a skill that guides you through building a new external agent using an E2E-first TDD pipeline. The skill runs in three phases:

1. **Research** — analyzes the target AI agent's file formats, session layout, and hook mechanisms
2. **Write tests** — generates E2E and unit tests against the external agent protocol
3. **Implement** — builds the Go binary to pass all tests

### Getting Started — Zero Setup

Clone the repo and open it in your AI coding tool. Each tool auto-discovers the skill with no additional configuration:

| Tool | How it discovers | What to say |
|------|-----------------|-------------|
| **Claude Code** | `.claude/skills/` directory | `/entire-external-agent` |
| **Codex** | `AGENTS.md` at project root | "Build an external agent" |
| **Cursor** | `.cursor/rules/` directory | "Build an external agent" |
| **OpenCode** | `.opencode/plugins/` auto-loaded | "Build an external agent" |

The skill files live in `.claude/skills/entire-external-agent/` if you want to read the details.

## E2E Tests

The `e2e/` directory contains a shared test harness that exercises all external agents. Tests are split into two tiers:

- **Subcommand tests** (`kiro_test.go`) — exercise each protocol subcommand directly against the agent binary (identity, sessions, transcript, hooks, transcript analysis). These run without any external dependencies beyond the agent binary itself.
- **Lifecycle tests** (`kiro_lifecycle_test.go`) — exercise the full integration flow: `entire enable`, agent prompt execution, git commit, checkpoint creation, and rewind. These require the `entire` CLI and the agent's own CLI (e.g. `kiro-cli-chat`) to be available.

### Running Tests

```bash
# Run all E2E tests (subcommand-level only; lifecycle tests skip if deps missing)
make test-e2e

# Run lifecycle tests (fails instead of skipping if entire/kiro-cli-chat are missing)
make test-e2e-lifecycle

# Run unit tests for all agents
make test-unit

# Run everything
make test-all
```

### Test Harness Architecture

The shared harness auto-discovers and builds all agents in `agents/` via `TestMain`:

| File | Purpose |
|------|---------|
| `e2e/setup_test.go` | `TestMain` entry point — discovers agents, builds binaries, configures PATH |
| `e2e/testenv.go` | `TestEnv` — isolated filesystem environment with agent binary runner |
| `e2e/harness.go` | `AgentRunner` — executes agent subcommands, captures stdout/stderr/exit code |
| `e2e/fixtures.go` | Test input builders: `HookInput`, `ParseHookInput`, `KiroTranscript` |
| `e2e/entire.go` | CLI wrappers: `EntireEnable`, `EntireDisable`, `EntireRewindList`, `EntireRewind` |
| `e2e/lifecycle.go` | `LifecycleEnv` — full lifecycle environment (git repo + `entire enable` + checkpoint helpers) |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `E2E_ENTIRE_BIN` | Path to the `entire` binary (defaults to `entire` from PATH) |
| `E2E_REQUIRE_LIFECYCLE` | Set to `1` to fail (instead of skip) when lifecycle dependencies are missing |

## Repository Layout

```
agents/                          # Standalone external agent projects
  entire-agent-kiro/             # Kiro agent (Go binary)
e2e/                             # Shared E2E test harness for all agents
.claude/skills/entire-external-agent/  # Skill files (research, test-writer, implementer)
AGENTS.md                        # Codex auto-discovery
.cursor/rules/                   # Cursor auto-discovery
.opencode/plugins/               # OpenCode auto-discovery
```
