# Installing External Agent Skills for OpenCode

## Prerequisites

- OpenCode installed

Run these commands from the repository root:

## Installation

```bash
mkdir -p ~/.config/opencode/plugins ~/.config/opencode/skills
ln -sf "$(pwd)/.opencode/plugins/entire-external-agent.js" ~/.config/opencode/plugins/entire-external-agent.js
ln -sf "$(pwd)/.claude/skills" ~/.config/opencode/skills/external-agents
```

Restart OpenCode so it discovers the plugin and skill directory.

## Verify

```bash
ls -l ~/.config/opencode/plugins/entire-external-agent.js
ls -l ~/.config/opencode/skills/external-agents
```

Both should be symlinks pointing into this repository.

## Usage

Use OpenCode's native `skill` tool to load `external-agents/entire-external-agent` when a user wants to build or validate an Entire CLI external agent.

## Tool Mapping

When the skill references Claude-oriented tools, map them to OpenCode equivalents:

- `TodoWrite` -> `update_plan`
- `Task` or subagents -> OpenCode's subagent system
- `Skill` tool -> OpenCode's native `skill` tool
- file and shell operations -> native OpenCode tools

## Updating

```bash
git pull
```
