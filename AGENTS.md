# External Agent Builder

This repository includes a skill that guides you through building standalone external agent binaries for the [Entire CLI](https://github.com/entireio/cli). The skill uses E2E-first TDD and runs in three phases.

## Available Commands

| Command | Skill file | Description |
|---------|-----------|-------------|
| Full pipeline | `.claude/skills/entire-external-agent/SKILL.md` | Run all three phases sequentially |
| Research | `.claude/skills/entire-external-agent/research.md` | Analyze the target agent's capabilities and map to the protocol |
| Write tests | `.claude/skills/entire-external-agent/write-tests.md` | Scaffold the binary and create E2E test harness |
| Implement | `.claude/skills/entire-external-agent/implement.md` | Build the binary using E2E-first TDD (unit tests last) |

## How to Use

When the user asks to "build an external agent", "create an agent binary", or "external agent plugin":

1. Read `.claude/skills/entire-external-agent/SKILL.md` for the full pipeline overview
2. Follow the three phases in order: research, write-tests, implement
3. Each phase has a dedicated skill file with detailed instructions

## Tool Mapping (Codex)

The skill files reference Claude Code tool names. Map them to Codex equivalents:

| Skill references | Codex equivalent |
|-----------------|-----------------|
| `TodoWrite` | `update_plan` |
| `Task` / subagent launches | Codex's native subagent system |
| `Skill` tool | Read the skill file directly |
| `Read`, `Write`, `Edit`, `Glob`, `Grep`, `Bash` | Native Codex tools |

## Protocol Spec

The external agent protocol is defined at:
`https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md`
