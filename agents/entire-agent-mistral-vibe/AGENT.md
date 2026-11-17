# Mistral Vibe — External Agent Research

## Verdict: COMPATIBLE

Mistral Vibe 3.1.1+ has a native hook system (`hooks.toml`) that covers session start, prompt submit, turn end and session end, and a session API (`vibe-app-server`) that returns the full conversation. The integration installs five hooks as lifecycle signals and reads the transcript from the session API. Vibe's on-disk session store is a private recovery format and is not parsed. Developed and verified against Vibe 3.1.2.

## Static Checks

| Check            | Result | Notes                                                                  |
| ---------------- | ------ | ---------------------------------------------------------------------- |
| Binary present   | PASS   | `vibe`, plus `vibe-app-server` from the same install                   |
| Help available   | PASS   | `vibe --help`                                                          |
| Version info     | PASS   | `vibe --version` (`vibe 3.1.2`)                                        |
| Hook keywords    | PASS   | Not in `--help`; documented at the URL below and in `vibe/core/hooks/` |
| Session keywords | PASS   | `-c/--continue`, `--resume [SESSION_ID]`                               |
| Config directory | PASS   | `~/.vibe/` (`$VIBE_HOME` overrides), project `.vibe/`                  |
| Documentation    | PASS   | https://docs.mistral.ai/vibe/code/cli/hooks                            |

## Binary

- Name: `vibe`
- Install: `uv tool install mistral-vibe`
- Auth: `MISTRAL_API_KEY` in the environment or `$VIBE_HOME/.env` (`vibe --setup`)
- Project hooks load only in a trusted folder (`--trust` covers one run).

## Hook Mechanism

- Config file: `.vibe/hooks.toml`
- Config format: TOML `[[hooks]]` tables (`name`, `type`, `match`, `command`, `timeout`). Entire's entries sit in a marked block; the user's own entries and the file's permissions are left alone.
- Hook input: JSON on stdin. Every event has `session_id`, `parent_session_id`, `transcript_path`, `cwd`, `hook_event_name`.
- Hook output: exit 0 with empty stdout is a pass-through; any other non-JSON stdout or a non-zero exit is shown as a hook failure. The installed command prints nothing and never fails.

| Vibe hook (`type`, `match`)      | Forwarded hook  | Protocol Event Type |
| -------------------------------- | --------------- | ------------------- |
| `session_start`                  | `session-start` | 1 = SessionStart    |
| `pre_agent`, `main`              | `pre-agent`     | 2 = TurnStart       |
| `notification`, `turn_completed` | `notification`  | 3 = TurnEnd         |
| `notification`, `turn_failed`    | `notification`  | 3 = TurnEnd         |
| `session_end`                    | `session-end`   | 5 = SessionEnd      |

Turn end uses the `turn_completed` notification, not `post_agent`: `post_agent` fires on a candidate answer that a hook can still reject, and the reply is not in the session history at that point. No tool hooks are installed; tool calls come from the session history.

A forked or rewound session carries the source session's id as `parent_session_id`, so that field alone does not mean "subagent". `session_start` and `pre_agent` (`match = "main"`) only fire for main sessions and are always tracked. A turn end or session end for a child session with no stored transcript is a subagent's and is ignored.

## Session Management

- Session ID source: `session_id` (a UUID) in every hook payload.
- Session directory: `<user cache dir>/entire/mistral-vibe/<repo hash>/`. It is not under `.entire/tmp`: Entire attributes a transcript to the first agent whose session directory contains it, and other agents claim `.entire/tmp` as a whole.
- Session file format: JSONL, one Vibe history entry per line, at `<session id>.jsonl`.
- Resume mechanism: `vibe --resume <session id>` (`vibe --continue` when no id is known).

## Transcript

- Authoritative storage: Vibe's session API. `transcript_path` in the hook payload is the private session store directory and is not read.
- Retrieval: the adapter runs `vibe-app-server` (newline-delimited JSON-RPC over stdio), calls `session/read` for model and token usage, then pages `session/history/list` backward from the newest entry and reassembles it oldest first. Reading needs no API key and does not resume the session. It runs at turn end, at session end, and from `prepare-transcript`.
- The app server binary comes from `VIBE_APP_SERVER_BIN` (set by Vibe for hooks, must be absolute) or `PATH`, never from a file in the repository.
- Entry types stored: `message` (`role` user/assistant, text content blocks), `reasoning`, `effect` (a tool call with `detail.toolName`, `detail.input`, `state.status`, `state.output`). Hook `notice` entries are dropped.
- User prompt extraction: text blocks of `message` entries with `role: "user"`.
- Assistant summary extraction: last non-empty assistant `message`.
- Modified file extraction: completed `effect` entries of kind `file_write` / `file_edit`, path from `state.output.filePath` or `detail.input`, made repo-relative.
- Token usage extraction: the session's cumulative `tokenUsage` is stored on the last line of each refresh; `calculate-tokens` reports the difference between snapshots. API call count is estimated per model response.

Session filename portability: Windows reserved device basenames (case-insensitive, including names followed by extensions and COM/LPT superscript-digit forms) use the hash mapping on every OS. This keeps resolver and transcript paths consistent when sessions move between platforms.

## Protocol Mapping

