# entire-agent-sarvam

External agent integration that teaches the [Entire CLI](https://github.com/entireio/cli) to work with [Sarvam Code](https://sarvam.ai).

Sarvam Code keeps its own durable session transcripts (rollout JSONL under
`$SARVAM_HOME/sessions/`), so Entire checkpoints attach real conversation
content, and `token_count` events feed token usage per checkpoint.

## Capabilities

| Capability | Status |
|-----------|--------|
| `hooks` | yes — project `.sarvam/config.toml`, five lifecycle events |
| `transcript_analyzer` | yes — native rollout JSONL |
| `compact_transcript` | yes — projects the rollout into Entire Transcript Format |
| `transcript_format` (info field) | `codex` — CLIs that know it convert the rollout with their Codex converter, which adds per-call model and usage and exit codes; older CLIs keep using `compact_transcript` |
| `token_calculator` | yes — per-call usage from `token_count` events |
| `uses_terminal` | yes — Sarvam Code is a TUI agent |
| `text_generator` / `hook_response_writer` / `subagent_aware_extractor` / `transcript_preparer` | no |

Events installed: `SessionStart`, `UserPromptSubmit`, `Stop`,
`PostCompact`. Tool and permission events are not installed: Entire has no
event type for them, and the native transcript already records tool calls.
`PreCompact` is not installed either: compaction is reported once, from
`PostCompact`, which fires after the append-only rollout has recorded the
compacted history — a config still carrying a legacy pre-compact entry
parses to a no-op.

## Build and install

```bash
cd agents/entire-agent-sarvam
mise run build
cp entire-agent-sarvam ~/.local/bin/
```

## Enable

```bash
cd /path/to/your/repo
entire agent add sarvam
```

`install-hooks` writes the hook declarations into the repo's
`.sarvam/config.toml`. Two sarvam-side gates then apply, both one-time per
repository:

1. **Directory trust.** Sarvam ignores a project config until the directory
   is trusted. The first interactive launch prompts for it — or a single
   headless run (`sarvam-code exec …`) records trust automatically.
2. **Hook review.** Freshly installed hooks must be trusted and enabled in
   the TUI: open sarvam-code, run `/hooks`, and for each `entire-*` hook
   press `t` (trust) then space (enable). Until then the hooks do not fire
   and Entire records no checkpoints.

Hooks fire in **interactive sessions only**. `sarvam-code exec` (headless)
never invokes hooks — checkpoints and transcripts are captured from TUI
sessions.

## Session snapshots

Entire captures session snapshots with `write-session`. A native rollout path
is only ever created when it does not exist yet — Entire restoring a captured
snapshot onto a fresh clone — and an existing rollout is never touched. On a
fresh clone the CLI resolves the plain `<id>.jsonl` fallback, so the write is
redirected to the rollout-shaped path derived from the snapshot's own
session_meta timestamp, which is what `sarvam-code resume` scans for.

## Testing

```bash
cd agents/entire-agent-sarvam
mise run test                              # unit tests
external-agents-tests verify ./entire-agent-sarvam   # protocol compliance

cd /path/to/repo
SARVAM_HOME=$(mktemp -d) E2E_AGENT=sarvam mise run test:e2e:lifecycle  # lifecycle (needs sarvam-code + tmux)
```

The lifecycle adapter drives the interactive TUI through tmux: it trusts the
temp repo with a headless exec run, automates the one-time `/hooks` review,
and sends prompts like a user would. It requires a logged-in `sarvam-code`.

The suite writes directory trust and hook state into sarvam's user config,
so it must run against a disposable `SARVAM_HOME`; `Bootstrap` fails loudly
when it is unset or points at the real `~/.sarvam`. sarvam-code keeps its
credentials in `$SARVAM_HOME/keyring`, so `Bootstrap` copies the real keyring
into the disposable home and verifies `sarvam-code login status` before any
test runs.

See `AGENT.md` for the full research mapping (hook payload schemas, session
file format, trust model) verified against sarvam-code 0.58.0.
