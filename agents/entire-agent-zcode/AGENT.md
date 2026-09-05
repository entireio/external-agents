# ZCode — External Agent Research

## Verdict: COMPATIBLE (hooks + SQLite sessions verified on a live install; headless lifecycle NOT possible — desktop app only)

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `/usr/bin/zcode` (symlink → `/etc/alternatives/zcode` → `/opt/ZCode/zcode`) |
| Help available | WARN | `zcode --help` starts the Electron desktop app; no CLI help text |
| Version info | PASS | User-agent strings in rollout log show `ZCode/3.10.2`; also `~/.zcode/v2/config.json` |
| Hook keywords | PASS | Documented hooks system (7 events) — see below |
| Session keywords | PASS | SQLite `session`/`message`/`part` tables with `parent_id` (subagents), resume via UI |
| Config directory | PASS | `~/.zcode/cli/` (config.json, db/, exec/, rollout/, log/) |
| Documentation | PASS | https://zcode.z.ai/en/docs/hooks (hook schema), /en/docs/welcome |

## Binary
- Name: `zcode`
- Version: 3.10.2 (verified on this machine)
- Install: Desktop app from https://zcode.z.ai (Electron; `.dmg`/Linux install under `/opt/ZCode`). No standalone headless CLI exists or is documented.

## Hook Mechanism
- Config file: `~/.zcode/cli/config.json` (user-wide — **the only executed configuration-file source**; project-level `.zcode/config.json` / `zcode.json` hooks are ignored for security)
- Config format: JSON
- Hook registration: top-level `hooks` key:
  ```json
  {
    "hooks": {
      "enabled": true,
      "events": {
        "SessionStart": [ { "matcher": "...", "hooks": [ { "type": "process", "command": "...", "args": [...], "timeoutMs": 60000 } ] } ]
      }
    }
  }
  ```
  `hooks.enabled: true` is REQUIRED or nothing runs. `type: "process"` (command + args, no shell) or `type: "command"` (shell string). Matcher is a case-sensitive regex.
- Hook names and protocol mapping:
  | Native Hook Name | When It Fires | Protocol Event Type |
  |-----------------|---------------|---------------------|
  | `SessionStart` (source=startup/resume/clear) | Session begins | 1 = SessionStart |
  | `SessionStart` (source=compact) | Context compaction | 4 = Compaction |
  | `UserPromptSubmit` | User submits a prompt | 2 = TurnStart (prompt) |
  | `Stop` | Assistant finished responding | 3 = TurnEnd (response_message) |
  | `PreToolUse` | Before a tool call | not mapped (skip; no protocol tool event type) |
  | `PostToolUse` | After a tool call | not mapped (skip) |
  | `PermissionRequest` / `PostToolUseFailure` | permission/failure | not mapped (skip) |
- Hook input format: **one line of JSON on stdin**, containing camelCase + Claude-style snake_case aliases:
  - Common: `session_id`, `transcript_path`, `cwd`, `permission_mode`, `hook_event_name`
  - SessionStart: `source` (startup/clear/compact), `agent_type`, `model`
  - UserPromptSubmit: `prompt`
  - PreToolUse/PostToolUse: `tool_name`, `tool_input`, `tool_use_id`
  - Stop: `stop_hook_active`, `last_assistant_message`
  - `transcript_path` is a **temp JSONL cleaned after the hook runs — NOT persistent storage**; real data lives in SQLite.
  - Hook output: exit 0 = pass, 2 = block, stdout JSON parsed with a strict schema (we emit nothing — hooks are observe-only).
- Env vars: `${CLAUDE_SESSION_ID}`, `${ZCODE_PROJECT_DIR}`/`${CLAUDE_PROJECT_DIR}` are expanded into the command/args. Config is snapshotted per session — hook changes need a new session.

