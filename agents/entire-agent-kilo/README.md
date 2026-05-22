# entire-agent-kilo

Standalone external agent binary that teaches the [Entire CLI](https://github.com/entireio/cli) how to work with [Kilo Code](https://github.com/Kilo-Org/kilocode).

## What it does

- Installs a project plugin at `.kilo/plugins/entire.ts` that subscribes to Kilo's `event` bus.
- Forwards five lifecycle hooks to the Entire CLI: `session-start`, `turn-start`, `turn-end`, `compaction`, `session-end`.
- Fetches the authoritative session JSON via the local Kilo SDK client (`@kilocode/sdk`) on turn-end and writes it to `session_ref` so Entire can checkpoint without polling.
- Maps Kilo's MessageV2 transcript into Entire's `AgentSessionJSON` envelope for compact transcripts, modified-file extraction, and token counting.

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

| Forwarded Hook  | Native Source                            | Protocol Event Type |
| --------------- | ---------------------------------------- | ------------------- |
| `session-start` | `session.created`                        | 1 = SessionStart    |
| `turn-start`    | `message.updated` / `message.part.updated` (user) | 2 = TurnStart |
| `turn-end`      | `session.status` (`status.type === "idle"`) | 3 = TurnEnd      |
| `compaction`    | `session.compacted`                      | 4 = Compaction      |
| `session-end`   | `session.deleted` / `server.instance.disposed` | 5 = SessionEnd |

## Build

```bash
mise run build
```

## Test

```bash
mise run test
```

## Install for use

```bash
go install ./cmd/entire-agent-kilo
# ensure ~/go/bin (or your GOBIN) is on PATH so `entire` discovers it
```

Then opt the project in:

```json
// .entire/settings.json
{ "external_agents": true }
```

And enable hooks:

```bash
entire enable --agent kilo
```

## Layout

```
cmd/entire-agent-kilo/   binary entrypoint
internal/kilo/           agent core (hooks, transcript, compact, types)
internal/protocol/       shared protocol handlers
scripts/                 helpers for local verification
```

## Resume command

`kilo run --session <id> "<prompt>"`

## Notes

- Kilo must be installed and on `PATH` as `kilo`.
- Set `KILO_PURE=1` to disable all external plugins, including this one.
- Sub-sessions (sessions where `info.parentID` is non-empty) are filtered out — only top-level sessions produce hook events.
