# Sarvam Code — External Agent Research

## Verdict: COMPATIBLE

Sarvam Code has a native TOML-configured lifecycle hook system with JSON
payloads on stdin, durable JSONL session transcripts that carry the full
conversation, and token-usage events inside those transcripts. Every protocol
subcommand maps to a native concept. Two caveats shape the integration:

1. **Hooks fire only in interactive (TUI) sessions.** Headless `sarvam-code
   exec` runs never invoke hooks (verified with a trusted, enabled config).
2. **Project hooks are gated behind a one-time interactive review.** A fresh
   `.sarvam/config.toml` needs (a) directory trust and (b) per-hook trust +
   enable via the `/hooks` TUI screen, which persists to the user config.
   Neither step is automatable from the agent binary (the trust hash scheme is
   opaque), so `install-hooks` writes the declarations and the review is a
   documented bootstrap step.

## Static Checks

| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `/Users/soph/.local/bin/sarvam-code` (installed via PATH) |
| Help available | PASS | `sarvam-code --help`, subcommands: exec, resume, fork, debug, … |
| Version info | PASS | `cli 0.58.0` (`sarvam-code --version`) |
| Hook keywords | PASS | `[hooks]` config table, `/hooks` TUI screen |
| Session keywords | PASS | `resume`, `fork`, `sessions/` directory, `--last` |
| Config directory | PASS | `$SARVAM_HOME` (default `~/.sarvam`); project `.sarvam/` |
| Documentation | PASS | bundled self-knowledge skill tree (hook + session reference) |

## Binary

- Name: `sarvam-code`
- Version: 0.58.0 (verified)
- Install: official installer; binary at `~/.local/bin/sarvam-code`
- Non-interactive prompt: `sarvam-code exec [--skip-git-repo-check] [-C <dir>] "<prompt>"`

## Hook Mechanism (verified against live payloads)

- Config file (project): `<repo>/.sarvam/config.toml` — trust-gated
- Config file (user): `$SARVAM_HOME/config.toml`
- Config format: TOML. Each event key is an **array of matcher groups**:

  ```toml
  [[hooks.SessionStart]]
  [[hooks.SessionStart.hooks]]
  type = "command"
  command = "sh -c '…'"
  ```

  The self-knowledge docs' `SessionStart` example omits the matcher-group
  header; without it the CLI fails with `invalid type: map, expected a
  sequence` (reproduced). Handler fields: `type` (`"command"`), `command`,
  optional `timeout` (seconds), `async`, `statusMessage`. Tool events
  additionally **require** `matcher` (a regex matched against the model-facing
  tool name, e.g. `Bash`, `Write`); a missing matcher silently never fires.

- Hook names and protocol mapping:

  | Native Hook Name | When It Fires | Protocol Event Type |
  |-----------------|---------------|---------------------|
  | `SessionStart` | session begins / first turn starts | 1 = SessionStart |
  | `UserPromptSubmit` | user submits a prompt | 2 = TurnStart |
  | `Stop` | agent ends its turn | 3 = TurnEnd |
  | `PreCompact` | before context compaction | 4 = Compaction |
  | `PostCompact` | after context compaction | 4 = Compaction |
  | `PreToolUse` | before a tool call (needs matcher) | null (not forwarded) |
  | `PostToolUse` | after a tool call (needs matcher) | null (not forwarded) |
  | `PermissionRequest` | permission approval needed | null (not forwarded) |

- Hook input format: JSON on stdin; the command's stdout is returned to the
  agent as context; non-zero exit can block the action (PreToolUse,
  PermissionRequest).
- Trust model (verified):
  - The project layer loads only after the directory is trusted. First
    interactive run prompts; accepting writes
    `[projects."<abs path>"] trust_level = "trusted"` to the **user** config.
    Until then the project config (hooks included) is silently ignored.
  - Each hook additionally needs interactive review: `/hooks` → select →
    `t` (trust) → space (enable). This persists
    `[hooks.state."<project-config>:<event_snake>:<matcher-idx>:<hook-idx>"]`
    entries with `enabled` and `trusted_hash` (sha256, scheme opaque) to the
    **user** config. Not reproducible offline (brute-forced several
    serializations; no match), so the installer cannot pre-seed it.

## Session Management

