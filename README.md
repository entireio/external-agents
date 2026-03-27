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

This repo includes a skill that guides you through building a new external agent with two test layers:

1. **Protocol compliance** — generic subcommand coverage from [`entireio/external-agents-tests`](https://github.com/entireio/external-agents-tests)
2. **Lifecycle integration** — repo-local `e2e/` tests that exercise `entire enable`, prompt execution, checkpoints, and rewind
3. **Implementation** — build the binary until both layers pass, then add unit tests

### Getting Started — Zero Setup

Clone the repo and open it in your AI coding tool. Each tool auto-discovers the skill with no additional configuration:

| Tool | How it discovers | What to say |
|------|-----------------|-------------|
| **Claude Code** | `.claude/skills/` directory | `/entire-external-agent` |
| **Codex** | `AGENTS.md` at project root | "Build an external agent" |
| **Cursor** | `.cursor/rules/` directory | "Build an external agent" |
| **OpenCode** | `.opencode/plugins/` auto-loaded | "Build an external agent" |

The skill files live in `.claude/skills/entire-external-agent/` if you want to read the details.

## Testing

Testing is intentionally split:

- **Generic protocol checks** run in GitHub Actions via [`entireio/external-agents-tests`](https://github.com/entireio/external-agents-tests). The workflow builds each `entire-agent-*` binary in this repo and runs the shared compliance suite against it.
- **Lifecycle tests** stay in this repo's [`e2e/`](e2e/) harness. These verify the parts that depend on Entire itself and on the real agent CLI: prompt execution, hook installation after `entire enable`, checkpoint creation, rewind behavior, and interactive sessions.
- **Unit tests** live with each agent implementation under [`agents/`](agents/).

### Running Tests

```bash
# Run unit tests for all agents
mise run test-unit

# Run lifecycle integration tests from this repo
mise run test-e2e

# Same as test-e2e, kept as the explicit name
mise run test-e2e-lifecycle

# Run unit + lifecycle tests locally
mise run test-all
```

Protocol compliance runs in CI through [`.github/workflows/protocol-compliance.yml`](.github/workflows/protocol-compliance.yml).

### Lifecycle Harness Architecture

The lifecycle harness auto-discovers and builds all agents in `agents/` via `TestMain`:

| File | Purpose |
|------|---------|
| `e2e/setup_test.go` | `TestMain` entry point — discovers agents, builds binaries, configures PATH |
| `e2e/lifecycle_test.go` | Shared lifecycle scenarios run against every registered agent |
| `e2e/agents/` | Agent adapters for the real CLIs used during lifecycle tests |
| `e2e/entire/` | Entire CLI wrappers used by lifecycle assertions |
| `e2e/testutil/` | Repo setup, artifact capture, git helpers, and checkpoint assertions |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `E2E_ENTIRE_BIN` | Path to the `entire` binary (defaults to `entire` from PATH) |
| `E2E_AGENT` | Filter lifecycle runs to a single registered agent |
| `E2E_ARTIFACT_DIR` | Override lifecycle artifact output directory |
| `E2E_KEEP_REPOS` | Preserve temp repos for debugging |
| `E2E_CONCURRENT_TEST_LIMIT` | Override the per-agent lifecycle concurrency limit |

## Repository Layout

```
agents/                          # Standalone external agent projects
  entire-agent-kiro/             # Kiro agent (Go binary)
e2e/                             # Lifecycle integration harness
.github/workflows/               # CI, including protocol compliance via external-agents-tests
.claude/skills/entire-external-agent/  # Skill files (research, test-writer, implementer)
AGENTS.md                        # Codex auto-discovery
.cursor/rules/                   # Cursor auto-discovery
.opencode/plugins/               # OpenCode auto-discovery
```
