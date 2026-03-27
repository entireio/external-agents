# Mistral Vibe — External Agent Research

## Verdict: COMPATIBLE

Mistral Vibe has excellent session management, parseable transcripts, and a **full lifecycle hook system** added in the `lifecycle-hooks` branch. The external agent binary can implement all required protocol subcommands including real-time lifecycle event tracking via hooks.

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `/Users/alisha/.local/bin/vibe` (Python script via uv) |
| Help available | PASS | Rich argparse help with all commands |
| Version info | PASS | `vibe 2.6.2` |
| Hook keywords | PASS (verified) | `HookManager`, `HooksConfig`, `emit_session_start`, `emit_turn_end` in source |
| Session keywords | PASS | `--resume`, `--continue`, `session` in help |
| Config directory | PASS | `~/.vibe/` with `config.toml`, `logs/`, `trusted_folders.toml` |
| Documentation | PASS | https://docs.mistral.ai/mistral-vibe/introduction |

## Binary
- Name: `vibe`
- Version: 2.6.2
- Install: `uv tool install mistral-vibe` or `pip install mistral-vibe`
- Location: `~/.local/bin/vibe` (uv-managed Python script)
- Package: `mistral-vibe` on PyPI
- Requires: `MISTRAL_API_KEY` environment variable

## Hook Mechanism
- Config file: `.vibe/config.toml` (project-level) or `~/.vibe/config.toml` (global)
- Config format: TOML
- Hook registration: Array of tables under `[hooks]` section, each with a `command` field
- Config example:
  ```toml
  [[hooks.session_start]]
  command = "entire hooks mistral-vibe session-start"

  [[hooks.user_prompt_submit]]
  command = "entire hooks mistral-vibe user-prompt-submit"

  [[hooks.pre_tool_use]]
  command = "entire hooks mistral-vibe pre-tool-use"

  [[hooks.post_tool_use]]
  command = "entire hooks mistral-vibe post-tool-use"

  [[hooks.turn_end]]
  command = "entire hooks mistral-vibe turn-end"
  ```
- Hook names and protocol mapping (verified):
  | Native Hook Name | When It Fires | Protocol Event Type |
  |-----------------|---------------|---------------------|
  | `session_start` | First call to `act()` in AgentLoop | 1 = SessionStart |
  | `user_prompt_submit` | Each prompt submitted to `act()` | 2 = TurnStart |
  | `pre_tool_use` | Before tool execution | nil (no event emitted) |
  | `post_tool_use` | After tool execution | nil (captures tool calls) |
  | `turn_end` | End of `act()` turn, after `_save_messages()` | 3 = TurnEnd |
- Hook input format (verified): JSON on stdin with these fields:
  - All events: `hook_event_name`, `cwd`, `session_id`
  - `user_prompt_submit`: adds `prompt` (string)
  - `pre_tool_use`: adds `tool_name` (string), `tool_input` (object)
  - `post_tool_use`: adds `tool_name`, `tool_input`, `tool_outcome` ("success"/"failed"/"cancelled"/"skipped"), `tool_response` (object|null), `tool_error` (string|null)
- Hook execution: async subprocess via `asyncio.create_subprocess_shell`, non-blocking, 30s timeout, stderr logged
- Drain: `HookManager.drain()` called at turn end to await all pending hook tasks
- Note: Vibe does NOT have a dedicated "session_end" or "stop" hook. The `turn_end` hook fires at the end of each turn, not at session teardown.

## Session Management
- Session directory: `~/.vibe/logs/session/` (configurable via `session_logging.save_dir` in config)
- Session ID source: UUID generated at session start (e.g., `0e9f7293-0151-4178-ba58-2c48c5abb8df`) (verified)
- Session folder naming: `session_YYYYMMDD_HHMMSS_<first-8-chars-of-session-id>` (verified)
- Session file format: Directory containing:
  - `meta.json` — Session metadata (session_id, start_time, end_time, git info, stats, tools, config)
  - `messages.jsonl` — Full conversation transcript (one JSON object per line)
