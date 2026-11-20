# entire-agent-vibe

External agent that lets the Entire CLI work with **Mistral Vibe Code** (VS Code extension / Vibe CLI, by Mistral AI) by reading Vibe's on-disk session journal.

## Installation

The agent is a single-file Python 3.8+ script with no third-party dependencies.

```bash
mise run build
cp entire-agent-vibe ~/.local/bin/
entire enable --agent vibe --telemetry=false
```

That puts `entire-agent-vibe` on `PATH`, which Entire discovers when external agents are enabled.

On Windows, use a tiny `.cmd` wrapper instead of the bash launcher:

```bat
@echo off
python "%~dp0entire_agent_vibe.py" %*
```

Save it as `entire-agent-vibe.cmd` in a directory on `PATH` (for example `%USERPROFILE%\.local\bin`).

## Enable In A Repo

Vibe declares project hooks in `.vibe/hooks.toml`. `install-hooks` appends a managed `post_agent` block to it:

```bash
echo '{}' | entire-agent-vibe install-hooks
entire-agent-vibe are-hooks-installed
```

## Usage

The binary speaks Entire's external-agent protocol: JSON on stdout, hook payloads and session JSON on stdin.

```bash
entire-agent-vibe info
entire-agent-vibe detect
entire-agent-vibe prepare-transcript --session-ref <session-dir>
entire-agent-vibe compact-transcript --session-ref <prepared-markdown>
```

## How It Works

- **Hooks.** Vibe's `post_agent` hook fires at turn end; `parse-hook` maps it to Entire's TurnEnd lifecycle event so the CLI stores a checkpoint.
- **Session storage.** Vibe journals each session under `~/.vibe/logs/session/unified/<session-uuid>/journal/*.jsonl`. The journal is a rolling window: Vibe compacts and deletes older chunks, so transcripts are captured at turn end.
- **Quiescence wait.** The `post_agent` hook fires *before* the turn's final assistant message is flushed to the journal. `prepare-transcript` polls the journal until its size stops growing, so the response the user just saw is included.
- **Two-source merge.** `compact-transcript` merges the prepared Markdown transcript (full history) with the live journal (complete recent tail) and emits Entire's compact transcript JSONL.

## Capabilities

| Capability | Status |
| ---------- | ------ |
| hooks | Yes, Vibe `post_agent` mapped to Entire TurnEnd via `entire hooks vibe post_agent` |
| transcript_analyzer | Yes, replays Vibe projection deltas into a readable Markdown transcript |
| transcript_preparer | Yes, `prepare-transcript` converts journal chunks to Markdown |
| token_calculator | Yes, per-turn deltas from Vibe's cumulative session tokenUsage |
| compact_transcript | Yes, merged Markdown + journal compact JSONL |

## Troubleshooting

Set `VIBE_AGENT_DEBUG=1` to append every subcommand invocation and result to `~/vibe-agent-debug.log`.

## Development

```bash
mise run test
mise run clean
```

The unit tests run the script as a subprocess with `HOME` pointed at a temp directory and synthetic journals, so neither a Vibe installation nor LLM credentials are needed.
