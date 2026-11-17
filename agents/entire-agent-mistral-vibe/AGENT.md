# Mistral Vibe — External Agent Research

Researched 2026-11-17 against Vibe **3.1.2** (upstream `mistralai/mistral-vibe`, tag
`v3.1.2`, released 2026-11-10). Reviewed by Codex the same day; its confirmed findings
are folded in.

## Verdict: PARTIAL (feasible with gaps)

Vibe 3 has a stable native hook system (`hooks.toml`) that covers session start, prompt
submit, tool use, turn end and session end, with `session_id` on every payload. No hook
carries the assistant's reply, and the native store is a private recovery format. The
transcript should therefore come from Vibe's **session API** (`vibe-app-server`,
`session/read` / `session/history/list`), which returns typed history without an API
key, with hooks used only as lifecycle signals.

### Confidence labels

- **(live)** observed in a sandboxed run on 2026-11-17 (`scripts/verify-mistral-vibe.sh
  --run`, private `VIBE_HOME`).
- **(source)** read from the installed 3.1.2 package or the upstream tag.
- **(docs)** docs site only.

### What the live runs covered

Headless runs with `--model mistral-medium-3.5`, each one prompt and one file write:

- All seven hook types fired with valid JSON: `session_start`, `pre_agent`, `pre_tool`,
  `post_tool`, `post_agent`, `notification` (`turn_completed` and, in an earlier failed
  run, `turn_failed`), `session_end`.
- The session API returned the user message, reasoning, the tool call and the
  assistant's reply, plus token usage, with no API key.
- A hook can call the session API on the live session: about 0.75s per read. At
  `post_tool` and `post_agent` the reply is **not yet** in the history and the session
  is still `running`; at `notification`/`turn_completed` and `session_end` it is there
  and the session is `idle`.