## Session Management
- Session directory (native): `~/.zcode/cli/db/db.sqlite` (SQLite, WAL mode) — single DB, not per-session files
- Session ID source: `session_id` in hook stdin (`sess_<uuid>`), e.g. `sess_58a1da30-14cb-4dda-99bf-82f56fb30881`
- Session file format: tables `session(id, project_id, workspace_id, parent_id, directory, title, time_created, time_updated, ...)`, `message(id, session_id, sequence, data JSON)`, `part(id, message_id, session_id, sequence, data JSON)` (verified via schema dump)
  - `message.data`: `{"role": "user"|"assistant", "time": {...}, "model": {...}, "tokens": {"total","input","output","reasoning","cache":{"read","write"}}, "semantics": {"origin","kind","transcriptVisibility"}, ...}`
  - `part.data`: `{"type":"text","text":"..."}` or `{"type":"tool","callID":"...","tool":"Read|Bash|Edit|Write|...","state":{"status":"completed","input":{...},"output":"..."}}`
  - `session.parent_id` links subagent sessions to their parent; `semantics.transcriptVisibility` distinguishes hidden/synthetic messages
- Other storage: `~/.zcode/cli/rollout/model-io-sess_<id>.jsonl` (raw LLM request/response log), `~/.zcode/cli/exec/sess_<id>/` (tool exec logs) — supplementary only

## Transcript
- Location (effective): exported by our binary from SQLite into `~/.zcode/entire/sessions/<session_id>.jsonl` (via `prepare-transcript`); raw transcript bytes read from that file
- Format: JSONL, one message object per line (our export format: `{role, text, tool_name, tool_input, tool_output, tokens{...}, time}` filtered to `transcriptVisibility: "visible"`)
- User prompt field: message with `role=user` + `semantics.kind="user_prompt"` + first text part
- Modified files field: `part.data.type="tool"` with `tool` ∈ {Write, Edit, ApplyPatch} → `state.input.file_path`/`path`
- Token usage field: `message.data.tokens.{input,output,cache.read,cache.write}` per assistant message
- Example entry: `{"role":"assistant","text":"Found it.","tokens":{"input":15786,"output":163}}`

