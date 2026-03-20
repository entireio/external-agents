# Installing External Agent Skills for Codex

Enable the `entire-external-agent` skill in Codex via native skill discovery.

Run these commands from the repository root:

## Installation

```bash
mkdir -p ~/.agents/skills
ln -sf "$(pwd)/.claude/skills" ~/.agents/skills/external-agents
```

Restart Codex so it discovers the new skill directory.

## Verify

```bash
ls -la ~/.agents/skills/external-agents
```

You should see a symlink pointing at this repository's `.claude/skills` directory.

## Usage

Ask Codex to use the `entire-external-agent` skill when you want to research, scaffold, implement, or validate an Entire CLI external agent.

## Updating

```bash
git pull
```

## Uninstalling

```bash
rm ~/.agents/skills/external-agents
```
