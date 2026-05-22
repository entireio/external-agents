# Kilo Code — External Agent Research

## Verdict: COMPATIBLE

Kilo Code is a fork of OpenCode with a `@kilocode/plugin` API that exposes session lifecycle events. Plugins are TypeScript/JavaScript modules loaded at startup from `.kilo/plugin/` (or `.kilocode/plugin/` / `.opencode/plugin/`) and run on Bun. The integration installs a project plugin that subscribes to the internal event bus, captures the active `sessionID` on `session.created`, and on `session.idle` retrieves the authoritative transcript via the local Kilo SDK client (`client.session.messages.list({ id })`) and writes it to `session_ref`.

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
- Plugin loading: auto from `.kilo/plugin/`; disabled when `KILO_PURE=1` is set.

## Hook Mechanism

- Config file: `.kilo/plugin/entire.ts`
- Config format: TypeScript plugin running on Kilo's Bun runtime.
- Hook registration: plugin default export receives `PluginAPI`, returns hooks map keyed by `event` (and optionally `chat.message`).
- Hook names and protocol mapping:

| Native Event              | When It Fires                                              | Protocol Event Type |
| ------------------------- | ---------------------------------------------------------- | ------------------- |
| `session.created`         | new session created                                        | 1 = SessionStart    |
| synthetic `turn.start`    | derived from `message.updated` with role=user              | 2 = TurnStart       |
| `session.idle`            | session finished responding (model + tools done for turn)  | 3 = TurnEnd         |

`session.created` carries `properties.info.id` (the session ID). `session.idle` carries `properties.sessionID`. The plugin captures session ID from `session.created` first, then re-uses it on subsequent events for the same session.

## Session Management

- Session ID source: Kilo `sessionID` from event bus payload.
- Session directory: `.entire/tmp/kilo/`.
- Session file format: JSON envelope `{ id, title, messages[], project, time }` produced by `client.session.messages.list({ id })` plus `client.session.get({ id })`. The binary writes this file on `session.created` (skeleton) and refreshes it on `session.idle`.
- Resume mechanism: `kilo run --session <sessionID> "<prompt>"` (and `kilo run --continue` for last session).

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
| `parse-hook`              | plugin event JSON               | Map event payloads to protocol events; refresh session on `session.created` and `session.idle`| Hooks               |
| `install-hooks`           | project plugin                  | Write `.kilo/plugin/entire.ts`                                                                | Hooks               |
| `uninstall-hooks`         | project plugin                  | Remove `.kilo/plugin/entire.ts`                                                               | Hooks               |
| `are-hooks-installed`     | project plugin                  | Check plugin file marker                                                                      | Hooks               |
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

- Kilo plugins do not load when `KILO_PURE=1` is set. The plugin must not assume that env is unset; if it is set, hooks never fire and `are-hooks-installed` still returns true (file exists).
- All transcript operations require the fetched Kilo session JSON at `session_ref`. The plugin triggers this fetch from the `session.created` and `session.idle` events; operations called before either fires return an error.
- `session.created` carries the session ID under `properties.info.id`; `session.idle` carries it under `properties.sessionID`. The parser must handle both shapes.
- Sub-sessions (Kilo subagent threads) emit their own `session.idle`. The plugin filters to top-level sessions by checking `info.parentID == null`.

## E2E Test Prerequisites

- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN`.
- Agent CLI binary: `kilo`.
- Non-interactive prompt command: `kilo run --print --no-color "<prompt>"`.
- Interactive mode: `kilo`.
- Expected prompt pattern: `>`.
- Timeout multiplier: 1.5.