- Session prefix: Configurable via `session_logging.session_prefix` (default: `"session"`)
- Resume command: `vibe --resume <SESSION_ID>` or `vibe -c` (most recent)

## Transcript
- Location: `~/.vibe/logs/session/session_*_<id>/messages.jsonl`
- Format: JSONL (newline-delimited JSON)
- Each line is an `LLMMessage` object (verified):
  ```json
  {"role": "user", "content": "can you create hello world golang", "message_id": "5bf535d7-cb5c-45f6-8872-3ba26703475a"}
  {"role": "assistant", "tool_calls": [{"id": "g4WVM7SD2", "index": 0, "function": {"name": "write_file", "arguments": "{...}"}, "type": "function"}], "message_id": "61dec2f6-920a-4c66-9b67-a92dbff14fb5"}
  {"role": "tool", "content": "path: /path/to/file\nbytes_written: 73\n...", "name": "write_file", "tool_call_id": "g4WVM7SD2"}
  {"role": "assistant", "content": "Created `hello.go`:\n...", "message_id": "3cac263a-e70d-4cf9-b765-170f4d54a830"}
  ```
- User prompt field: `.content` where `.role == "user"`
- Modified files field: Extracted from `.tool_calls[].function.arguments` where tool name is `write_file`, `search_replace`, or `bash`
- Token usage: Available in `meta.json` under `stats.session_prompt_tokens` and `stats.session_completion_tokens`
- Tool calls: Stored in assistant messages as `tool_calls` array with `function.name` and `function.arguments`

## Data Storage Verification
- Session files contain actual assistant content: YES — full LLM responses stored in messages.jsonl (verified)
- Secondary storage location: None needed — messages.jsonl contains complete conversation
- Cross-reference key: Session ID (first 8 chars) in folder name
- Hook data flow verified: YES — stdin JSON payloads confirmed via `/tmp/vibe-hooks/` captures
- Hook payloads include: session_id, cwd, prompt, tool_name, tool_input, tool_outcome, tool_response, tool_error
- Verification method: hook-logger.py script in test repo, captures at `/tmp/vibe-hooks/all-events.jsonl`

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | — | Static metadata, always implementable | Required |
| `detect` | `~/.vibe/` dir, `vibe` binary | Check for binary in PATH or config dir | Required |
| `get-session-id` | `session_id` from hook stdin JSON | Extract from hook input JSON `session_id` field | Required |
| `get-session-dir` | `~/.vibe/logs/session/` | Return session log directory | Required |
| `resolve-session-file` | `session_*_<id>/messages.jsonl` | Glob for matching session folder by first 8 chars of ID | Required |
| `read-session` | `meta.json` + `messages.jsonl` | Parse session metadata and files | Required |
| `write-session` | Write to session dir | Create/update session files | Required |
| `read-transcript` | `messages.jsonl` | Read raw JSONL bytes | Required |
| `chunk-transcript` | Base64 chunking | Language-generic byte chunking | Required |
| `reassemble-transcript` | Base64 reassembly | Language-generic byte reassembly | Required |
| `format-resume-command` | `vibe --resume <id>` | Format resume command string | Required |
| `parse-hook` | Hook stdin JSON (verified) | Map `hook_event_name` → protocol event type. `session_start`→1, `user_prompt_submit`→2, `turn_end`→3 | Required (hooks capable) |
| `install-hooks` | `.vibe/config.toml` `[hooks]` section | Write TOML hook entries pointing to `entire hooks mistral-vibe <event>` | Required (hooks capable) |
| `uninstall-hooks` | Remove `[hooks]` entries | Remove Entire-owned hook entries from config | Required (hooks capable) |
| `are-hooks-installed` | Check config for hook commands | Grep config.toml for `entire hooks mistral-vibe` | Required (hooks capable) |
| `get-transcript-position` | File size of messages.jsonl | `os.Stat().Size()` | Feasible |
| `extract-modified-files` | Parse JSONL tool calls | Scan for write_file/search_replace/bash tool calls | Feasible |
| `extract-prompts` | Parse JSONL user messages | Filter `.role == "user"` entries | Feasible |
| `extract-summary` | Last assistant message | Return last assistant content block | Feasible |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | Full lifecycle hook system: session_start, user_prompt_submit, pre_tool_use, post_tool_use, turn_end (verified) |
| transcript_analyzer | true | messages.jsonl is structured JSONL with full tool calls and user messages |
| transcript_preparer | false | JSONL is already in a directly parseable format |
| token_calculator | true | Token usage is available in meta.json stats |
| text_generator | false | Would require API key management — out of scope |
| hook_response_writer | false | Hooks are fire-and-forget, no response mechanism |
| subagent_aware_extractor | false | Vibe does not spawn subagents with separate transcripts |