| Subcommand                | Native Concept              | Implementation Notes                                                       | Feasibility         |
| ------------------------- | --------------------------- | -------------------------------------------------------------------------- | ------------------- |
| `info`                    | static metadata             | Name `mistral-vibe`, `protected_dirs: [".vibe"]`                           | Required            |
| `detect`                  | `vibe` binary or `.vibe/`   | `vibe` on PATH, or a `.vibe` directory in the repo                         | Required            |
| `get-session-id`          | hook `session_id`           | Returns the input session id                                               | Required            |
| `get-session-dir`         | user cache dir              | `<cache>/entire/mistral-vibe/<repo hash>`                                  | Required            |
| `resolve-session-file`    | stored history JSONL        | `<session dir>/<session id>.jsonl`                                         | Required            |
| `read-session`            | stored history JSONL        | Raw bytes as `native_data`, plus modified files                            | Required            |
| `write-session`           | stored history JSONL        | Writes `native_data` to the session ref; does not recreate a Vibe session  | Required            |
| `read-transcript`         | stored history JSONL        | Raw bytes                                                                  | Required            |
| `chunk-transcript`        | raw bytes                   | Fixed-size byte chunks                                                     | Required            |
| `reassemble-transcript`   | chunks                      | Concatenates chunks                                                        | Required            |
| `format-resume-command`   | Vibe resume                 | `vibe --resume <id>`, shell-quoted                                         | Required            |
| `parse-hook`              | Vibe hook JSON              | Maps payloads to events; refreshes the transcript on turn and session end  | Hooks               |
| `install-hooks`           | `.vibe/hooks.toml`          | Appends a marked block; refuses symlinks and inline `hooks = [...]`        | Hooks               |
| `uninstall-hooks`         | `.vibe/hooks.toml`          | Removes only the marked block                                              | Hooks               |
| `are-hooks-installed`     | `.vibe/hooks.toml`          | Requires every hook in the block; fails when the state cannot be read      | Hooks               |
| `get-transcript-position` | stored history JSONL        | Entry (line) count                                                         | Transcript analyzer |
| `extract-modified-files`  | `effect` entries            | Completed file writes and edits from the offset                            | Transcript analyzer |
| `extract-prompts`         | user `message` entries      | Text from the offset                                                       | Transcript analyzer |
| `extract-summary`         | assistant `message` entries | Last non-empty reply                                                       | Transcript analyzer |
| `prepare-transcript`      | session API                 | Re-fetches the session; keeps the stored transcript if Vibe is unreachable | Transcript preparer |
| `compact-transcript`      | stored history JSONL        | User, assistant and tool-use lines; reasoning dropped                      | Compact transcript  |
| `calculate-tokens`        | session `tokenUsage`        | Delta between stored usage snapshots                                       | Token calculator    |

## Selected Capabilities

| Capability               | Declared | Justification                                                 |
| ------------------------ | -------- | ------------------------------------------------------------- |
| hooks                    | true     | Native `hooks.toml`                                           |
| transcript_analyzer      | true     | Session history has prompts, tool calls and replies           |
| transcript_preparer      | true     | History is fetched from the session API                       |
| compact_transcript       | true     | Typed history maps to Entire's compact format                 |
| token_calculator         | true     | Session `tokenUsage` snapshots give per-turn deltas           |
| text_generator           | false    | Not needed                                                    |
| hook_response_writer     | false    | Entire's external wrapper does not forward the response today |
| subagent_aware_extractor | false    | Subagent history is not captured separately                   |

## Gaps & Limitations

- **Resume is local.** `vibe --resume <id>` works only where Vibe still has the session. Restoring a checkpoint elsewhere restores Entire's transcript, not a Vibe session. The agent is `is_preview: true` accordingly.
- **Turn end and session end can race.** On exit Vibe starts both hooks in the background about 60ms apart and stops hooks five seconds later. Session end waits up to 1.5s for the turn-end hook to start and then for it to finish (3s in all). A slower turn-end hook is overtaken and that turn's checkpoint is not linked to the next commit.
- **Interrupted or crashed turns** fire no turn-end notification. They are closed by the next turn or by session end; a crash fires neither.
- **No compaction hook**, so Entire receives no Compaction event.
- **Windows needs Git Bash.** The hook commands are POSIX shell; under PowerShell they do not run.
- **Custom `VIBE_HOME`.** A refresh from a git hook cannot find the session if Vibe ran with a `VIBE_HOME` the shell does not export; the transcript stored at turn end is used.
- **Shell-made file changes** are not in the transcript's modified files; Entire picks them up from git.
- **Default model on free plans.** With no model configured, a key without a paid Vibe plan is routed to a model it cannot use, and Vibe reports that as an invalid API key. Tests pass `--model` explicitly.
- Not verified against a live Vibe: subagents, `--resume`, and sessions longer than one 500-entry history page (covered by a unit test that copies Vibe's paging rules).

## E2E Test Prerequisites

- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN`.
- Agent CLI binary: `vibe` 3.1.1 or later.
- Non-interactive prompt command: `vibe --model <model> --trust --auto-approve --output text -p "<prompt>"`.
- Interactive mode: `vibe --trust --auto-approve`, model set by `active_model` in `config.toml`.
- Expected prompt pattern: `>`.
- Timeout multiplier: 2.0.
- Bootstrap: a valid `MISTRAL_API_KEY`. Live tests are opt-in with `VIBE_E2E=1 E2E_AGENT=mistral-vibe` and run Vibe with a private `VIBE_HOME`; `VIBE_E2E_MODEL` overrides the model (default `mistral-medium-3.5`).