The built adapter was then run through the lifecycle suite (see "Implementation as
built"), which adds interactive (TUI) sessions and multi-prompt runs.

Not covered: subagents, compaction, interrupts, `--resume`, `post_agent` retries, long
histories that need paging.

## Implementation as built

- **Hooks installed** (in a marked block of `.vibe/hooks.toml`): `session_start`,
  `pre_agent` (`match = "main"`), `notification` for `turn_completed` and for
  `turn_failed`, `session_end`. No tool hooks and no `post_agent`: tool calls come from
  the session history, so Entire is not in the path of every tool call.
- **Transcript**: on turn end and session end the adapter runs `vibe-app-server`,
  reads the session and pages its history **backward from the newest entry**, then
  reassembles it oldest first, drops hook notices, and writes one entry per line. With
  no cursor Vibe returns the newest page whatever direction is asked for, and only a
  backward request says how to reach older entries (source: `history_page` in
  `vibe/app_server/_state.py`). The multi-page path is covered by a unit test that
  copies those semantics; live sessions so far have fitted in one page. The last line of each refresh carries an `entire` object with the
  session's cumulative token usage and model; earlier snapshots are kept, which is what
  makes per-turn token deltas possible.
- **Session directory**: `<user cache>/entire/mistral-vibe/<repo hash>/`, not
  `.entire/tmp`. Entire attributes a transcript to the first agent whose session
  directory contains it; built-in OpenCode and the amp and kiro external agents claim
  `.entire/tmp` as a whole, so a transcript stored below it was attributed to them and
  every hook was dropped as "forwarded". (live)
- **Turn-end before session-end**: when a headless run exits, Vibe starts the
  `turn_completed` and `session_end` hooks about 60ms apart and runs both in the
  background. Left alone, Entire's session-end handling finished first and the next
  commit linked no checkpoint. (live) The adapter keeps a per-session marker: turn
  start writes "open", the turn-end hook replaces it with the PID of the `entire`
  process handling it, and session end waits while the marker is "open" (up to 1.5s)
  or names a live process (3s in all). This narrows the race; it does not close it.
- **Forks and rewinds**: a forked session's `parent_session_id` is the session it came
  from (source: `_host.py`), so that field alone does not mean "subagent".
  `session_start` and `pre_agent` (`match = "main"`) only fire for main sessions and
  are always tracked; a turn end or session end for a child session the adapter has no
  transcript for is treated as a subagent's and ignored.
- **Hooks file safety**: neither `.vibe` nor `hooks.toml` is followed if it is a
  symlink; the block markers only count as whole lines; an inline `hooks = [...]` key,
  bare or quoted, is refused; an existing file keeps its permissions.
- **App server path**: from `VIBE_APP_SERVER_BIN` (set by Vibe for hooks) or `PATH`.
  It is never read from a file in the repository.

Test status on 2026-11-17, Vibe 3.1.2, Entire CLI 0.13.2-nightly:

| Suite | Result |
|---|---|
| Unit tests (`mise run test`) | pass, 84.5% statement coverage of `internal/vibe` |
| Protocol compliance (`external-agents-tests verify`) | 46 pass, 0 fail, 6 skipped (undeclared capabilities, optional fixtures) |
| Lifecycle, live | 6 pass: single prompt, multiple prompts, session persistence, detect and enable, hooks installed, interactive TUI session |
| Lifecycle, live | 2 fail: `RewindPreCommit`, `RewindAfterCommit` — the harness calls `entire checkpoint rewind --list`, which this Entire CLI no longer has. Not specific to this agent. |

A Codex review of the first commit found three blocking defects (history beyond 500
entries dropped, forked sessions ignored, `.vibe` symlink followed) and several smaller
ones; all were reproduced with failing tests and fixed, and the suites above were rerun.

A checkpoint from the live single-prompt run holds the prompt, the `write_file` tool
call with its result, the assistant's reply, the model, token usage and the touched
file.

## History in this repo

`origin/mistral-plugin` (2026-03-25) holds an earlier adapter: 21 files, ~4.2k lines.
It targeted a fork of Vibe 2.6.2 (`lifecycle-hooks`) that added a `[hooks]` table to
`config.toml` and read `messages.jsonl`. Upstream never had that table. Its hook and
transcript code do not apply to 3.x; the protocol scaffolding and e2e adapter shape do.

## Static Checks

| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `~/.local/bin/vibe` → uv tool `mistral-vibe` from PyPI |
| Help available | PASS | Rust CLI front end |
| Version info | PASS | `vibe 3.1.2` |
| Hook keywords | PASS | Not in `--help`; documented and in source |
| Session keywords | PASS | `-c/--continue`, `--resume [ID]`, `--output-dir` |
| Config directory | PASS | `~/.vibe/` (`$VIBE_HOME` overrides); project `./.vibe/` |
| Documentation | PASS | https://docs.mistral.ai/vibe/code/cli/hooks |

## Binary

- Names: `vibe`, `vibe-acp`, `vibe-app-server`
- Install: `uv tool install mistral-vibe`
- Auth: `MISTRAL_API_KEY` in the environment or `$VIBE_HOME/.env` (`vibe --setup`)

## Hook Mechanism

- Files: `./.vibe/hooks.toml` (project, trusted folders only) and `~/.vibe/hooks.toml`
  (user). Both load; same `name` in both: project wins. (docs, source)
- Format: TOML `[[hooks]]` with `name`, `type`, `command`, optional `match`, `timeout`
  (default 60s), `strict`, `description`. (source)
- Commands run through the shell in `cwd`, JSON on stdin. Hook env includes `VIBE_HOME`
  and `VIBE_APP_SERVER_BIN` (path to the running install's app server). (live)

| Native type | When it fires | Entire use |
|---|---|---|
| `session_start` | Session starts; `source` = `startup`/`resume`/`clear`/`fork`. Not for subagents. | 1 SessionStart |
| `pre_agent` | Once per turn, before the model reads the message. | 2 TurnStart (main agent only) |
| `post_agent` | On a **completion candidate**: a hook may return `deny` and force a retry (max 3 per turn) (source). The reply is not yet in the session history at this point (live). | Not a turn end; ignore or use as a hint |
| `notification` / `turn_completed` | Main-agent turn accepted as complete. Background. Message is the fixed string "Vibe finished its turn"; reply is readable from the session API (live). | 3 TurnEnd |
| `notification` / `turn_failed` | Turn failed; message carries the error. (live) | 3 TurnEnd with error text |
| `session_end` | Session ends; `reason` = `exit`/`clear`/`other`. Background; not after a crash. | 5 SessionEnd |
| `pre_tool` | Per tool call, before approval; the call may still be denied. | Tool record; not proof a subagent started |
| `post_tool` | Per tool call, only if the tool body ran. | Tool record, modified files |
| — | No compaction hook. | 4 Compaction: missing |

Stdin on every event: `session_id`, `parent_session_id`, `transcript_path`, `cwd`,
`hook_event_name`. Extra fields:

- `session_start`: `source`, `agent_name`, `model` (live)
- `pre_agent`: `agent_name`, `turn_id`, `prompt` (live)
- `notification`: `notification_type`, `message`, `agent_name` (live); `tool_name` on approval (docs)
- `session_end`: `reason` (live)
- `post_agent`: `agent_name` only — no reply text (live)
- `pre_tool`: `tool_name`, `tool_call_id`, `tool_input` (live), e.g.
  `{"tool_name":"file_system.write_file","tool_call_id":"YaJMmky2Y","tool_input":{"file_path":"probe-entire.txt","content":"…"}}`
- `post_tool`: plus `tool_status`, `tool_output` (for a write: `bytes_written`,
  `file_existed`, absolute `path`), `tool_output_text`, `tool_error`, `duration_ms` (live)

`agent_name` is the configured agent's name (`auto-approve` in the live run); `main` is
only a `match` selector. In runs without `--model`, the `model` in `session_start` (`mistral-medium-3.5`)
differed from the model the session API reported and Vibe actually called
(`zai-glm-5-3-flash`); with `--model` set they agreed. So
do not treat the hook value as authoritative.

Stdout contract (docs, source): exit 0 with empty stdout passes through; exit 0 with a
JSON object is a structured response; exit 0 with any other non-empty stdout, a non-zero
exit, or a timeout is a hook failure (a warning, or a block when `strict`). Output of
`notification` and `session_end` hooks is ignored.

Ordering and timing:

- `session_start` hooks run as a background task. Vibe's wait for them happens *after*
  the user's `pre_agent` hooks, so Entire's `pre_agent` hook can start while its
  `session_start` hook is still running. (source) In the live run the two were 2.4s
  apart and did not overlap, but the probe hook returned instantly.
- On quit Vibe waits at most 5 seconds in total for running `session_end` and
  `notification` hooks. (docs) Live: `session_end` fired 30ms after `turn_failed`.

## Session Management

- Session ID: UUID in every payload; also the store directory name. (live)
- Store: `<session_logging.save_dir>/unified/<session_id>/`, default
  `$VIBE_HOME/logs/session/unified/<id>/`. With session logging disabled Vibe uses a
  throwaway directory. (source; default path live)
- Contents (live): `CURRENT` (JSON pointer, `store_format:
  mistral.vibe.unified-session-store/v1`), `journal/<seq>.jsonl`, `generations/<seq>/`
  (`checkpoint.json`, `manifest.json`, `projection-state.json`, `runtime-state.json`),
  `chunks/<sha>.json`, `meta.json`, `scheduled-loops.json`.
- Journal records are hash-chained recovery records (`record_sha256`,
  `previous_record_sha256`, `sequence`, `type`, `payload`), not chat messages. Superseded
  generations and journal segments are deleted. (live, source) ADR 0006 calls this
  runtime-private state.
- `meta.json` (live): `session_id`, `parent_session_id`, `start_time`, `end_time`,
  `git_commit`, `git_branch`, `environment.working_directory`, `origin_directory`,
  `child_sessions`, `title`. No token stats in the failed run.
- Resume: `vibe --resume <SESSION_ID>`; `vibe -c` picks this terminal's last session in
  the folder, else the folder's most recent. Resume needs the native store on that
  machine.

## Transcript

- `transcript_path` is the session store **directory**. (live)
- **Recommended source: the session API.** Start `vibe-app-server` (stdio, newline-
  delimited JSON-RPC 2.0), send `initialize`, `initialized`, then `session/read` or
  `session/history/list` with `{"sessionId": …}`. (live) Observed:
  - works with no `MISTRAL_API_KEY` in the environment;
  - about 0.8s end to end for a tiny session, without resuming it;
  - returns `vibe.public-session-state/v1`: session `status` (`running` with
    `activeTurnId`, `idle`, `failed`), `cwd`, `model`, `tokenUsage`, `contextUsage`,
    `parentSessionId`, and a `history` list;
  - history entry types seen: `message` (`role` `user`/`assistant`, `turnId`,
    `content: [{"type":"text","text":…}]`), `reasoning` (`text`), `effect` (a tool
    call: `title`, `detail.toolName`, display fields), `notice` (hook runs). Every entry
    has a stable `id` and `turnId`.
- The adapter materializes that history into an Entire-owned JSONL transcript under its
  own session dir, at `turn_completed`/`turn_failed` and again at `session_end`.
  `entire-agent-kilo` already does this kind of fetch through `prepare-transcript`.
- The reply also sits in the private store (`journal/*.jsonl`, `chunks/*.json`,
  `generations/*/checkpoint.json`) and in `vibe -p --output json` stdout. (live)
- Token usage (live): session state `tokenUsage` = `inputTokens`, `outputTokens`,
  `totalTokens`, `cachedInputTokens`, cumulative for the session and readable mid-turn;
  per-turn figures are the difference between reads. `export.json` has the same totals
  plus `cost_usd`. `meta.json` has no stats. No usage in any hook payload.

## Data Storage Verification

- Session files contain actual assistant content: YES, but only in private formats
  (`journal`, `chunks`, `generations/*/checkpoint.json`). (live)
- Public route to the same content: session API history. (live)
- Hook data flow verified: YES for all seven event types. (live)
- Method: sandboxed headless run, then `find`/`grep` over the private `VIBE_HOME` and a
  JSON-RPC read against `vibe-app-server`.

## Protocol Mapping

| Subcommand | Native concept | Implementation notes | Feasibility |
|---|---|---|---|
| `info` | — | `protocol_version: 1`, name `mistral-vibe`, type `Mistral Vibe`, `protected_dirs: [".vibe"]`, `hook_names` listed below | Supported |
| `detect` | `vibe` on PATH or `.vibe/` | | Supported |
| `get-session-id` | HookInput `session_id` | Input here is Entire's HookInput, not the native payload | Supported |
| `get-session-dir` | Entire-owned dir | `<user cache>/entire/mistral-vibe/<repo hash>`, outside `.entire/tmp` | Supported |
| `resolve-session-file` | `<dir>/<session_id>.jsonl` | Must stay inside `get-session-dir`; session IDs are UUIDs so filenames are safe | Supported |
| `read-session` / `write-session` | materialized transcript | `native_data` is base64 in JSON. Restores Entire's copy only, not a Vibe session | Supported, see gap 4 |
| `read-transcript`, `chunk-`, `reassemble-` | materialized bytes | Raw bytes on read; chunks base64 | Supported |
| `format-resume-command` | `vibe --resume <id>` | | Supported on the original machine |
| `parse-hook` | `hook_event_name` | Native payload → Event. Drop events with `parent_session_id` set from turn mapping. `metadata` values must be strings | Supported |
| `install-hooks` | `./.vibe/hooks.toml` | Append a marked block of `[[hooks]]` tables named `entire-*`, keep the user's; no TOML dependency | Supported; folder must be trusted |
| `uninstall-hooks`, `are-hooks-installed` | same file | | Supported |
| `prepare-transcript` | session API | Fetch history, write the Entire transcript | Supported (live) |
| `get-transcript-position`, `extract-modified-files`, `extract-prompts` | materialized transcript | Spec says byte offsets; Qwen uses record positions — settle before copying | Supported |
| `compact-transcript` | materialized transcript | CLI calls this first and falls back to its generic compactor; must accept temp-file slices | Supported |
| `calculate-tokens` | session `tokenUsage` | Difference between the usage snapshots stored at each refresh (live) | Supported |
| `write-hook-response` | `system_message` JSON | Entire's external wrapper discards the binary's stdout for this call today (`external.go` `WriteHookResponse`), so nothing would display | Blocked in CLI |
| `extract-all-modified-files` + `calculate-total-tokens` | `parentSessionId`, `child_sessions` | Both are required together | Unverified |

Hook names for `info`: `session-start`, `pre-agent`, `notification`, `session-end`.

## Selected Capabilities

| Capability | Declared | Justification |
|---|---|---|
| hooks | true | Native `hooks.toml` |
| transcript_analyzer | true | Materialized transcript |
| transcript_preparer | true | History is fetched from the session API at turn end |
| compact_transcript | true | Typed history maps cleanly |
| token_calculator | true | Per-turn deltas from stored `tokenUsage` snapshots (live) |
| text_generator | false | Not needed for v1 |
| hook_response_writer | false | CLI wrapper drops the output today |
| subagent_aware_extractor | false | Subagent payloads not captured |

## Gaps & Limitations

1. **Assistant text depends on the session API** (low–medium). No hook carries it. The
   API route works live, including from inside a hook, at about 0.75s per read for a
   one-turn session. Untested: pagination and cost on long sessions, and whether the
   app-server protocol stays stable across Vibe releases.
2. **Turn end needs care** (medium). The reply is only readable at
   `notification`/`turn_completed`, not at `post_agent`. That notification is a
   background hook and does not fire on interrupt, so interrupted or crashed turns close
   on the next `pre_agent`, at `session_end`, or not at all.
3. **Out-of-order lifecycle hooks** (medium). Turn end and session end overlap on a
   headless exit; the adapter orders them (see "Implementation as built").
   `session_start` and the first `pre_agent` can also overlap in principle; the adapter
   does nothing that depends on their order, and no overlap was seen live.
4. **Resume is not portable** (medium). Restoring a checkpoint on another machine
   writes Entire's transcript, not a Vibe session, so `vibe --resume` fails there.
   Kilo documents the same limit.
5. **5-second quit budget** (low–medium). `session_end` and `turn_completed` work that
   runs longer is killed. Live, turn end took about 1s and session end about 1s after
   it, so there is room, but a slow machine or a very long history could exceed it.
6. **No compaction event** (low). An independent transcript needs no offset reset, but
   compaction summaries are not recorded as an event.
7. **Hook responses not shown** (low). Depends on a fix in the Entire CLI wrapper.
8. **Trusted folders** (low). Project hooks load only after the folder is trusted;
   e2e must pass `--trust`.
9. **Strict stdout contract** (low). Stray output from `entire hooks …` shows as a
   warning on every blocking event.
10. **TOML editing in Go** (low). Handled without a dependency by appending a marked
    block; a file that defines `hooks = [...]` inline is refused with an explanation.
11. **Shell writes** (low). Modified-file detection must cover `file_system.bash` and
    background `process_start` commands as well as edit tools.

## Closest existing agents and size

- **`entire-agent-kilo`** for the transcript: fetch native session data through the
  agent's own CLI in `prepare-transcript`, and the documented restore limitation.
- **`entire-agent-qwen`** for the hook set and Entire-owned session-file mechanics.
- **`entire-agent-grok`** for a dedicated hooks file (`.grok/hooks/entire.json` ≈
  `.vibe/hooks.toml`).
- Size as built: about 2.4k lines of Go, of which about 500 are the protocol package
  copied from Kilo, plus 1.7k lines of tests.

## Captured Payloads

- Script: `agents/entire-agent-mistral-vibe/scripts/verify-mistral-vibe.sh`
- Captures were taken locally and are not in the repository. Sanitized copies of the
  session history and the hook payloads are the unit-test fixtures in `testdata/`.
- Status: VERIFIED for a single-turn headless session; see "Not covered" above.
- Differences from docs: `transcript_path` is a directory; hook env carries
  `VIBE_APP_SERVER_BIN`; with no config, a free-plan key gets a default model it is not
  entitled to, and Vibe reports the resulting HTTP 403 as "Invalid API key".

## E2E Test Prerequisites

- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN`
- Agent CLI: `vibe` ≥ 3.1.1 (first release with `session_start`, `session_end`,
  `pre_agent`, `notification` hooks)
- Non-interactive: `vibe --model mistral-medium-3.5 --trust --auto-approve --output text -p "<prompt>"`
  (pass `--model` explicitly; about 6s and $0.015 per one-tool prompt)
- Isolation: set `VIBE_HOME` to a temp dir so user hooks, plugins and sessions are not
  touched (live)
- Interactive: `vibe --trust --auto-approve` in tmux, model from `config.toml`
  (`active_model`); prompt pattern `>`
- Timeout multiplier: 2.0
- Bootstrap: valid `MISTRAL_API_KEY`; gated by `VIBE_E2E=1 E2E_AGENT=mistral-vibe`;
  adapter in `e2e/agents/mistral_vibe.go`
- Exit codes: 1 `config_error` (seen with a rejected key and with a model the key
  cannot use), 2 `infrastructure_failure`,
  3 limit/deadline stops

## Sources

- Hooks: https://docs.mistral.ai/vibe/code/cli/hooks
- Plugins: https://docs.mistral.ai/vibe/code/cli/plugins
- Upstream tag: https://github.com/mistralai/mistral-vibe/tree/v3.1.2
  (`vibe/core/hooks/`, `vibe/app_server/_host_hooks.py`,
  `docs/reference/app-server-protocol.md`, `docs/adr/0006-local-sessions.md`,
  `docs/programmatic-mode.md`, `CHANGELOG.md`,
  `harness/runtimes/python/python/mistralai_vibe_local_harness/vibe/_storage.py`,
  `_local_actions.py`, `_foreign_hooks.py`)
- Protocol: https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md
