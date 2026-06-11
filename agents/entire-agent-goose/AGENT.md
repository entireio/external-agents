# Goose — External Agent Research

## Verdict: COMPATIBLE

Goose (Block's open-source AI coding agent, Rust) has a native lifecycle hooks
system, durable session storage with full message content and token totals, and
a headless run mode. All findings below were verified against goose 1.37.0 with
real captured payloads unless marked `(unverified)`.

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `~/.local/bin/goose` |
| Help available | PASS | `goose --help` |
| Version info | PASS | `1.37.0` |
| Hook keywords | PASS | hooks system in source (`crates/goose/src/hooks/mod.rs`); not surfaced in `--help` |
| Session keywords | PASS | `session`, `--resume`, `export` |
| Config directory | PASS | `~/.config/goose/config.yaml` (YAML) |
| Documentation | PASS | Open Plugins hooks spec: https://open-plugins.com/agent-builders/components/hooks |

## Binary
- Name: `goose`
- Version: 1.37.0
- Install: `curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | bash` (or Homebrew `brew install block-goose-cli`)

## Hook Mechanism
- Config file: `<project-root>/.agents/plugins/<plugin-name>/hooks/hooks.json` (project scope) or `~/.agents/plugins/<plugin-name>/hooks/hooks.json` (user scope) **(verified, project scope)**
- Config format: JSON (Open Plugins hooks spec)
- Hook registration: drop a plugin directory containing `hooks/hooks.json`; plugins are **enabled by default** when present (no settings change needed). `enabledPlugins`/`disabledPlugins` in `~/.config/goose/settings.json` or `<project>/.config/goose/settings.json` / `settings.local.json` can override. Project root for discovery = goose's working directory.
- Hook actions: `{"type": "command", "command": "...", "timeout": <secs>}`; command runs via `sh -c` with `PLUGIN_ROOT` env var and `${PLUGIN_ROOT}` placeholder substitution; default timeout 30s; exit 0 = success. `Stop` and `PreToolUse` are blocking: exit code 2 or `{"decision":"block","reason":"..."}` on stdout denies.
- Hook names and protocol mapping **(all verified via captures)**:
  | Native Hook Name | When It Fires | Protocol Event Type |
  |-----------------|---------------|---------------------|
  | `SessionStart` | session begins (interactive + headless `goose run`) | 1=SessionStart |
  | `UserPromptSubmit` | user prompt accepted, before agent works | 2=TurnStart |
  | `Stop` | agent finished responding (blocking hook — always exit 0) | 3=TurnEnd |
  | `SessionEnd` | session terminates | 5=SessionEnd |
  | `SubagentStart` / `SubagentStop` | subagent spawned/completed | 6/7 (not declared in v1) |
  | `PreToolUse` / `PostToolUse` / `PostToolUseFailure` | around each tool call | not needed (modified files come from transcript) |
  | `BeforeReadFile` / `AfterFileEdit` / `BeforeShellExecution` / `AfterShellExecution` | file/shell granularity | not needed |
  - There is **no Compaction hook** (event type 4). Goose compacts context internally without emitting a hook.
- Hook input format: JSON on stdin (`HookContext`, snake_case). Captured payloads:
  ```json
  {"event":"SessionStart","session_id":"20260611_1","matcher_context":null}
  {"event":"UserPromptSubmit","session_id":"20260611_1","matcher_context":"<prompt>","message":"<prompt>"}
  {"event":"Stop","session_id":"20260611_1","matcher_context":null}
  {"event":"SessionEnd","session_id":"20260611_1","matcher_context":null}
  ```
  Tool events additionally carry `tool_name`, `tool_input`, and `working_dir`.
  **Session-level events do NOT include `working_dir`, `session_ref`, or a timestamp** — the binary derives the transcript reference from `session_id` and stamps events at receipt time.

## Session Management
- Session directory: `${XDG_DATA_HOME:-~/.local/share}/goose/sessions/` **(verified on macOS)**. `GOOSE_PATH_ROOT` env var overrides all goose paths (`$GOOSE_PATH_ROOT/data/sessions/`) — useful for hermetic e2e tests.
- Session ID source: `session_id` field of every hook payload. Format `YYYYMMDD_N` (e.g. `20260611_1`), incrementing per day.
- Session file format: **SQLite database** `sessions.db` (tables: `sessions`, `messages`, `threads`). There is no per-session file; stray `*.jsonl` files in the sessions dir are empty legacy artifacts.
- `sessions` table carries `working_dir`, `created_at`/`updated_at` (UTC `YYYY-MM-DD HH:MM:SS`), `provider_name`, `model_config_json`, and token totals. Sessions for a repo are findable via `WHERE working_dir = ?`.

## Transcript
- Location: no native file. Recommended approach **(verified)**: materialize via
  `goose session export --session-id <id> --format json`, written to
  `<session-dir>/<session-id>.export.json` by the `prepare-transcript` subcommand
  (`transcript_preparer` capability). `session_ref` = that exported path.
- Format: single JSON object with session metadata + `conversation` array of messages.
- Export top-level fields **(verified)**: `id`, `working_dir`, `name`, `session_type`, `created_at`/`updated_at` (RFC 3339), `total_tokens`, `input_tokens`, `output_tokens`, `accumulated_total_tokens`, `accumulated_input_tokens`, `accumulated_output_tokens`, `accumulated_cost`, `conversation`, `message_count`, `provider_name`, `model_config` (has `model_name`).
- Message schema (camelCase content, unlike snake_case hook payloads) **(verified)**:
  ```json
  {"id":"msg_...","role":"user","created":1781175769,
   "content":[{"type":"text","text":"..."}],
   "metadata":{"userVisible":true,"agentVisible":true}}
  ```
- User prompt field: `conversation[].content[].text` where `role=="user"` and `content[].type=="text"` (skip `toolResponse` user messages).
- Modified files field: `conversation[].content[]` where `type=="toolRequest"`, `toolCall.value.name` in `write`/`text_editor`/`edit`-style tools; file path at `toolCall.value.arguments.path`. Shell commands at `arguments.command` for heuristics.
- Token usage field: session-level `accumulated_input_tokens`/`accumulated_output_tokens` (export and `sessions` table). Per-message `tokens` column exists but was NULL in verification. No cache token split available.
- Example tool entry **(verified)**:
  ```json
  {"type":"toolRequest","id":"toolu_...","toolCall":{"status":"success",
   "value":{"name":"write","arguments":{"path":"/abs/path/verify.txt","content":"..."}}}}
  ```

## Data Storage Verification
- Session files contain actual assistant content: **YES** — real response text (`ENTIRE_VERIFY_OK`), full tool calls with arguments, and tool results; no placeholders.
- Secondary storage location: none needed — `sessions.db` / `goose session export` is complete.
- Cross-reference key: `session_id` (hook payload) → `sessions.id` / `messages.session_id` / export `--session-id`.
- Hook data flow verified: **YES** — stdin JSON received for all 6 session/turn events plus tool events; `PLUGIN_ROOT` env set; no stdin redirect issues.
- Verification method: capture plugin in probe workspace + `goose run -t` with distinctive strings, then grepped `sessions.db` and export output.

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | — | static metadata; `protected_dirs: [".agents"]` | Required |
| `detect` | `goose` binary | `exec.LookPath("goose")` | Required |
| `get-session-id` | hook `session_id` | from HookInput | Required |
| `get-session-dir` | sessions dir | `$GOOSE_PATH_ROOT/data/sessions` else `${XDG_DATA_HOME:-~/.local/share}/goose/sessions` | Required |
| `resolve-session-file` | export path | `<session-dir>/<session-id>.export.json` | Required |
| `read-session` | export JSON | session metadata from export/DB; `start_time` from `created_at` | Required |
| `write-session` | — | persist AgentSession native_data alongside export (no goose import needed for checkpoint flow) | Required |
| `read-transcript` | export file | raw bytes of `session_ref` | Required |
| `chunk-transcript` / `reassemble-transcript` | — | generic base64 chunking | Required |
| `format-resume-command` | resume flag | `goose session --resume --session-id <id>` | Required |
| `parse-hook` | HookContext JSON | map per table above; TurnStart prompt from `message` | hooks |
| `install-hooks` | project plugin | write `<repo>/.agents/plugins/entire/hooks/hooks.json` calling the entire hook handler | hooks |
| `uninstall-hooks` | — | remove that plugin dir | hooks |
| `are-hooks-installed` | — | check plugin dir + hooks.json content | hooks |
| `get-transcript-position` | export size | file size (re-export shifts bytes; position resets via prepare) | transcript_analyzer |
| `extract-modified-files` | `toolRequest` entries | parse `conversation` for write/edit tool calls | transcript_analyzer |
| `extract-prompts` | user text messages | parse `conversation` | transcript_analyzer |
| `extract-summary` | session `name` | auto-generated session name (`"File verification request"`) as summary | transcript_analyzer |
| `prepare-transcript` | `goose session export` | shell out: `goose session export --session-id <id> --format json -o <path>` | transcript_preparer |
| `calculate-tokens` | export token fields | `accumulated_input/output_tokens`; `api_call_count` = assistant message count | token_calculator |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | native hooks.json plugin system, verified end-to-end |
| transcript_analyzer | true | export JSON has prompts, tool calls with file paths |
| transcript_preparer | true | transcript must be materialized from sessions.db via `goose session export` |
| token_calculator | true | accumulated input/output tokens in export |
| text_generator | false | possible later via headless `goose run -t --no-session`, but output includes UI text; defer |
| hook_response_writer | false | no clean native channel for display-only messages (Stop deny blocks the turn) |
| subagent_aware_extractor | false | subagent sessions are separate `sessions.db` rows (`session_type='subagent'`), not per-dir transcripts; defer |

## Gaps & Limitations
- Hook payloads have **no timestamp and no session file reference** — events are stamped at receipt; transcript path is derived from `session_id`.
- Session-level hook events lack `working_dir` (tool events have it). Entire sets cwd to repo root, so this doesn't block.
- **No Compaction hook** — goose's internal context compaction can't trigger event type 4; token deltas may jump after compaction.
- Per-message token counts were NULL in verification; only session-level accumulated totals are reliable.
- `prepare-transcript` shells out to `goose` (must be on PATH — already guaranteed by `detect`). Direct SQLite reads are a fallback but are exposed to schema migrations (schema v10 at time of research).
- Hook payload contains full `tool_input` (file contents) — payloads can be large.

## Captured Payloads
- Verification script: `agents/entire-agent-goose/scripts/verify-goose.sh`
- Capture directory: `agents/entire-agent-goose/.probe-goose-*/captures/`
- Verification status: **VERIFIED** — script ran headless `goose run -t`; all 6 session/turn/tool event payloads captured; stored data completeness confirmed against sessions.db and `goose session export`.
- Notable differences from docs: none material; source matched behavior. Hook JSON is snake_case while exported message content is camelCase — easy to conflate.

## E2E Test Prerequisites
- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN`
- Agent CLI binary: `goose` (1.37.0 verified); install via download_cli.sh or Homebrew
- Non-interactive prompt command: `goose run -t "<prompt>"` (runs SessionStart → UserPromptSubmit → Stop → SessionEnd)
- Interactive mode: supported — `goose session`; resume via `goose session --resume --session-id <id>`
- Expected prompt pattern: `goose is ready` banner on session start (unverified for interactive input prompt regex)
- Timeout multiplier: 2.0 (provider on this machine: openrouter / anthropic claude — network-bound)
- Bootstrap steps: provider must be configured (`~/.config/goose/config.yaml` + keyring/secret); `GOOSE_PATH_ROOT=<tmp>` gives hermetic config/data dirs but then provider config must be seeded under `$GOOSE_PATH_ROOT/config/`
- Transient error patterns: `rate limit`, `overloaded`, `503`, `529`, `tunnel`
