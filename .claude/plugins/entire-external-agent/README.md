# External Agent Plugin

Local plugin providing individual commands for building standalone external agent binaries that implement the Entire CLI's external agent protocol.

## How This Differs from `agent-integration`

| | `agent-integration` | `entire-external-agent` |
|---|---|---|
| **Output** | Built-in agent integration inside the Entire CLI codebase | Standalone binary in any language |
| **Protocol** | Direct Go interface implementation | Subcommand-based protocol over stdin/stdout |
| **Testing** | Uses CLI's built-in `ForEachAgent` E2E framework | Self-contained `e2e/` test harness in the agent project |
| **Audience** | Contributors implementing built-in Entire CLI agents | Internal or external developers building agent plugins |
| **Language** | Go only | Go, Python, TypeScript, Rust |

## Commands

| Command | Description |
|---------|-------------|
| `/entire-external-agent:research` | Research a target agent's capabilities and map them to the external agent protocol |
| `/entire-external-agent:write-tests` | Scaffold the binary and create an e2e test harness |
| `/entire-external-agent:implement` | Implement real logic using E2E-first TDD (unit tests last) |

## Related

- Orchestrator skill: `.claude/skills/entire-external-agent/SKILL.md` (`/entire-external-agent` -- runs research, write-tests, then implement)

## Standalone Usage

This plugin can be used outside the Entire CLI repo. The skills should use the protocol spec at:
`https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md`

```bash
claude --plugin-dir /path/to/entire-external-agent/
```
