# External Agent Plugin

Build standalone external agent binaries that implement the Entire CLI's external agent protocol. These are standalone executables (`entire-agent-<name>`) that the CLI discovers on `$PATH` and communicates with via subcommands and JSON over stdin/stdout.

## How This Differs from `agent-integration`

| | `agent-integration` | `entire-external-agent` |
|---|---|---|
| **Output** | Built-in Go package in `cmd/entire/cli/agent/` | Standalone binary in any language |
| **Protocol** | Direct Go interface implementation | Subcommand-based protocol over stdin/stdout |
| **Audience** | Internal contributors to the Entire CLI repo | Internal or external developers building agent plugins |
| **Language** | Go only | Go, Python, TypeScript, Rust |

## Commands

| Command | Description |
|---------|-------------|
| `/entire-external-agent:research` | Research a target agent's capabilities and map them to the external agent protocol |
| `/entire-external-agent:scaffold` | Generate a project skeleton for a new external agent binary |
| `/entire-external-agent:implement` | Implement real logic for each subcommand |
| `/entire-external-agent:validate` | Run conformance tests against the built binary |

## Orchestrator

Run `/entire-external-agent` to execute all 4 phases sequentially (research, scaffold, implement, validate) with shared parameters.

See `.claude/skills/entire-external-agent/SKILL.md` for the orchestrator procedure.

## Standalone Usage

This plugin can be used outside the Entire CLI repo. When `docs/architecture/external-agent-protocol.md` is not found, the skills will ask for the protocol spec location (a URL, file path, or repo path) and adapt accordingly.

```bash
claude --plugin-dir /path/to/entire-external-agent/
```
