# entire-agent-dsh

External agent binary that teaches the [Entire CLI](https://docs.entire.com/) how
to capture [DeepSeek Harness (DSH)](https://github.com/deepseek-ai/deepseek-harness)
sessions.

DSH has no repository-local hook command, so capture runs through the
[`@sredevopsorg/dsh-entire`](https://github.com/sredevopsorg/dsh-plugins/tree/main/plugins/entire)
Host plugin. In a repository carrying `.entire/dsh-hooks.json`, that plugin
forwards DSH session events to `entire hooks dsh <hook>`. This binary resolves the
native DSH session log, normalizes it, and supplies transcripts, prompts,
modified files, token usage, and compact transcripts to Entire.

## Capabilities

| Capability | Status |
| --- | --- |
| `hooks` | Yes — via the DSH Host plugin marker |
| `transcript_analyzer` | Yes — prompts, file writes, summaries |
| `transcript_preparer` | Yes — decodes and materializes the native log |
| `token_calculator` | Yes — per-call assistant usage |
| `compact_transcript` | Yes — Entire Transcript Format v1 |

## Installation

```bash
cd agents/entire-agent-dsh
go build -o entire-agent-dsh ./cmd/entire-agent-dsh
cp entire-agent-dsh ~/.local/bin/
```

Or use mise:

```bash
cd agents/entire-agent-dsh
mise run build
```

## Prerequisites

- Entire CLI installed and on `PATH`.
- DSH with the `@sredevopsorg/dsh-entire` plugin loaded in the active profile
  (or the plugin installed from the repository's plugin index).
- `entire-agent-dsh` on `PATH` for the DSH Host process.
- `DSH_HOME` available to the Host (defaults to `~/.dsh`).
- `external_agents: true` in the repository's local Entire settings, which
  `entire enable --agent dsh` writes. Keep it in `.entire/settings.local.json`;
  Entire refuses the grant from the committed settings file.

## Enable

```bash
cd /path/to/your/repo
entire enable --agent dsh
```

This installs `.entire/dsh-hooks.json`, the marker the DSH Host plugin reads.
`uninstall-hooks` removes it again.

## How It Works

| DSH Host event | Entire hook | Protocol event |
| --- | --- | --- |
| `session/created` | `session-start` | SessionStart (1) |
| `user/message` with a prompt source | `turn-start` | TurnStart (2) |
| `turn/end` | `turn-end` | TurnEnd (3) |
| `compaction/end` | `compaction` | Compaction (4) |
| `session/disposed` | `session-end` | SessionEnd (5) |

On each hook the binary resolves the session's native
`$DSH_HOME/sessions/<project>/<session>/session.vN.jsonl.zstd`, decodes the
Zstandard frames, and refreshes a normalized JSONL cache under
`$DSH_HOME/entire/sessions/<project>/`. `turn-end` waits briefly for the expected
`turn/end` event so it does not race the Host's write-behind buffer.

The cache lives outside the repository and outside `.entire/tmp` on purpose:
other external agents use `.entire/tmp`, and nesting inside another agent's
session directory can misattribute sessions.

## Verify

```bash
# Unit tests
mise run test

# Protocol compliance (from a directory without a go.mod)
external-agents-tests verify ./entire-agent-dsh

# Real Entire lifecycle with a synthetic DSH session
bash scripts/verify-dsh.sh --entire "$(command -v entire)"
```

After enabling in a real repository, run DSH and check:

```bash
entire status
entire session list --json
entire checkpoint list
entire checkpoint explain CHECKPOINT_ID --short
```

## Limitations

- The DSH Host plugin is mandatory; this binary cannot install DSH hooks itself.
- The plugin looks for `.entire/dsh-hooks.json` in the session workspace, so DSH
  must be started with the repository root as its workspace. The plugin also
  resolves `entire`/`entire-agent-dsh` from the Host process `PATH`; restart the
  Host after PATH changes.
- The current bridge emits `turn-start` only for human/user/prompt sources, so
  scheduled and webhook-driven DSH turns are not captured until the bridge is
  widened.
- `session-end` depends on the Host emitting `session/disposed`; long-lived Web
  Hosts may leave sessions active until Entire's stale sweep.
- Subagent hooks are declared but not yet emitted by the bridge.
- DSH has no interactive `--resume`; `format-resume-command` returns the headless
  command that adopts a persisted session, and interactive resume is opening the
  session in `dsh web`. `entire session resume` re-frames the restored transcript
  into DSH's native store when no native generation exists; an existing live
  generation is never overwritten.
- Sessions larger than Entire's 10 MB subcommand stdout cap cannot be returned
  in one `read-transcript` call.

## Development

```bash
go build -o entire-agent-dsh ./cmd/entire-agent-dsh
go test ./...
gofmt -l .
go vet ./...
```

See [AGENT.md](AGENT.md) for the protocol mapping and the verified DSH storage
details.