## Gaps & Limitations
- **No session_end hook**: Vibe has `turn_end` but no dedicated session teardown hook. The `turn_end` at the last turn effectively serves as session end.
- **No stop hook**: Unlike Kiro's `stop` hook, Vibe fires `turn_end`. The external agent maps `turn_end` → TurnEnd (protocol event type 3).
- **Session discovery requires glob**: Session folders use timestamps in names, so finding a session by ID requires globbing `session_*_<id-prefix>`.
- **Hook config is in main config.toml**: Unlike Kiro which has separate hook files, Vibe hooks are in `.vibe/config.toml` `[hooks]` section. The install-hooks subcommand must read/modify TOML.
- **Python-only**: Vibe is a Python tool — the external agent binary (Go) interacts with it only through filesystem and CLI.
- **Token usage**: Available per-session in meta.json but not per-message in the JSONL.
- **tool_outcome field**: post_tool_use includes `tool_outcome` ("success"/"failed"/"cancelled"/"skipped") which is not present in all agents — useful for accurate tracking.

## Captured Payloads
- Verification script: `agents/entire-agent-mistral-vibe/scripts/verify-mistral-vibe.sh`
- Hook logger: `/Users/alisha/Projects/test-repos/vibe-hooks-test/.vibe/hook-logger.py`
- Capture directory: `/tmp/vibe-hooks/`
- Verification status: VERIFIED (hook payloads captured from real Vibe session)
- Verified payloads:
  - `session_start`: `{"hook_event_name":"session_start","cwd":"/Users/alisha/Projects/test-repos/vibe-hooks-test","session_id":"0e9f7293-0151-4178-ba58-2c48c5abb8df"}`
  - `user_prompt_submit`: adds `"prompt":"hello"`
  - `pre_tool_use`: adds `"tool_name":"write_file","tool_input":{...}`
  - `post_tool_use`: adds `"tool_name":"write_file","tool_input":{...},"tool_outcome":"success","tool_response":{...},"tool_error":null`
  - `turn_end`: same base fields as session_start
- Notable: `session_start` and `user_prompt_submit` may fire out of order (both fire on first prompt, async)

## E2E Test Prerequisites
- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN` env (found at `/opt/homebrew/bin/entire`)
- Agent CLI binary: `vibe` at `~/.local/bin/vibe`
- Non-interactive prompt command: `vibe -p "prompt text" --output json --workdir <dir>`
- Interactive mode: `vibe` (launches Textual TUI) — not suitable for automated E2E tests
- Expected prompt pattern: N/A (programmatic mode exits after completion, no interactive prompt)
- Timeout multiplier: 2.0 (API calls to Mistral can be slow)
- Bootstrap steps: Requires `MISTRAL_API_KEY` environment variable
- Transient error patterns: `"Rate limits exceeded"`, `"overloaded"`, `"429"`, `"Too Many Requests"`, `"BackendError"`, `"timeout"`
