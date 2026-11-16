# DeepSeek Harness (DSH) — External Agent Research

## Verdict: COMPATIBLE

DSH keeps an append-only, Zstandard-compressed session log for every session. It has
no repository-local hook command, so this integration pairs with the
[`@sredevopsorg/dsh-entire`](https://github.com/sredevopsorg/dsh-plugins/tree/main/plugins/entire)
Host plugin, which forwards DSH Host events to `entire hooks dsh <hook>`. The
binary in this directory implements the Entire side of that contract.

The mapping was verified against a live DSH 0.2.2-rc.1 session on disk and against
Entire CLI 0.12.2 end to end (session, checkpoint, `Entire-Checkpoint` trailer,
compact transcript, token usage).

## Static Checks

| Check | Result | Notes |
| --- | --- | --- |
| DSH installed | PASS | Host runs `node .../dsh web`; `$DSH_HOME` is set today |
| Help available | PASS | `dsh --help`, `dsh headless --help` |
| Hook keywords | PASS | No native project hook file; the Host plugin provides the bridge |
| Session keywords | PASS | `sessions`, `--session-id`, `dsh web` session list |
| Config directory | PASS | `$DSH_HOME` (default `~/.dsh`) |
| Documentation | PASS | `docs.entire.com` protocol; DSH session-format packages on disk |

## Binary

- Name: `dsh` (the Host/CLI is a Node application; sessions are written by the Host)
- Integration binary: `entire-agent-dsh`
- Install: build with `go build -o entire-agent-dsh ./cmd/entire-agent-dsh`, put it on `PATH`
- Prerequisite: the active DSH profile must load `@sredevopsorg/dsh-entire`

## Hook Mechanism

- Config file: `.entire/dsh-hooks.json` (the marker the Host plugin checks)
- Config format: JSON marker; the Host plugin reads only its presence
- Hook registration: the DSH Host plugin subscribes to `session/created`,
  `session/event`, and `session/disposed` and runs `entire hooks dsh <hook>`
- Hook payload (JSON on stdin):

```json
{
  "hook_type": "turn_end",
  "session_id": "session-…",
  "timestamp": "2026-11-05T01:22:12Z",
  "raw_data": {"dsh": {"turn": 1, "reason": "completed"}}
}
```

| DSH Host event | Hook | Protocol Event |
| --- | --- | --- |
| `session/created` | `session-start` | 1 SessionStart |
| `user/message` with a prompt source | `turn-start` | 2 TurnStart |
| `turn/end` | `turn-end` | 3 TurnEnd |
| `compaction/end` | `compaction` | 4 Compaction |
| `session/disposed` | `session-end` | 5 SessionEnd |
| subagent activity (declared, not yet emitted by the bridge) | `subagent-start` / `subagent-end` | 6 / 7 |

The bridge currently emits `turn-start` only for `source.kind` in
`{human, user, prompt}`. Scheduled, webhook, team-message and
`dsh-superagent` turns are filtered by the bridge before they reach this
binary. That is a bridge limitation, not a protocol limitation.

## Session Management

- Root: `$DSH_HOME/sessions` (default `~/.dsh/sessions`)
- Project directory: `--<cwd with separators -> '-'>--` (DSH `projectKey`)
- Session directory: DSH-escaped session ID (safe units literal, others `~XXXX`)
- Lookups and cache keys resolve symlinks to the physical checkout path
- Log: highest `session.vN.jsonl.zstd` generation in that directory
  (plain `session.vN.jsonl` when compression is disabled)
- Header line (first decompressed line):
  `{"type":"session","version":4,"id":"…","createdAt":<ms>,"cwd":"…","isSeeded":false,"delegationDepth":0,…}`
- Event lines: `{"type":"…","seq":<n>,"time":<ms>,"data":{…},…}`
- Session ID source: `session_id` from the bridge payload
- Materialized transcript: `$DSH_HOME/entire/sessions/<projectKey>/<safe-id>.jsonl`

The materialized cache is global and repository-scoped on purpose: another
external agent (Kiro) owns `.entire/tmp`, and nesting inside it risks
misattributing sessions.

## Transcript

- Native format: concatenated Zstandard frames containing JSONL. The first
  frame is the session header; later frames are events.
- Decoding: concatenated frames are decoded in order; a torn final frame keeps
  the complete-line prefix, so a hook racing the Host write-behind buffer does
  not fail.
- User prompts: `user/message.data.content` text blocks; `data.source.kind` and
  `data.source.form` distinguish prompts from injected context.
- Assistant output: `assistant/message.data.message.content` (`text`, `reasoning`,
  `tool-call`).
- Tool calls: `assistant/message` `tool-call` blocks and `tool/call` events;
  results arrive as `tool/result` events and are inlined by `callId`.
- Token usage: `assistant/message.data.usage` with
  `inputTokens`, `outputTokens`, `cacheReadTokens`, `cacheWriteTokens`.
- Timestamps: `time` and `createdAt` are Unix milliseconds.
- Whole-session file sizes can exceed the CLI's 10 MB stdout cap; the integration
  returns raw bytes and does not silently truncate.

## Data Storage Verification

- Session files contain actual assistant text and tool calls (verified against a
  live session; no placeholder-only content).
- `user/message` source kinds observed: `user`, `runtime-context`,
  `skill-catalog`, `time-context`, `agent-instructions`; the context kinds carry
  a `form` and are excluded from prompts/compact user lines.
- `assistant/message` carries usage and model identity on every model call.
- Hook data flow is the Host plugin → `entire hooks dsh` path; there is no
  native DSH hook file to inspect.
- Verification method: decoded the live `session.v4.jsonl.zstd`, inspected event
  types/shapes, then drove Entire CLI with the same payload shape in a throwaway
  repository.

## Protocol Mapping

| Subcommand | Native Concept | Implementation Notes |
| --- | --- | --- |
| `info` | adapter metadata | `dsh`, 7 hook names, capabilities below |
| `detect` | DSH installation | `$DSH_HOME`, `dsh` on `PATH`, or `~/.dsh` |
| `get-session-id` | bridge `session_id` | payload session ID |
| `get-session-dir` | Entire cache tree | `$DSH_HOME/entire/sessions/<projectKey>` |
| `resolve-session-file` | cache path | `<dir>/<safe-session-id>.jsonl` |
| `read-session` | cache transcript | reads the cache or materializes the native log |
| `write-session` | cache + native restore | writes the cache and, when absent, re-frames a native DSH generation for `--session-id` |
| `read-transcript` | native/cache transcript | decodes zstd; returns raw JSONL |
| `chunk-transcript` | raw bytes | base64 chunks via protocol JSON |
| `reassemble-transcript` | raw bytes | reverse of chunking |
| `format-resume-command` | DSH resume | `dsh --profile headless --session-id <id> "continue"` |
| `parse-hook` | bridge payload | maps hooks 1–7, materializes the cache, waits for the turn boundary |
| `install-hooks` | marker | writes `.entire/dsh-hooks.json` |
| `uninstall-hooks` | marker | removes it |
| `are-hooks-installed` | marker | presence check |
| `get-transcript-position` | JSONL lines | non-blank line count |
| `extract-modified-files` | tool history | `write`, `edit`, `str_replace_editor`, `apply_patch` |
| `extract-prompts` | user messages | prompt sources only; context forms excluded |
| `extract-summary` | assistant text | last non-empty assistant text |
| `prepare-transcript` | cache refresh | materializes the native log before reading |
| `compact-transcript` | Entire Transcript Format | user/assistant lines, tool results inlined, per-call usage |
| `calculate-tokens` | assistant usage | sums usage after the line offset |

## Selected Capabilities

| Capability | Declared | Justification |
| --- | --- | --- |
| `hooks` | true | Host plugin forwards lifecycle events |
| `transcript_analyzer` | true | Session log carries prompts, tools, and assistant text |
| `transcript_preparer` | true | Native log must be decoded/materialized before reading |
| `token_calculator` | true | `assistant/message.usage` is authoritative per call |
| `compact_transcript` | true | DSH events map cleanly to Entire Transcript Format |
| `text_generator` | false | DSH is used for agent execution, not summary generation |
| `hook_response_writer` | false | The Host plugin renders its own messages |
| `subagent_aware_extractor` | false | The bridge does not emit subagent hooks yet |

## Gaps & Limitations

- No native repository hook file: the DSH Host plugin is required for capture.
- The plugin checks `.entire/dsh-hooks.json` relative to the session workspace
  (`header.cwd`), not the git root. Start DSH with the repository root as its
  workspace, or install the marker in the workspace DSH actually uses.
- The plugin's `entire` and `entire-agent-dsh` lookups use the Host process
  `PATH`; restart the Host after changing its PATH.
- The bridge filters machine-originated `turn-start` events; scheduled/webhook
  DSH sessions are not captured until the bridge widens its source-kind set.
- `session-end` fires only on `session/disposed`, which long-lived Web Hosts may
  not emit; sessions may remain active until Entire's stale sweep.
- `subagent-start` / `subagent-end` are declared for forward compatibility but
  the bridge does not emit them.
- DSH has no interactive `--resume`; the only shipped headless app adopts a
  session via `--session-id`, and interactive resume is opening the session in
  `dsh web`. `entire session resume` re-frames the restored transcript into the
  native DSH store when no generation exists, rewriting the header `id`/`cwd` to
  the current physical checkout; an existing native generation is never
  overwritten.
- Session format is currently v4. The resolver selects the highest generation,
  but a future event-schema change may require mapping updates.
- Very large sessions can exceed Entire's 10 MB subcommand stdout cap.

## Captured Payloads

- Verification script: `scripts/verify-dsh.sh`
- Capture shape: the exact JSON payload shown under Hook Mechanism
- Verification status: VERIFIED against a live DSH session and Entire CLI 0.12.2
- Notable difference from docs: DSH hook names are hyphenated on the command
  line (`turn-end`) while the payload's `hook_type` is underscored
  (`turn_end`); the parser accepts both.

## E2E Test Prerequisites

- Entire CLI binary: `entire` on `PATH` or `--entire <path>`
- DSH Host with the `@sredevopsorg/dsh-entire` plugin in the active profile
- `entire-agent-dsh` on `PATH`
- `DSH_HOME` readable (and writable for the normalized cache)
- A live model-driven DSH prompt is **not** required for `scripts/verify-dsh.sh`;
  it writes a synthetic v4 session and drives the real Entire hooks.
