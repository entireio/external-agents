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

## Building a New External Agent

This repo includes a skill that guides you through building a new external agent using an E2E-first TDD pipeline. The skill runs in three phases:

1. **Research** — analyzes the target AI agent's file formats, session layout, and hook mechanisms
2. **Write tests** — generates E2E and unit tests against the external agent protocol
3. **Implement** — builds the Go binary to pass all tests

To use it:

1. Clone this repo
2. Install the plugin for your editor (see [Editor Plugin Installation](#editor-plugin-installation) below)
3. Run `/entire-external-agent` and follow the prompts

The skill files live in `.claude/skills/entire-external-agent/` if you want to read the details.

## Editor Plugin Installation

### Claude Code

```bash
claude mcp add-from-claude-desktop  # if using Claude Desktop MCP servers
claude --plugin-dir /path/to/external-agents/.claude/plugins/entire-external-agent
```

### Codex

Tell Codex:

```text
Fetch and follow instructions from https://raw.githubusercontent.com/obra/external-agents/main/.codex/INSTALL.md
```

### OpenCode

Tell OpenCode:

```text
Fetch and follow instructions from https://raw.githubusercontent.com/obra/external-agents/main/.opencode/INSTALL.md
```

### Cursor

This repo includes a Cursor plugin manifest at `.cursor-plugin/plugin.json` that points Cursor at the shared skill and command directories.

## Repository Layout

```
agents/                          # Standalone external agent projects
  entire-agent-kiro/             # Kiro agent (Go binary)
.claude/plugins/                 # Claude Code plugin for building agents
.claude/skills/entire-external-agent/  # Skill files (research, test-writer, implementer)
.codex/                          # Codex installation guide
.opencode/                       # OpenCode installation guide + bootstrap plugin
.cursor-plugin/                  # Cursor plugin manifest
```
