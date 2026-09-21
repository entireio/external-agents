# Kiro - External Agent Research

## Verdict: COMPATIBLE

Kiro has enough hook, session, and transcript surface area to fit the Entire external-agent protocol with one logical `kiro` agent. The built-in adapter in the Kiro worktree already treats CLI and IDE as two native entrypoints that normalize into the same Entire lifecycle.

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | DOCS/CODE | Worktree code and docs refer to `kiro-cli-chat` as the standalone CLI used for headless auth and prompt runs |
| Help available | DOCS/CODE | Worktree code and docs show CLI support for `chat`, `whoami`, `login`, and `--no-interactive` usage |
| Version info | DOCS/CODE | Version output was not live-probed here; protocol mapping only requires the binary be discoverable through CLI tooling |
| Hook keywords | PASS | CLI hooks, IDE hooks, prompt submit, tool use, stop |
| Session keywords | PASS | session ID, session ref, resume, transcript, workspace sessions |
| Config directory | PASS | `.kiro/`, `.vscode/`, and Kiro IDE workspace storage under the home directory |
| Documentation | PASS | `/Users/alisha/Projects/wt/kiro-oneshot/docs/architecture/external-agent-protocol.md` and Kiro adapter code |

## Binary
- Name: `kiro-cli-chat` for headless CLI usage; `kiro-cli` is the desktop wrapper that forces browser OAuth
- Version: not required for protocol mapping
- Install: use the Kiro CLI/desktop installation that provides `kiro-cli-chat`; the adapter expects the binary to be on `PATH`

## Hook Mechanism
- CLI hook config file: `.kiro/agents/entire.json`
- CLI hook config format: JSON with `name`, `tools`, and nested `hooks`
- CLI hook names and protocol mapping:
  | Native Hook Name | When It Fires | Protocol Event Type |
  |-----------------|---------------|---------------------|
  | `agentSpawn` | CLI session starts | `SessionStart` |
  | `userPromptSubmit` | User submits a prompt | `TurnStart` |
  | `preToolUse` | Before a tool call | no lifecycle event |
  | `postToolUse` | After a tool call | no lifecycle event |
  | `stop` | CLI turn ends | `TurnEnd` |
