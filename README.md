# external-agents

Repository for external agents and agent-building tooling that integrate with the Entire CLI through the external agent protocol.

See the protocol documentation in the Entire CLI repo:
[external-agent-protocol.md](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md).

## Layout

- `.claude/plugins/entire-external-agent/` contains a Claude plugin that helps users research, scaffold, implement, and validate external agents.
- `.claude/skills/entire-external-agent/` contains the matching skill files used by that plugin.
- `agents/` contains standalone external agent projects for the CLI.

This repo starts intentionally small so agent-specific structure can be added as the protocol and authoring workflow settle.
