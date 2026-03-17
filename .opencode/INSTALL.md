# Installing External Agent Skills for OpenCode

## Prerequisites

- OpenCode installed
- Git installed

## Installation

### 1. Clone the repository

```bash
git clone https://github.com/entireio/external-agents.git ~/.config/opencode/external-agents
```

### 2. Register the plugin

```bash
mkdir -p ~/.config/opencode/plugins
rm -f ~/.config/opencode/plugins/entire-external-agent.js
ln -s ~/.config/opencode/external-agents/.opencode/plugins/entire-external-agent.js ~/.config/opencode/plugins/entire-external-agent.js
```

### 3. Symlink the shared skill directory

```bash
mkdir -p ~/.config/opencode/skills
rm -rf ~/.config/opencode/skills/external-agents
ln -s ~/.config/opencode/external-agents/.claude/skills ~/.config/opencode/skills/external-agents
```

### 4. Restart OpenCode

Restart OpenCode so it discovers the plugin and the new skill directory.

## Verify

```bash
ls -l ~/.config/opencode/plugins/entire-external-agent.js
ls -l ~/.config/opencode/skills/external-agents
```

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
cd ~/.config/opencode/external-agents && git pull
```