## Data Storage Verification
- Session files contain actual assistant content: YES — `part.data` holds full response text and tool inputs/outputs (verified by dumping rows from the live DB on this machine)
- Secondary storage: none required; `rollout/model-io-sess_*.jsonl` and `exec/sess_*/` exist but are supplementary
- Cross-reference key: `message.session_id` → `session.id`; `part.message_id` → `message.id`; `session.parent_id` for subagents
- Hook data flow verified: YES per docs (stdin JSON with `session_id`, `prompt`, `tool_name`); `transcript_path` is temp-only so we ignore it and read from SQLite instead. Marked (unverified-by-capture) — no hook payload was captured live in this environment yet; see Captured Payloads.
- Verification method: sqlite3 schema dump + row sampling on the live DB; official hooks docs; local zcode-guide plugin skill (`diagnosing-hooks`) cross-check

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | — | Static metadata | Required |
| `detect` | binary + DB | `exec.LookPath("zcode")` or `~/.zcode/cli/db` exists | Required |
| `get-session-id` | hook stdin `session_id` | Read HookInput / raw payload | Required |
| `get-session-dir` | — | `~/.zcode/entire/sessions` (outside the repo: Entire attributes transcripts by session-dir prefix, and `.entire/tmp` is claimed by other external agents like kilo/amp) | Required |
| `resolve-session-file` | — | `<session_dir>/<session_id>.jsonl` (our export) | Required |
| `read-session` | SQLite | Query session+message+part, build AgentSession; native_data = session row JSON | Required |
| `write-session` | sidecar | Persist AgentSession JSON snapshot to `<session_dir>/<id>.json` (never writes to ZCode's DB — resume-restore is GUI-driven) | Required (partial fidelity) |
| `read-transcript` | export file | Raw bytes of exported JSONL | Required |
| `chunk-transcript` / `reassemble-transcript` | — | Base64 chunking (generic) | Required |
| `format-resume-command` | GUI resume | `zcode` (opens the app; session list resume) — no headless resume exists | Required (degraded) |
| `parse-hook` | hook stdin JSON | Map SessionStart/UserPromptSubmit/Stop/compact as per table above | hooks |
| `install-hooks` | user config.json | Backup, merge `hooks.events.{SessionStart,UserPromptSubmit,Stop}` + `enabled:true`, restore-able | hooks |
| `uninstall-hooks` | reverse | Remove only entries we added | hooks |
| `are-hooks-installed` | config check | Look for our marker command in config | hooks |
| `get-transcript-position` | export file | Byte size of exported JSONL | transcript_analyzer |
| `extract-modified-files` | tool parts | Parse Write/Edit/ApplyPatch parts from JSONL | transcript_analyzer |
| `extract-prompts` | user messages | `role=user`, `kind=user_prompt` text parts | transcript_analyzer |
| `extract-summary` | last assistant text | Last visible assistant text part | transcript_analyzer |
| `prepare-transcript` | SQLite export | Stream session → JSONL file | transcript_preparer |
| `calculate-tokens` | message tokens | Sum input/output/cache fields | token_calculator |
| `write-hook-response` | UserPromptSubmit additionalContext | `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":...}}` | hook_response_writer (optional) |
| `generate-text` | none | No headless model invocation — NOT declared | — |
| subagent extractors | `session.parent_id` | Possible via DB query; NOT declared in v1 | — |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | Full documented hook system with stdin JSON carrying session_id/prompt |
| transcript_analyzer | true | SQLite-exported JSONL is fully parseable |
| transcript_preparer | true | Transcripts must be exported from SQLite before reading (temp hook transcript is deleted) |
| token_calculator | true | Per-message token usage stored in `message.data.tokens` |
| text_generator | false | No headless CLI to invoke the model |
| hook_response_writer | false | UserPromptSubmit `additionalContext` output documented but not implemented in v1 |
| subagent_aware_extractor | false | Deferred: DB supports it via `parent_id`, but subagent-dir contract doesn't map cleanly in v1 |

## Gaps & Limitations
- **No headless CLI**: zcode is an Electron desktop app; there is no `-p`/`exec` mode. `format-resume-command` can only launch the GUI; lifecycle e2e prompts cannot be automated — the e2e adapter is opt-in (`ZCODE_E2E=1 && E2E_AGENT=zcode`) and documents the limitation.
- **write-session does not restore into ZCode**: real sessions live in ZCode's SQLite DB with an internal schema; writing into it is unsafe. write-session persists an opaque snapshot for Entire's benefit only (resume happens through the GUI session list).
- **Hook config is user-global** (`~/.zcode/cli/config.json`): install-hooks edits the user file (with backup + marker-scoped merge/uninstall) because workspace-level hook configs are ignored for security.
- **Config snapshotted per session**: hook changes take effect in new ZCode sessions only.
- Hook payloads documented but not captured live in this environment (would require manual GUI interaction) — mapping is doc-based, schema cross-checked against the local zcode-guide plugin skill.

## Captured Payloads
- Verification script: `agents/entire-agent-zcode/scripts/verify-zcode.sh`
- Capture directory: `agents/entire-agent-zcode/.probe-zcode-*/captures/`
- Verification status: PARTIAL — static checks + SQLite schema/rows verified on live install (ZCode 3.10.2, Linux); hook stdin payloads UNVERIFIED (doc-based; script captures them when the user runs a live session)
- Notable differences from docs: none found between docs and local skill/plugin documentation

## E2E Test Prerequisites
- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN`
- Agent CLI binary: `zcode` (desktop app; GUI only)
- Non-interactive prompt command: **NOT AVAILABLE** (no headless mode) — adapter returns an explanatory error
- Interactive mode: GUI only (tmux cannot drive an Electron app) — interactive lifecycle tests skip
- Expected prompt pattern: n/a
- Timeout multiplier: n/a
- Bootstrap steps: install ZCode desktop app and sign in; adapter registers only with `ZCODE_E2E=1` and `E2E_AGENT=zcode`
- Transient error patterns: "429", "rate limit", "overloaded", "503", "timeout"
