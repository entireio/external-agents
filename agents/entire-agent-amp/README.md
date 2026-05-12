# entire-agent-amp

External agent binary that teaches the Entire CLI how to work with Amp.

## Capabilities

| Capability          | Status                                                            |
| ------------------- | ----------------------------------------------------------------- |
| hooks               | Yes, project plugin in `.amp/plugins/entire.ts`                   |
| transcript_analyzer | Yes, parses prepared `amp threads export` JSON                  |
| transcript_preparer | Yes, exports `amp threads export <thread-id>` JSON to session ref |
| compact_transcript  | Yes, emits Entire compact transcript JSONL for checkpoints v2     |
| token_calculator    | Yes, sums token usage from exported Amp thread JSON               |

## Installation

```bash
cd agents/entire-agent-amp
go build -o entire-agent-amp ./cmd/entire-agent-amp
cp entire-agent-amp /usr/local/bin/
```

Or use mise:

```bash
cd agents/entire-agent-amp
mise run build
```

## Prerequisites

- Amp CLI installed as `amp` on `PATH`.
- Amp must be launched with `PLUGINS=all` for project plugins to load except for users using the [Neo](https://ampcode.com/news/neo) version of Amp, which supports project plugins without the env var.

## How It Works

`install-hooks` creates `.amp/plugins/entire.ts`. The plugin forwards Amp lifecycle events to `entire hooks amp <event>`:

| Amp Event                                                         | Protocol Event        |
| ----------------------------------------------------------------- | --------------------- |
| synthetic `session.start` before first `agent.start` for a thread | SessionStart (type 1) |
| `agent.start`                                                     | TurnStart (type 2)    |
| `agent.end`                                                       | TurnEnd (type 3)      |

Amp threads are server-side, so hook payloads are cached as JSONL only long enough to identify the thread. When Entire calls `prepare-transcript`, the binary replaces that cache with the JSON output from `amp threads export <thread-id>`. That exported JSON is the authoritative payload for session reads, transcript analysis, compaction, and token calculation.

## Development

```bash
go build -o entire-agent-amp ./cmd/entire-agent-amp
go test ./...
external-agents-tests verify ./entire-agent-amp

cd ../../
E2E_AGENT=amp mise run test:e2e
```