- Session directory: `$SARVAM_HOME/sessions/YYYY/MM/DD/` (local start date).
  Override root via `SARVAM_HOME` env. Archived sessions move to
  `$SARVAM_HOME/archived_sessions/YYYY/MM/DD/`.
- Session files: one JSONL per run:
  `rollout-<YYYY-MM-DDThh-mm-ss>-<thread_id>.jsonl` where `<thread_id>` is
  the session UUID.
- Session ID source: hook input `session_id` (== thread UUID == the rollout
  filename suffix == `session_meta.payload.id`).
- Session file format: JSONL, one object per line with `timestamp` + `type`:
  - `session_meta` (first line): `payload.id`, `cwd`, `cli_version`,
    `source` (`"cli"` interactive / `"exec"` headless), git info.
  - `turn_context`: `payload.turn_id`, model context window.
  - `event_msg`: lightweight events (`task_started`, `task_complete`,
    `token_count`, `token_consumption`, `agent_message`, `user_message`,
    `exec_command_end`, …).
  - `response_item`: the conversation content (see Transcript).

## Transcript

- Location: the session rollout JSONL itself — every hook payload carries it
  as `transcript_path`.
- Format: JSONL (`response_item` entries hold the conversation).
- User prompt field: `payload.type == "message"` && `payload.role == "user"`,
  text in `payload.content[].input_text`. **Filter out entries whose text
  starts with `<environment_context>`** — the first user entry is harness
  context, not a human prompt (verified).
- Assistant content: `payload.role == "assistant"`, text in
  `payload.content[].output_text` — full real content, no placeholders
  (verified).
- Tool calls: `payload.type == "function_call"` with `name` (runtime tool
  name: `write_file`, `edit_file`, `exec_command`, …), `call_id`
  (`call-<uuid>`), `arguments` (JSON string). Results: `function_call_output`
  keyed by `call_id`.
- Modified files field: derived from `function_call` entries —
  `write_file`/`edit_file` take `path`; `exec_command` takes `command` (parse
  shell redirects with care).
- Token usage field: `event_msg` with `payload.type == "token_count"` carrying
  `payload.info.last_token_usage.{input_tokens, cached_input_tokens,
  output_tokens, reasoning_output_tokens, total_tokens}` (per API call) and
  `info.total_token_usage` (cumulative). `info` is null on rate-limit-only
  events.
- Example entry:

  ```json
  {"timestamp":"…","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Create a file…"}]}}
  ```

## Data Storage Verification

- Session files contain actual assistant content: **YES** (verified — the
  stored `output_text` matches what the TUI displayed, e.g. "done").
- Tool calls recorded with names, inputs, and outputs: **YES** (verified —
  `write_file` with `{"contents":"hello","path":"hello.txt"}`).
- Secondary storage: rollout trace bundles under
  `$SARVAM_HOME/rollout-traces/` (debugging artifacts; not needed for the
  protocol).
- Hook data flow: **VERIFIED** — stdin JSON present for all eight events;
  `transcript_path` resolves to the live native session file.
- Verification method: probe project at a temp dir with capture hooks for all
  8 events; payloads in `testdata/hook-payloads/`; scripted in
  `scripts/verify-sarvam.sh`.

## Protocol Mapping

| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | — | Static metadata | Required |
| `detect` | `sarvam-code` on PATH | Also true when `$SARVAM_HOME` exists | Required |
| `get-session-id` | hook input `session_id` | Echo; generate stub when empty | Required |
| `get-session-dir` | `$SARVAM_HOME/sessions` | Honor `SARVAM_HOME` env | Required |
| `resolve-session-file` | rollout filename suffix | Glob `**/rollout-*-<id>.jsonl` under dir; fallback `<dir>/<safe-id>.jsonl` | Required |
| `read-session` | AgentSessionJSON | Round-trip via fallback path | Required |
| `write-session` | AgentSessionJSON | Write to resolved path | Required |
| `read-transcript` | raw rollout bytes | Passthrough | Required |
| `chunk-transcript` | raw bytes | Base64 chunks of `--max-size` | Required |
| `reassemble-transcript` | reverse chunk | Concatenate | Required |
| `format-resume-command` | `sarvam-code resume` | `sarvam-code resume <session-id>` | Required |
| `parse-hook` | native hook JSON → EventJSON | Dual-format: accept native sarvam JSON (has `hook_event_name`) and protocol HookInputJSON (has `hook_type`) | Required |
| `install-hooks` | project `.sarvam/config.toml` | Upsert `[[hooks.*]]` matcher-group entries; `matcher = ".*"` for tool events; plain-`entire` command via PATH guard | If hooks capable |
| `uninstall-hooks` | reverse of install | Remove only entire-owned entries | If hooks capable |
| `are-hooks-installed` | project config check | All installed events present | If hooks capable |
| `get-transcript-position` | file size | Byte offset (transcript is append-only) | If transcript analyzer |
| `extract-modified-files` | `function_call` entries | `write_file`/`edit_file` `path`; `exec_command` shell parsing | If transcript analyzer |
| `extract-prompts` | user messages | `input_text`, drop `<environment_context>` | If transcript analyzer |
| `extract-summary` | — | No native summary in session files → `has_summary=false` | If transcript analyzer |
| `calculate-tokens` | `token_count` events | Sum `last_token_usage` since offset; count non-null `info` for `api_call_count` | If token calculator |

