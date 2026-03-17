# external-agents

Repository for external agents and agent-building tooling that integrate with the Entire CLI through the external agent protocol.

See the protocol documentation in the Entire CLI repo:
[external-agent-protocol.md](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md).

## Installation

### Claude

Use the existing standalone Claude plugin directory:

```bash
claude --plugin-dir /path/to/external-agents/.claude/plugins/entire-external-agent
```

### Codex

Tell Codex:

```text
Fetch and follow instructions from https://raw.githubusercontent.com/obra/external-agents/main/.codex/INSTALL.md
```

This installs native Codex skill discovery by symlinking the shared `.claude/skills` directory.

### OpenCode

Tell OpenCode:

```text
Fetch and follow instructions from https://raw.githubusercontent.com/obra/external-agents/main/.opencode/INSTALL.md
```

This installs a small bootstrap plugin and symlinks the shared `.claude/skills` directory.

### Cursor

This repo includes a Cursor plugin manifest at `.cursor-plugin/plugin.json` that points Cursor at the shared skill and command directories.

## Layout

- `.claude/plugins/entire-external-agent/` contains a Claude plugin that helps users research, scaffold, implement, and validate external agents.
- `.claude/skills/entire-external-agent/` contains the matching skill files used by that plugin.
- `.codex/INSTALL.md` documents Codex native skill discovery setup.
- `.opencode/` contains the OpenCode installation guide and bootstrap plugin.
- `.cursor-plugin/plugin.json` contains the Cursor plugin manifest.
- `agents/` contains standalone external agent projects for the CLI.

This repo starts intentionally small so agent-specific structure can be added as the protocol and authoring workflow settle.
