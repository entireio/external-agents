# Qwen Code — External Agent Research

## Verdict: Compatible

Qwen Code exposes command hooks in `.qwen/settings.json` and stores project-scoped JSONL sessions under the Qwen runtime directory. The first integration uses Qwen hooks as the lifecycle source of truth and writes an Entire-owned sidecar transcript under `.entire/tmp/qwen/` so Entire can read stable session data even if Qwen's native transcript evolves.

## Static Checks

| Check | Result | Notes |
|-------|--------|-------|
| Binary present | Optional | `qwen` on `PATH`; protocol commands still run without it |
| Help available | PASS | Qwen Code documents `qwen -p`, `--continue`, and `--resume` |
| Hooks available | PASS | `SessionStart`, `UserPromptSubmit`, `Stop`, `StopFailure`, `SessionEnd`, `PreCompact`, `PostToolUse`, `PostToolUseFailure` |
| Config directory | PASS | Workspace `.qwen/settings.json` |
| Session storage | PASS | Native JSONL under Qwen runtime; Entire sidecar in `.entire/tmp/qwen/` |

## Protocol Mapping

| Protocol | Qwen Concept | Implementation |
|----------|--------------|----------------|
| `info` | Static metadata | Name `qwen`, type `Qwen Code`, preview |
| `detect` | CLI availability | Checks `qwen` on `PATH` |
| `get-session-id` | Hook `session_id` | Returns input session ID or stub |
| `get-session-dir` | Entire sidecar dir | `.entire/tmp/qwen` |
| `resolve-session-file` | Entire sidecar file | `<session_id>.jsonl` |
| `read-session` | Sidecar JSONL | Returns native bytes and computed modified files |
| `write-session` | Sidecar JSONL | Writes `native_data` to `session_ref` |
| `read-transcript` | Sidecar JSONL | Reads bytes directly |
| `chunk-transcript` | Raw bytes | Fixed-size byte chunks |
| `reassemble-transcript` | Raw bytes | Concatenates chunks |
| `format-resume-command` | Qwen resume | `qwen --resume <session_id>` |
| `parse-hook` | Qwen hooks | Maps lifecycle hooks to Entire event objects |
| `install-hooks` | `.qwen/settings.json` | Read-modify-write command hooks |
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
| `PostToolUse` | `post-tool-use` | sidecar metadata only |
| `PostToolUseFailure` | `post-tool-use-failure` | sidecar metadata only |

## Test Notes

Default CI does not require Qwen credentials. Real lifecycle testing is gated with `QWEN_E2E=1 E2E_AGENT=qwen`.