## Selected Capabilities

| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | Native TOML hook config; install/uninstall/is-installed map directly |
| transcript_analyzer | true | Native JSONL transcripts with full content |
| transcript_preparer | false | Native transcript already structured; no preprocessing needed |
| token_calculator | true | `token_count` events carry per-call usage |
| text_generator | false | Would require live model API per invocation; out of scope |
| hook_response_writer | false | Hook stdout already returns to the agent, but the full writer contract is unverified |
| subagent_aware_extractor | false | Subagent threads live in separate rollout files; defer |
| uses_terminal | true | Sarvam Code is an interactive TUI agent |

## Gaps & Limitations

- **Headless runs are invisible to hooks.** `sarvam-code exec` never fires
  hooks (verified). Checkpoint capture requires interactive sessions; the e2e
  adapter must drive the TUI through tmux, and `RunPrompt` cannot use exec.
- **One-time interactive bootstrap per repo.** After `install-hooks`, the user
  must (1) trust the directory on first sarvam launch and (2) review + enable
  the hooks in `/hooks`. The `trusted_hash` scheme is opaque, so the binary
  cannot pre-seed hook state; automating it would bypass sarvam's security
  model. Same class of caveat as Grok's `/hooks-trust`.
- **Tool hooks need a matcher.** Declarations for `PreToolUse`/`PostToolUse`
  must set `matcher` or they silently never fire (verified). The installer
  sets `matcher = ".*"`.
- `extract-summary` returns `has_summary=false`: session files have no
  native summary object (compaction summaries live in trace bundles, not the
  rollout JSONL).
- Interactive-only hook firing also means the compliance suite's hook tests
  validate install/uninstall/parse behavior only — real hook delivery is
  covered by the lifecycle harness.

## Captured Payloads

- Verification script: `scripts/verify-sarvam.sh`
- Captures: `testdata/hook-payloads/{SessionStart,UserPromptSubmit,PreToolUse,PostToolUse,PermissionRequest,PreCompact,PostCompact,Stop}.json`
- Verification status: **VERIFIED** (all 8 events captured from live
  sarvam-code 0.58.0 sessions on 2026-11-16)
- Notable differences from docs: the documented `SessionStart` hook config
  example omits the `[[hooks.SessionStart]]` matcher-group header and is
  rejected by the CLI; tool hooks require an explicit `matcher`; hooks do not
  fire in exec mode (docs do not state this).

## E2E Test Prerequisites

- Entire CLI binary: `entire` from PATH (`E2E_ENTIRE_BIN` override)
- Agent CLI binary: `sarvam-code`
- Non-interactive prompt command: `sarvam-code exec --skip-git-repo-check -C <dir> "<prompt>"` — usable for file-creation assertions only; hooks do NOT fire, so checkpoints require the interactive path
- Interactive mode: supported — `sarvam-code` TUI; drive via tmux
- Expected prompt pattern: the TUI status bar line, e.g. `Flow · (Auto|Ask|Yolo)` — the composer line is `› `
- Timeout multiplier: 2.0 (full agent turns with model latency)
- Bootstrap steps: login already done in the environment; per-repo: directory trust + `/hooks` review (see Gaps)
- Transient error patterns: `overloaded`, `rate limit`, `429`, `500`, `503`, `econnreset`, `etimedout`, `timeout`