- CLI hook payload: JSON on stdin with `hook_event_name`, `cwd`, `prompt`, `tool_name`, `tool_input`, and `tool_response`
- IDE hook config files: `.kiro/hooks/*.kiro.hook`
- IDE hook config format: JSON with `enabled`, `name`, `description`, `version`, `when`, and `then`
- IDE trigger types installed by Entire: `promptSubmit`, `agentStop`, `preToolUse`, `postToolUse`
- IDE hook input: the adapter tolerates empty stdin and falls back to environment variables such as `USER_PROMPT`
- IDE hook command form: macOS/Linux use `sh -c '<command> </dev/null'`; Windows uses `cmd /c "<command> <NUL"` so the parent `entire` CLI process inherits a closed stdin (Kiro IDE on Windows leaves stdin open, and the adapter's 100ms stdin timeout only covers the agent binary, not the CLI parent)

## Session Management
- Session ID source: `parse-hook` resolves identity in this order: (1) explicit IDE session ID (`session_id` / `sessionId` / `chatSessionId`) in the hook payload — populates both `entire_session_id` and `ide_session_id`; (2) explicit CLI `conversation_id` in the payload — populates `entire_session_id` and `conversation_id` only, leaving `ide_session_id` empty so transcript capture skips the IDE path (otherwise a repo with leftover IDE workspace data would silently checkpoint an unrelated IDE chat for what is actually a CLI turn); (3) the most-recently-modified `<sessionId>.json` file in the IDE workspace-sessions directory — Kiro writes the user's prompt or assistant's response to the active chat's transcript file before invoking the hook, so file mtime is the closest signal to "which chat tab fired"; the IDE session UUID is used as both `ide_session_id` and `entire_session_id`; (4) repo-global `kiro-active-session` cache (Kiro CLI / non-IDE flow); (5) CLI `conversation_id` queried from the SQLite DB; (6) a generated Entire ID
- Session cache: `.entire/tmp/kiro-active-session` is used exclusively for the Kiro-CLI (non-IDE) flow. IDE chats aren't cached as a "current session" because re-deriving from `<sessionId>.json` mtime each turn is what lets tab-switching scenarios resolve correctly
- Active turn cache: `.entire/tmp/kiro-active-turn` holds the IDE session ID for the in-flight turn — written at `user-prompt-submit`, read by `post-tool-use` and `stop`, cleared at end of `stop`. This binds an entire turn (prompt → tool uses → stop) to the chat that fired prompt-submit, so a tab switch mid-turn cannot cause stop to finalize against a different chat
- Per-chat trim offset: every IDE chat and every CLI `conversation_id` gets its own offset file (`.entire/tmp/kiro-transcript-offset-<key>.json`) so capturing one chat never trims another chat's history
- Per-chat tool-call capture: `post-tool-use` appends to a per-chat file (`.entire/tmp/kiro-tool-calls-<sanitized-id>.jsonl`) keyed by the active-turn cache; transcript capture reads only the current chat's file. The legacy global file is reserved for CLI-only flows where there is no IDE chat ID

## Concurrency limitations
- `kiro-active-turn` is a single repo-global file. Each prompt-submit overwrites it; each stop owner-checks before clearing. This is correct for the common cases — single chat, tab-switch mid-turn, sequential turns across tabs, and concurrent CLI+IDE activity (each side's payload lets the other flow stay isolated). It is best-effort under truly overlapping IDE turns (`A prompt → B prompt → A stop`): A's stop has no payload signal that distinguishes it from B's, so the resolver returns whichever IDE chat is currently in cache. The owner-check at clear time prevents A's stop from deleting B's binding, but A's session_id will still resolve to whatever the cache currently holds. Fixing this fully requires a per-turn signal in the hook payload from Kiro, which it does not provide today
- For the same reason, post-tool-use hooks during a truly overlapping IDE turn fall back to mtime-based active-chat detection when the cache holds the wrong chat. CLI flows are unaffected because they short-circuit on payload `conversation_id`
- Session directory: `.entire/tmp/` under the repo root
- Session file format: **JSONL, one message per line**, one file per session ID. The native CLI and IDE documents are ingestion formats only — they are converted before being written
- Session file path: `.entire/tmp/<session-id>.json`
- Native CLI session lookup: SQLite database at `~/Library/Application Support/kiro-cli/data.sqlite3` on macOS, `~/.local/share/kiro-cli/data.sqlite3` on Linux, or `%LOCALAPPDATA%/kiro-cli/data.sqlite3` on Windows
- Native CLI lookup key: the current working directory is queried against `conversations_v2`
- Native IDE session lookup: `~/Library/Application Support/Kiro/User/globalStorage/kiro.kiroagent/workspace-sessions/<base64(cwd)>/sessions.json` on macOS, `~/.config/Kiro/User/globalStorage/kiro.kiroagent/workspace-sessions/<base64(cwd)>/sessions.json` on Linux, or `%APPDATA%/Kiro/User/globalStorage/kiro.kiroagent/workspace-sessions/<base64(cwd)>/sessions.json` on Windows
- IDE transcript source: the most recent entry in `sessions.json`, then `<sessionId>.json` in the same directory

## Transcript
- Stored format: **JSONL, one message per line.** Entire slices external-agent
  transcripts by LINE offset (`transcript.SliceFromLine`) before compacting
  them, so a single JSON document is cut mid-value and every checkpoint's
  `transcript.jsonl` came out empty.
- **A paired history entry becomes TWO lines.** Kiro's history entries are
  `{"user":…,"assistant":…}` pairs, and Entire's `normalizeKind` yields exactly
  one kind per line, so a pair cannot survive as one line without losing a half.
  The two halves are emitted as separate records sharing an `entry` index.
- Each stored record carries kiro's native `user`/`assistant` payload, the
  session-level fields (`conversation_id`, `cli_version`) stamped on every
  record because a scoped slice has no header, and an Entire-facing projection
  (`type`, `timestamp`, `message`) built from them. Entire's generic JSONL
  compactor needs a top-level `type`/`role` AND content under a top-level
  `message` wrapper; kiro's own `user`/`assistant` keys are where Entire does
  not look.
- A record whose payload this build cannot project carries no `type`, so Entire
  drops it rather than emitting an empty envelope, while kiro's own extractors
  still see it.
- Back-compat: a native whole-document transcript written by an older build
  still parses, reported in its original history-entry unit; the next capture
  rewrites it as JSONL and the unit moves with the bytes. A position and the
  bytes it indexes are always stored together in one checkpoint, so historical
  checkpoints are never re-scoped.
- Native CLI transcript format (input to the conversion): JSON object with `conversation_id` and `history`
- CLI history entries: paired user and assistant messages
- User prompt shape: `history[].user.content` containing a tagged union such as `{"Prompt":{"prompt":"..."}}`
- Assistant tool-call shape: `history[].assistant` containing `{"ToolUse": {...}}`
- Assistant response shape: `history[].assistant` containing `{"Response": {...}}`
- Native IDE transcript format (input to the conversion): JSON object with `history`, where each entry contains a `message` object
- IDE message shape: Anthropic-style `role` plus `content`
- CLI transcript capture: fetched from SQLite at turn end, materialized as JSONL, and written atomically to `.entire/tmp/<session-id>.json`
- IDE transcript capture: read from the workspace session file, converted to CLI shape, materialized as JSONL, and written atomically to `.entire/tmp/<session-id>.json`

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | adapter metadata | static JSON describing `kiro` and declared capabilities | Required |
| `detect` | `.kiro` repository config | presence is inferred from repo-local Kiro state | Required |
| `get-session-id` | cached stable session ID | use the cached Entire session ID, not Kiro's transient DB state | Required |
| `get-session-dir` | Entire cache directory | return `<repo>/.entire/tmp` | Required |
| `resolve-session-file` | normalized cache path | return `<repo>/.entire/tmp/<id>.json` | Required |
| `read-session` | cached transcript file | read the normalized JSON cache and synthesize `AgentSession` | Required |
| `write-session` | normalized transcript cache | write the cached transcript back for rewind/resume | Required |
| `read-transcript` | cached transcript bytes | return raw bytes from `.entire/tmp/<id>.json` | Required |
| `chunk-transcript` | raw transcript bytes | chunk the cached JSON for transport; no native Kiro format change needed | Required |
| `reassemble-transcript` | chunk reassembly | reverse `chunk-transcript` | Required |
| `format-resume-command` | Kiro resume command | return `kiro-cli chat --resume` | Required |
| `parse-hook` | CLI/IDE hook payloads | map CLI stdin JSON or IDE env-based input to lifecycle events | If hooks capable |
| `install-hooks` | `.kiro/agents/entire.json`, `.kiro/hooks/*.kiro.hook`, `.vscode/settings.json` | install both CLI and IDE support in one operation | If hooks capable |
| `uninstall-hooks` | reverse hook installation | remove Entire-owned CLI and IDE hook files plus trusted commands | If hooks capable |
| `are-hooks-installed` | config presence check | true when either CLI hooks or IDE hooks are present | If hooks capable |
| `get-transcript-position` | transcript length | number of stored JSONL lines (two per paired history entry); a legacy whole-document transcript still reports its history-entry count | If transcript analyzer |
| `extract-modified-files` | transcript tool-use history | parse tool calls and file edits from cached transcript | If transcript analyzer |
| `extract-prompts` | user prompt history | return prompt text from transcript history | If transcript analyzer |
| `extract-summary` | last assistant response | return the final assistant response text | If transcript analyzer |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | Kiro has both CLI hook config and IDE hook files |
| transcript_analyzer | true | Kiro transcripts are JSON and can be parsed for prompts and file changes |
| transcript_preparer | false | the adapter only needs to read cached transcripts, not pre-process live files |
| token_calculator | false | the worktree adapter does not expose a token-counting path |
| text_generator | false | Kiro's CLI is used for agent execution, not metadata generation |
| hook_response_writer | false | the adapter does not depend on structured hook responses |
| subagent_aware_extractor | false | the worktree code does not model a subagent transcript tree |

## Gaps & Limitations
- Kiro CLI and Kiro IDE do not share the same native storage format, so Entire must normalize them into `.entire/tmp/<session-id>.json`
- `kiro-cli` is not the right binary for headless support; the adapter uses `kiro-cli-chat` because it supports device-flow or SIGV4-based non-interactive use
- The CLI transcript is only reliable at turn end because Kiro's SQLite conversation record is populated late
- IDE prompt input may arrive through environment variables instead of stdin, so hook parsing must tolerate empty stdin
- CLI transcript capture now uses an embedded SQLite driver instead of the `sqlite3` shell command, so CLI-native transcript lookup no longer depends on an external binary being installed
