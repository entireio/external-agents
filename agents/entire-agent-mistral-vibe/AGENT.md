# Mistral Vibe — External Agent Research

## Verdict: PARTIAL

Mistral Vibe has excellent session management and parseable transcripts, but **no native hook/lifecycle system**. The external agent binary can implement session reading, transcript analysis, and detection, but cannot do real-time lifecycle event tracking via hooks. The Entire CLI will need to poll for session changes rather than receiving push notifications.

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `/Users/alisha/.local/bin/vibe` (Python script via uv) |
| Help available | PASS | Rich argparse help with all commands |
| Version info | PASS | `vibe 2.6.2` |
| Hook keywords | FAIL | No hook, lifecycle, callback, or event keywords in help |
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
- **Not available.** Mistral Vibe has no native hook/lifecycle system.
- The internal middleware system (`MiddlewarePipeline` with `before_turn` callbacks) is Python-level only and not configurable via external commands or config files.
- There is no `.vibe/hooks/` directory, no hook registration config, and no CLI commands for hooks.
- The agent supports custom "agents" (`.vibe/agents/NAME.toml`) but these are prompt profiles, not lifecycle hooks.

## Session Management
- Session directory: `~/.vibe/logs/session/` (configurable via `session_logging.save_dir` in config)
- Session ID source: UUID generated at session start (e.g., `a1b2c3d4-e5f6-7890-abcd-ef1234567890`)
- Session folder naming: `session_YYYYMMDD_HHMMSS_<first-8-chars-of-session-id>`
- Session file format: Directory containing:
  - `meta.json` — Session metadata (session_id, start_time, end_time, git info, stats, tools, config)
  - `messages.jsonl` — Full conversation transcript (one JSON object per line)
- Session prefix: Configurable via `session_logging.session_prefix` (default: `"session"`)
- Resume command: `vibe --resume <SESSION_ID>` or `vibe -c` (most recent)

## Transcript
- Location: `~/.vibe/logs/session/session_*_<id>/messages.jsonl`
- Format: JSONL (newline-delimited JSON)
- Each line is an `LLMMessage` object:
  ```json
  {"role": "user", "content": "Fix the login bug", "message_id": "uuid"}
  {"role": "assistant", "content": "I'll fix that.", "tool_calls": [...], "message_id": "uuid"}
  {"role": "tool", "content": "File written", "tool_call_id": "call_id", "name": "write_file"}
  ```
- User prompt field: `.content` where `.role == "user"`
- Modified files field: Extracted from `.tool_calls[].function.arguments` where tool name is `write_file`, `search_replace`, or `bash`
- Token usage: Available in `meta.json` under `stats.session_prompt_tokens` and `stats.session_completion_tokens`
- Tool calls: Stored in assistant messages as `tool_calls` array with `function.name` and `function.arguments`

## Data Storage Verification
- Session files contain actual assistant content: YES — full LLM responses stored in messages.jsonl
- Secondary storage location: None needed — messages.jsonl contains complete conversation
- Cross-reference key: Session ID (first 8 chars) in folder name
- Hook data flow verified: N/A — no hooks
- Verification method: Read messages.jsonl directly, grep for known response text

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | — | Static metadata, always implementable | Required |
| `detect` | `~/.vibe/` dir, `vibe` binary | Check for binary in PATH or config dir | Required |
| `get-session-id` | UUID from HookInput | Extract from hook input JSON (passed by CLI) | Required |
| `get-session-dir` | `~/.vibe/logs/session/` | Return session log directory | Required |
| `resolve-session-file` | `session_*_<id>/messages.jsonl` | Glob for matching session folder | Required |
| `read-session` | `meta.json` + `messages.jsonl` | Parse session metadata and files | Required |
| `write-session` | Write to session dir | Create/update session files | Required |
| `read-transcript` | `messages.jsonl` | Read raw JSONL bytes | Required |
| `chunk-transcript` | Base64 chunking | Language-generic byte chunking | Required |
| `reassemble-transcript` | Base64 reassembly | Language-generic byte reassembly | Required |
| `format-resume-command` | `vibe --resume <id>` | Format resume command string | Required |
| `parse-hook` | **N/A** | No native hooks — capability disabled | Not feasible |
| `install-hooks` | **N/A** | No hook config format to write | Not feasible |
| `uninstall-hooks` | **N/A** | No hooks to remove | Not feasible |
| `are-hooks-installed` | **N/A** | Always false | Not feasible |
| `get-transcript-position` | File size of messages.jsonl | `os.Stat().Size()` | Feasible |
| `extract-modified-files` | Parse JSONL tool calls | Scan for write_file/search_replace/bash tool calls | Feasible |
| `extract-prompts` | Parse JSONL user messages | Filter `.role == "user"` entries | Feasible |
| `extract-summary` | Last assistant message | Return last assistant content block | Feasible |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | false | No native hook mechanism — Vibe has no way to register external lifecycle callbacks |
| transcript_analyzer | true | messages.jsonl is structured JSONL with full tool calls and user messages |
| transcript_preparer | false | JSONL is already in a directly parseable format |
| token_calculator | true | Token usage is available in messages.jsonl assistant entries and meta.json stats |
| text_generator | false | Would require API key management — out of scope |
| hook_response_writer | false | No hooks to respond to |
| subagent_aware_extractor | false | Vibe does not spawn subagents with separate transcripts |

## Gaps & Limitations
- **No lifecycle hooks**: The biggest limitation. Without hooks, the Entire CLI cannot receive real-time session start/end/prompt events. Integration is limited to on-demand session reading and transcript analysis.
- **Session discovery requires glob**: Session folders use timestamps in names, so finding a session by ID requires globbing `session_*_<id-prefix>`.
- **No conversation ID in HookInput**: Since Vibe has no hooks, the session ID must be derived from the session folder name or meta.json.
- **Python-only**: Vibe is a Python tool — the external agent binary (Go) interacts with it only through filesystem and CLI.
- **Token usage**: Available per-session in meta.json but not per-message in the JSONL. Token calculation requires reading the metadata file.

## Captured Payloads
- Verification script: `agents/entire-agent-mistral-vibe/scripts/verify-mistral-vibe.sh`
- Capture directory: N/A (no hooks to capture)
- Verification status: VERIFIED (session storage confirmed via source code analysis)
- Notable differences from docs: Vibe's internal session format matches source code exactly. No hooks documentation exists because there are no hooks.

## E2E Test Prerequisites
- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN` env (found at `/opt/homebrew/bin/entire`)
- Agent CLI binary: `vibe` at `~/.local/bin/vibe`
- Non-interactive prompt command: `vibe -p "prompt text" --output json --workdir <dir>`
- Interactive mode: `vibe` (launches Textual TUI) — not suitable for automated E2E tests
- Expected prompt pattern: N/A (programmatic mode exits after completion, no interactive prompt)
- Timeout multiplier: 2.0 (API calls to Mistral can be slow)
- Bootstrap steps: Requires `MISTRAL_API_KEY` environment variable
- Transient error patterns: `"Rate limits exceeded"`, `"overloaded"`, `"429"`, `"Too Many Requests"`, `"BackendError"`, `"timeout"`
