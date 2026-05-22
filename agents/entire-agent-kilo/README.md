# entire-agent-kilo

Standalone external agent binary that teaches the [Entire CLI](https://github.com/entireio/cli) how to work with [Kilo Code](https://github.com/Kilo-Org/kilocode).

## What it does

- Installs a project plugin at `.kilo/plugin/entire.ts` that subscribes to Kilo's `event` bus.
- On `session.created` and `session.idle`, fetches the authoritative session JSON via the local Kilo SDK client (`@kilocode/sdk`) and forwards it to the Entire CLI hook handler.
- Maps Kilo's MessageV2 transcript into Entire's `AgentSessionJSON` envelope for checkpointing, compact transcripts, modified-file extraction, and token counting.

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

| Native Event       | Protocol Event Type |
| ------------------ | ------------------- |
| `session.created`  | 1 = SessionStart    |
| `session.idle`     | 3 = TurnEnd         |

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
