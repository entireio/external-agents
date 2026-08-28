# Kilo Code — External Agent Research

## Verdict: COMPATIBLE

Kilo Code is a fork of OpenCode with a `@kilocode/plugin` API that exposes session lifecycle events on its internal event bus. Plugins are TypeScript/JavaScript modules loaded at startup from `.kilo/plugins/` and run on Bun. The integration installs a project plugin that subscribes to the bus and forwards five lifecycle hooks (`session-start`, `turn-start`, `turn-end`, `compaction`, `session-end`) to the Entire CLI. The plugin uses Kilo's native event bus directly — `session.idle` is deprecated upstream and `session.status` with `status.type === "idle"` is the reliable turn-end signal in both interactive and `kilo run` modes.

## Static Checks

| Check            | Result | Notes                                                                                            |
| ---------------- | ------ | ------------------------------------------------------------------------------------------------ |
| Binary present   | PASS   | `kilo` CLI installable from Kilo-Org/kilocode                                                    |
| Help available   | PASS   | `kilo --help`                                                                                    |
| Version info     | PASS   | `kilo --version`                                                                                 |
| Hook keywords    | PASS   | `event` hook bus emits `session.created`, `session.idle`, `session.compacted`, `message.updated` |
| Session keywords | PASS   | `kilo session list/delete`, `kilo run --session <id>`, `kilo run --continue`                     |
| Config directory | PASS   | `~/.config/kilo/`, project `.kilo/`                                                              |
| Documentation    | PASS   | https://kilo.ai/docs (plugins page at `packages/kilo-docs/pages/automate/extending/plugins.md`)  |

## Binary

- Name: `kilo`
- Install: `npm install -g kilo` or per upstream docs.
- Plugin loading: auto from `.kilo/plugins/`; disabled when `KILO_PURE=1` is set.

## Hook Mechanism

- Config file: `.kilo/plugins/entire.ts`
- Config format: TypeScript plugin running on Kilo's Bun runtime.
- Hook registration: plugin default export returns `{ event }` handler subscribed to the internal bus.
- Native event → forwarded hook → protocol event mapping:

| Native Event                 | Forwarded Hook   | Protocol Event Type |
| ---------------------------- | ---------------- | ------------------- |
| `session.created`            | `session-start`  | 1 = SessionStart    |
| `message.updated` (user)     | `turn-start`     | 2 = TurnStart       |
| `message.part.updated` (text) | `turn-start`    | 2 = TurnStart       |
| `session.status` (idle)      | `turn-end`       | 3 = TurnEnd         |
| `session.compacted`          | `compaction`     | 4 = Compaction      |
| `session.deleted`            | `session-end`    | 5 = SessionEnd      |
| `server.instance.disposed`   | `session-end`    | 5 = SessionEnd      |

`session.created` carries `properties.info.id`. `session.status` and `session.compacted` carry `properties.sessionID`. The plugin captures session ID from `session.created` (or the first `message.updated`) and re-uses it on subsequent events.

## Session Management

- Session ID source: Kilo `sessionID` from event bus payload.
- Session directory: `.entire/tmp/kilo/`.
- Session file format: JSON envelope `{ id, title, messages[], project, time }` produced by `client.session.messages.list({ id })` plus `client.session.get({ id })`. The binary writes this file on `session.created` (skeleton) and refreshes it on `session.idle`.
- Resume mechanism: `kilo run --session <sessionID>` (and `kilo run --continue` for last session).

## Transcript

- Authoritative storage: `session_ref` contains the full Kilo session JSON (`{ id, title, messages, project, time }`) where `messages` is the `MessageV2` array fetched from the local Kilo server via the SDK.
- Retrieval: the plugin calls `client.session.messages.list({ id })` during `session.created` (initial snapshot) and `session.idle` (per-turn refresh) and writes the JSON to `session_ref`. `prepare-transcript --session-ref <path>` is available as a refresh — it parses the existing prepared transcript to recover `id` and re-fetches via the SDK.
- Authoritative parser: `internal/kilo/types.go` models the JSON envelope as `Session`, `SessionMessage`, `MessagePart`, `ToolPart`, `Usage`.
- User prompt extraction: `Session.Messages[].Parts[]` `text` parts on `role: "user"` messages.
- Assistant summary extraction: last non-empty `text` part on `role: "assistant"` messages.
- Modified file extraction: `ToolPart.state.output.metadata.filePath`, `ToolPart.state.input.filePath`/`path` from known mutating tools (`write`, `edit`, `patch`, `multiEdit`).
- Token usage extraction: `MessageV2.assistant.tokens` (`{ input, output, reasoning, cache: { read, write } }`).
- Unprepared behavior: transcript analyzer, compact transcript, token calculation, and read-session require `session_ref` populated by the plugin. Operations called before `session.created` fires return an error.

## Protocol Mapping

