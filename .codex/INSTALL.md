# Installing External Agent Skills for Codex

Enable the `entire-external-agent` skill in Codex via native skill discovery.

## Prerequisites

- Git

## Installation

1. **Clone the repository:**
   ```bash
   git clone https://github.com/obra/external-agents.git ~/.codex/external-agents
   ```

2. **Create the skills symlink:**
   ```bash
   mkdir -p ~/.agents/skills
   ln -s ~/.codex/external-agents/.claude/skills ~/.agents/skills/external-agents
   ```

   **Windows (PowerShell):**
   ```powershell
   New-Item -ItemType Directory -Force -Path "$env:USERPROFILE\.agents\skills"
   cmd /c mklink /J "$env:USERPROFILE\.agents\skills\external-agents" "$env:USERPROFILE\.codex\external-agents\.claude\skills"
   ```

3. **Restart Codex** so it discovers the new skill directory.

## Verify

```bash
ls -la ~/.agents/skills/external-agents
```

You should see a symlink or junction pointing at `~/.codex/external-agents/.claude/skills`.

## Usage

Ask Codex to use the `entire-external-agent` skill when you want to research, scaffold, implement, or validate an Entire CLI external agent.

## Updating

```bash
cd ~/.codex/external-agents && git pull
```

## Uninstalling

```bash
rm ~/.agents/skills/external-agents
```
