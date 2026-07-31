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
| [Amp](agents/entire-agent-amp/) | `agents/entire-agent-amp/` | Implemented — hooks + transcript analysis + token calculation + compact transcripts |
| [Qwen Code](agents/entire-agent-qwen/) | `agents/entire-agent-qwen/` | Implemented — hooks + transcript analysis + compact transcripts |
| [Oh My Pi](agents/entire-agent-omp/) | `agents/entire-agent-omp/` | Implemented — hooks + transcript analysis + compact transcripts |
| [Kilo](agents/entire-agent-kilo/) | `agents/entire-agent-kilo/` | Implemented (preview) — hooks + transcript analysis + token calculation + compact transcripts |

See each agent's own README for setup and usage instructions.

### Native agent notes

The Entire CLI integrates several AI coding agents natively; see [entireio/cli](https://github.com/entireio/cli) for the current list. Known gaps in native integrations that surface to contributors here:

- **Cursor inside JetBrains via ACP is not captured.** JetBrains AI Chat's Agent Client Protocol (which Cursor [joined 2026-03-04](https://blog.jetbrains.com/ai/2026/03/cursor-joined-the-acp-registry-and-is-now-live-in-your-jetbrains-ide/)) does not invoke the `.cursor/hooks.json` hooks Entire installs. Workaround for JetBrains workflows: use standalone Cursor IDE or the `cursor agent` CLI. Tracking: [#18](https://github.com/entireio/external-agents/issues/18).

## Enabling External Agents

External agent discovery is opt-in. Once an `entire-agent-<name>` binary is on your `PATH`, set `external_agents: true` in the repo's `.entire/settings.json` so Entire scans for it:

```json
{
  "external_agents": true
}
```

Without this flag, Entire ignores external agent binaries even when they're installed.

### Qwen Code

Qwen support targets the terminal `qwen` coding agent:

```bash
cd agents/entire-agent-qwen
mise run build
cp entire-agent-qwen /usr/local/bin/

cd /path/to/your/repo
entire enable --agent qwen --telemetry=false
qwen -p "Create hello.txt with hello world" --yolo
```

The adapter installs Qwen command hooks in `.qwen/settings.json`. The stable Entire sidecar transcript lives in a repo-scoped OS temp directory, with a small `.entire/tmp/<session>.json` marker for Entire session discovery. Qwen Code must execute actual tools for checkpoints; local model backends that only print XML-style tool tags as text will not fire Qwen `PostToolUse` hooks.

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
- **Agent unit/build checks** run in GitHub Actions for every `agents/entire-agent-*` directory discovered at runtime.
- **Lifecycle tests** stay in this repo's [`e2e/`](e2e/) harness. These verify the parts that depend on Entire itself and on the real agent CLI: prompt execution, hook installation after `entire enable`, checkpoint creation, rewind behavior, and interactive sessions.
- **Unit tests** live with each agent implementation under [`agents/`](agents/).

### Running Tests

```bash
# Run default test suite for all agents
mise run test

# Same as test, kept for explicit CI-style naming
mise run test:ci

# Run lifecycle integration tests from this repo
mise run test:e2e

# Same as test:e2e, kept as the explicit name
mise run test:e2e:lifecycle
```

Protocol compliance runs in CI through [`.github/workflows/protocol-compliance.yml`](.github/workflows/protocol-compliance.yml).

For a newly added agent to be picked up automatically by local root tasks and CI, add `agents/entire-agent-<name>/mise.toml` with `build` and `test` tasks. The shared runner falls back to Go defaults when `go.mod` exists, but the `mise` tasks are the contract for non-standard setups.

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
| `QWEN_E2E` | Set to `1` with `E2E_AGENT=qwen` to run live Qwen Code lifecycle tests |

## Repository Layout

```
agents/                          # Standalone external agent projects
  entire-agent-kiro/             # Kiro agent (Go binary)
  entire-agent-amp/              # Amp agent (Go binary)
  entire-agent-qwen/             # Qwen Code agent (Go binary)
  entire-agent-omp/              # Oh My Pi agent (Go binary)
  entire-agent-kilo/             # Kilo agent (Go binary)
e2e/                             # Lifecycle integration harness
.github/workflows/               # CI, including protocol compliance via external-agents-tests
.claude/skills/entire-external-agent/  # Skill files (research, test-writer, implementer)
AGENTS.md                        # Codex auto-discovery
.cursor/rules/                   # Cursor auto-discovery
.opencode/plugins/               # OpenCode auto-discovery
```
