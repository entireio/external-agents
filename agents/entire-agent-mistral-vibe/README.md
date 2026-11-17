# entire-agent-mistral-vibe

Standalone external agent binary that teaches the [Entire CLI](https://github.com/entireio/cli) how to work with [Mistral Vibe](https://github.com/mistralai/mistral-vibe), Mistral's coding CLI.

Requires Vibe 3.1.1 or later (the first release with `session_start`, `session_end`, `pre_agent` and `notification` hooks). Developed against 3.1.2.

## What it does

- Adds five hooks to the project's `.vibe/hooks.toml`, inside a marked block, leaving your own hooks alone.
- Maps them to Entire lifecycle events: session start, turn start, turn end, session end.
- Reads the conversation from Vibe's own session API (`vibe-app-server`, `session/read` and `session/history/list`) and stores it as one history entry per line. That gives Entire the prompt, tool calls, the assistant's reply and token usage. Reading needs no API key and does not resume the session.
- Converts that history to Entire's compact transcript format, and reports modified files and per-turn token usage.

Vibe's on-disk session store is a private recovery format, so this adapter never parses it.

## Capabilities

| Capability               | Declared |
| ------------------------ | -------- |
| hooks                    | true     |
| transcript_analyzer      | true     |
| transcript_preparer      | true     |
| compact_transcript       | true     |
| token_calculator         | true     |
| text_generator           | false    |
| hook_response_writer     | false    |
| subagent_aware_extractor | false    |

## Hook events

| Vibe hook (`type`, `match`)             | Forwarded as    | Protocol event   |
| --------------------------------------- | --------------- | ---------------- |
| `session_start`                         | `session-start` | 1 = SessionStart |
| `pre_agent`, `main`                     | `pre-agent`     | 2 = TurnStart    |
| `notification`, `turn_completed`        | `notification`  | 3 = TurnEnd      |
| `notification`, `turn_failed`           | `notification`  | 3 = TurnEnd      |
| `session_end`                           | `session-end`   | 5 = SessionEnd   |

Turn end comes from the `turn_completed` notification, not from `post_agent`: `post_agent` fires on a candidate answer that a hook can still reject, and the reply is not in the session history until the turn is accepted.

No tool hooks are installed. Tool calls are read from the session history, so Entire is not in the path of every tool call.

## Build

```bash
mise run build
```

## Test

```bash
mise run test
```

Protocol compliance, from a directory that is not a Go module:

```bash
external-agents-tests verify /path/to/entire-agent-mistral-vibe
```

Live lifecycle tests call the Mistral API and are opt-in. They run Vibe with a private `VIBE_HOME`, taking only `MISTRAL_API_KEY` from the environment or your `~/.vibe/.env`:

```bash
VIBE_E2E=1 E2E_AGENT=mistral-vibe mise run test:e2e
```

`VIBE_E2E_MODEL` overrides the model (default `mistral-medium-3.5`).

## Install for use

```bash
go install ./cmd/entire-agent-mistral-vibe
# ensure ~/go/bin (or your GOBIN) is on PATH so `entire` discovers it
```

Then opt the project in:

```json
// .entire/settings.json
{ "external_agents": true }
```

```bash
entire enable --agent mistral-vibe
```

Vibe only loads project hooks from a **trusted folder**. Trust the folder when Vibe asks, or pass `--trust` for a single run; until then the hooks do not fire and nothing is recorded.

## Where things are stored

- Hooks: `.vibe/hooks.toml`, between the `# >>> entire hooks` and `# <<< entire hooks <<<` markers.
- Transcripts: `<user cache dir>/entire/mistral-vibe/<repo hash>/<session id>.jsonl` (`~/Library/Caches` on macOS, `~/.cache` on Linux). They are not kept under `.entire/tmp`, because Entire attributes a transcript to the first agent whose session directory contains it and other agents claim that directory.

## Limitations

- **Resume is local.** `vibe --resume <id>` works only where Vibe still has the session. Restoring a checkpoint on another machine restores Entire's transcript, not a Vibe session.
- **Interrupted or crashed turns** produce no turn-end event. They are closed by the next turn or by session end, and a crash produces neither.
- **Custom `VIBE_HOME`.** Entire refreshes the transcript again at commit time. If Vibe ran with a `VIBE_HOME` that your shell does not export, that refresh cannot find the session and Entire uses the transcript stored at turn end.
- **Shell-made changes.** Modified files are taken from Vibe's file tools. Files changed by a shell command are picked up by Entire from git, not from the transcript.
- **Subagents** are recorded as part of the parent's turn; their own history is not captured separately.
- **Compaction** has no Vibe hook, so Entire receives no compaction event.

See [AGENT.md](AGENT.md) for the research behind these choices and what was verified against a live Vibe.
