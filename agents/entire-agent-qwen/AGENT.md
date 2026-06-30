# Qwen Code — External Agent Research

## Verdict: Compatible

Qwen Code exposes command hooks in `.qwen/settings.json` and stores project-scoped JSONL sessions under the Qwen runtime directory. The first integration uses Qwen hooks as the lifecycle source of truth and writes an Entire-owned sidecar transcript under a repo-scoped OS temp directory so Entire can read stable session data even if Qwen's native transcript evolves.

## Static Checks

| Check | Result | Notes |
|-------|--------|-------|
| Binary present | Optional | `qwen` on `PATH`; protocol commands still run without it |
| Help available | PASS | Qwen Code documents `qwen -p`, `--continue`, and `--resume` |
| Hooks available | PASS | `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `Stop`, `StopFailure`, `SessionEnd`, `PreCompact`, `PostCompact`, `PostToolUse`, `PostToolUseFailure`, `Notification`, `PermissionRequest`, `SubagentStart`, `SubagentStop` |
| Config directory | PASS | Workspace `.qwen/settings.json` |
| Session storage | PASS | Native JSONL under Qwen runtime; Entire sidecar in `/tmp/entire-qwen/<repo-hash>/` plus `.entire/tmp/<session_id>.json` marker |

## Protocol Mapping

| Protocol | Qwen Concept | Implementation |
|----------|--------------|----------------|
| `info` | Static metadata | Name `qwen`, type `Qwen Code`, preview |
| `detect` | CLI availability | Checks `qwen` on `PATH` |
| `get-session-id` | Hook `session_id` | Returns input session ID or stub |
| `get-session-dir` | Entire sidecar dir | Repo-scoped OS temp directory outside `.entire/tmp` |
| `resolve-session-file` | Entire sidecar file | `<session_id>.jsonl` |
| `read-session` | Sidecar JSONL | Returns native bytes and computed modified files |
| `write-session` | Sidecar JSONL | Writes `native_data` to `session_ref` |
| `read-transcript` | Sidecar JSONL | Reads bytes directly |
| `chunk-transcript` | Raw bytes | Fixed-size byte chunks |
| `reassemble-transcript` | Raw bytes | Concatenates chunks |
| `format-resume-command` | Qwen resume | `qwen --resume <session_id>` |
| `parse-hook` | Qwen hooks | Maps lifecycle hooks to Entire event objects |
| `install-hooks` | `.qwen/settings.json` | Read-modify-write command hooks while preserving user and future Qwen hook fields |
| `are-hooks-installed` | `.qwen/settings.json` | Requires all Entire Qwen hooks |
| `uninstall-hooks` | `.qwen/settings.json` | Removes only Entire hook entries |
| `extract-modified-files` | Tool inputs | Reads path fields and simple shell write patterns |
| `extract-prompts` | `UserPromptSubmit` | Reads sidecar prompt records |
| `extract-summary` | `Stop`/failure | Reads final assistant/error message |
| `compact-transcript` | Sidecar JSONL | Emits base64 Entire Transcript Format JSONL |

## Lifecycle Mapping

| Qwen Hook | Entire Hook Verb | Entire Event |
|-----------|------------------|--------------|
| `SessionStart` | `session-start` | `SessionStart` |
| `UserPromptSubmit` | `user-prompt-submit` | `TurnStart` |
| `Stop` | `stop` | `TurnEnd` |
| `StopFailure` | `stop-failure` | `TurnEnd` with error metadata |
| `SessionEnd` | `session-end` | `SessionEnd` |
| `PreCompact` | `pre-compact` | `Compaction` |
| `PostCompact` | `post-compact` | `Compaction` |
| `PreToolUse` | `pre-tool-use` | sidecar metadata only |
| `PostToolUse` | `post-tool-use` | sidecar metadata only |
| `PostToolUseFailure` | `post-tool-use-failure` | sidecar metadata only |
| `Notification` | `notification` | sidecar metadata only |
| `PermissionRequest` | `permission-request` | sidecar metadata only |
| `SubagentStart` | `subagent-start` | sidecar metadata only |
| `SubagentStop` | `subagent-stop` | sidecar metadata only |

## Compatibility Notes

The adapter accepts Qwen Code's documented and observed payload aliases, including `prompt`/`user_prompt`, `tool_input`/`inputs`/`input`, and `tool_response`/`response`/`tool_result`. It also preserves unknown hook configuration fields so future Qwen Code settings are not destroyed by `entire enable` or uninstall.

Qwen Code must execute an actual tool for `PostToolUse` to fire. Local model backends that only print XML-style tool tags as assistant text do not create file changes or Qwen tool events, so there is nothing for Entire to checkpoint until the model/backend returns structured tool calls that Qwen Code executes.

## Test Notes

Default CI does not require Qwen credentials. Real lifecycle testing is gated with `QWEN_E2E=1 E2E_AGENT=qwen`.