| Subcommand                | Native Concept                  | Implementation Notes                                                                          | Feasibility         |
| ------------------------- | ------------------------------- | --------------------------------------------------------------------------------------------- | ------------------- |
| `info`                    | static metadata                 | Return name `kilo`, capabilities                                                              | Required            |
| `detect`                  | `kilo` binary                   | Check `command -v kilo`                                                                       | Required            |
| `get-session-id`          | Kilo session ID                 | Read from plugin event payload                                                                | Required            |
| `get-session-dir`         | `.entire/tmp/kilo`              | Standard Entire temp dir                                                                      | Required            |
| `resolve-session-file`    | `.entire/tmp/kilo/<id>.json`    | Standard path resolution                                                                      | Required            |
| `read-session`            | Kilo session JSON               | Validate and parse `Session`; expose `Session.ID`, raw JSON `native_data`, modified files     | Required            |
| `write-session`           | Kilo session JSON               | Write native data to session ref                                                              | Required            |
| `read-transcript`         | Kilo session JSON               | Return raw bytes after validating `Session` JSON                                              | Required            |
| `chunk-transcript`        | raw bytes                       | Base64 chunking via protocol JSON encoding                                                    | Required            |
| `reassemble-transcript`   | chunks                          | Reassemble raw bytes                                                                          | Required            |
| `format-resume-command`   | Kilo run session                | `kilo run --session <id> "<prompt>"`                                                          | Required            |
| `parse-hook`              | plugin event JSON               | Map per-hook payloads to protocol events; write session_ref on turn-end                       | Hooks               |
| `install-hooks`           | project plugin                  | Write `.kilo/plugins/entire.ts` with `__ENTIRE_CMD__` substituted                              | Hooks               |
| `uninstall-hooks`         | project plugin                  | Remove `.kilo/plugins/entire.ts`                                                              | Hooks               |
| `are-hooks-installed`     | project plugin                  | Check plugin file marker (`Auto-generated by ...`)                                            | Hooks               |
| `get-transcript-position` | Kilo session JSON               | Validate `Session` and return message count                                                   | Transcript analyzer |
| `extract-modified-files`  | Kilo session JSON               | Parse `ToolPart` state inputs/outputs from `write`/`edit`/`patch`/`multiEdit`                 | Transcript analyzer |
| `extract-prompts`         | Kilo session JSON               | Parse user `text` parts from `Session.Messages`                                               | Transcript analyzer |
| `extract-summary`         | Kilo session JSON               | Parse last assistant `text` part from `Session.Messages`                                      | Transcript analyzer |
| `prepare-transcript`      | SDK `session.messages.list`     | Re-fetch the Kilo session using the ID from the existing prepared transcript at `session_ref` | Transcript preparer |
| `compact-transcript`      | Kilo session JSON               | Emit Entire compact JSONL from `Session`                                                      | Compact transcript  |
| `calculate-tokens`        | Kilo session JSON               | Sum `MessageV2.assistant.tokens` fields                                                       | Token calculator    |

## Selected Capabilities

| Capability               | Declared | Justification                                                                  |
| ------------------------ | -------- | ------------------------------------------------------------------------------ |
| hooks                    | true     | Kilo plugins expose lifecycle events via the `event` hook bus                  |
| transcript_analyzer      | true     | Kilo session JSON contains prompts, tool calls/results, and assistant messages |
| compact_transcript       | true     | Kilo session JSON maps to Entire compact transcript format                     |
| transcript_preparer      | true     | Fetches Kilo session JSON via SDK into session ref                             |
| token_calculator         | true     | `MessageV2.assistant.tokens` is present on assistant messages                  |
| text_generator           | false    | Kilo CLI is used for agent execution, not summary generation                   |
| hook_response_writer     | false    | Not needed for lifecycle hooks                                                 |
| subagent_aware_extractor | false    | No documented subagent transcript tree distinct from main messages             |

## Gaps & Limitations

- **Resume restore is EXPERIMENTAL / incomplete.** Kilo keeps sessions in a global store (`~/.local/share/kilo/kilo.db`) keyed by session id, not in `.entire/`. `entire resume` restores the transcript to `.entire/tmp/kilo/<id>.json` and prints `kilo run --session <id>`, but Kilo only reads its own store — so resume works **only while Kilo still has the session locally**. Restoring a session Entire has but Kilo does not (after `kilo session delete`, a fresh clone, or another machine) is **not yet supported**: it would require importing the session back into Kilo via `kilo import <file>` (Kilo exposes `kilo export [id]` / `kilo import <file>` for this). That needs the durable artifact to be Kilo's native export blob rather than the analysis-oriented JSONL stored today, and is tracked as future work. Everything else (checkpointing, attribution, summaries, transcript analysis) is unaffected. The agent is `is_preview: true` accordingly.
- Kilo plugins do not load when `KILO_PURE=1` is set. The plugin must not assume that env is unset; if it is set, hooks never fire and `are-hooks-installed` still returns true (file exists).
- The transcript is materialized by `prepare-transcript` (Go side, via `kilo session show --format json` — see `command_runner.go`), which Entire calls before reading, mirroring the OpenCode parent. The plugin's `turn-end` fires **synchronously** and carries only `session_id` + `model`: `kilo run` exits on the same idle event, so an awaited in-plugin fetch could be cancelled before the hook runs. Turn-end payloads that do carry messages are still honored, but an empty message set never overwrites an existing transcript.
- `session.idle` is deprecated upstream and not reliably emitted in `kilo run` mode. The plugin subscribes to `session.status` and filters on `status.type === "idle"` for turn-end.
- Sub-sessions (Kilo subagent threads) are filtered out at `session.created` time by checking `info.parentID`. Subagent activity inside the parent session is still captured because the parent session JSON includes all parts.

## E2E Test Prerequisites

- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN`.
- Agent CLI binary: `kilo`.
- Non-interactive prompt command: `kilo run --print --no-color "<prompt>"`.
- Interactive mode: `kilo`.
- Expected prompt pattern: `>`.
- Timeout multiplier: 1.5.
