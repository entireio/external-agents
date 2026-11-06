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

Session filename portability: Windows reserved device basenames (case-insensitive, including names followed by extensions and COM/LPT superscript-digit forms) use the hash mapping on every OS. This keeps resolver and transcript paths consistent when sessions move between platforms.

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
| `calculate-tokens` | Native chat-log usage | Sums the `AssistantUsage` sidecar records after `--offset`; see Token Usage |

## Sidecar Record Shape

Every sidecar record carries Qwen's own hook fields plus an Entire-facing
projection, because Entire compacts an external agent's transcript with the
generic JSONL reader in `cmd/entire/cli/transcript/compact/compact.go`, which
keeps a line only when **both** of these hold. An adapter's own
`compact-transcript` method does not change this: `checkpoint/persistent.go`
always runs the generic compactor over the raw transcript.

| half | what cli reads | native record |
|---|---|---|
| kind | `normalizeKind` (compact.go:197) reads a top-level `type`, falling back to `role` | had neither — the discriminator is `event`, so every line was dropped |
| content | `parseMessage` (compact.go:617) reads content from a top-level `message` **object** | `message` was a **string** (Qwen's notification text), so nothing was found |

The projection is derived data — `readSidecarRecords` ignores `type` and
`message` — so a sidecar written before it existed still reads.

Records carry `v: 2` since the adapter started copying token usage; `v: 1`
records come from an older adapter. The two differ only in that the token
usage copy starts at the first `v: 2` record (see Token Usage).

| event | projected as | content |
|---|---|---|
| `UserPromptSubmit` | `user` | text block with the prompt |
| `PreToolUse` | `assistant` | `tool_use` (`type`/`id`/`name`/`input`, the only fields `stripAssistantContent` keeps) |
| `PostToolUse` / `PostToolUseFailure` | `user` | `tool_result` with snake_case `tool_use_id`, so cli inlines the output into the preceding `tool_use` |
| `Stop` / `StopFailure` | `assistant` | text block with the final message or error details |
| `AssistantUsage` (adapter-written, see Token Usage) | not projected | dropped from the compact transcript |
| everything else | not projected | dropped from the compact transcript |

`message` is stored as raw JSON so a record round-trips whichever shape it
carries. Typed as a string, a projected line would fail to unmarshal and take
the whole sidecar read with it.

## Token Usage

Read from Qwen Code 0.26.9 source (`QwenLM/qwen-code` 11c83aa) and checked
with a live run of the 0.26.9 npm package against a local OpenAI-compatible
mock that streams usage: `ChatRecordingService.recordAssistantTurn` stores the
provider's usage, normalized to Gemini field names (`promptTokenCount`,
`candidatesTokenCount`, `cachedContentTokenCount`, `thoughtsTokenCount`,
`totalTokenCount`), as `usageMetadata` on each assistant record of the native
chat log. The OpenAI-compatible pipeline requests it with
`stream_options.include_usage`; the Anthropic and Responses generators convert
their usage to the same shape.

| Step | Behavior |
|---|---|
| Copy | At `PreToolUse`, `Stop`, `StopFailure`, `SubagentStop` and `SessionEnd`, each native `assistant` record with `usageMetadata` not yet copied becomes an `AssistantUsage` sidecar record holding its usage, `uuid` (`native_uuid`) and the byte offset of its line (`native_offset`) |
| Wait | At `Stop`, while the log ends with a `user` or `tool_result` record (ignoring `system` records) or a line still being written, the copy polls for up to 1 s: Qwen queues the turn's final assistant record before it fires `Stop`, but the write can still be in flight |
| Resume | A copy finds the last `AssistantUsage` record for the log by reading the sidecar backwards from its end, and starts after that record's line while the line still holds the same record. Otherwise it rereads the log (Qwen can rewrite it, e.g. when prompts are deleted) and copies its calls again; the count keeps one copy of each |
| Skip | Lines still being written (no newline yet), records `/branch` copied from a parent session (`forkedFrom`), subagent records (`isSidechain`), and records older than the sidecar's first `v: 2` record: the history of a resumed session whose sidecar was removed from the temp directory, or of a session an older adapter started |
| Count | `calculate-tokens --offset <line>` sums `AssistantUsage` records from that sidecar line, counting each `native_uuid` once. Input is `promptTokenCount` minus `cachedContentTokenCount`, cache reads are `cachedContentTokenCount`, and output counts reasoning once, decided by `totalTokenCount` as in Qwen's own `outputTokenCount` |

Qwen writes its log through a queue that no hook waits for, so an API call can
land after the hook that follows it (seen once in six live runs, at
`PreToolUse`; that call was copied at `Stop`). `Stop` waits for the turn's
final call as above; a call that lands later still is copied at the next hook
and counted with that turn or checkpoint. Calls outside the session's chat log
(subagents, the auto-memory extractor and other background helpers) are not
counted, and Qwen keeps no separate cache-write count.

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
| `PreToolUse` | `pre-tool-use` | sidecar only; projected as an assistant `tool_use` |
| `PostToolUse` | `post-tool-use` | sidecar only; projected as a user `tool_result` |
| `PostToolUseFailure` | `post-tool-use-failure` | sidecar only; projected as a failed user `tool_result` |
| `Notification` | `notification` | sidecar metadata only |
| `PermissionRequest` | `permission-request` | sidecar metadata only |
| `SubagentStart` | `subagent-start` | sidecar metadata only |
| `SubagentStop` | `subagent-stop` | sidecar metadata only |

## Compatibility Notes

The adapter accepts Qwen Code's documented and observed payload aliases, including `prompt`/`user_prompt`, `tool_input`/`inputs`/`input`, and `tool_response`/`response`/`tool_result`. It also preserves unknown hook configuration fields so future Qwen Code settings are not destroyed by `entire enable` or uninstall.

Qwen Code must execute an actual tool for `PostToolUse` to fire. Local model backends that only print XML-style tool tags as assistant text do not create file changes or Qwen tool events, so there is nothing for Entire to checkpoint until the model/backend returns structured tool calls that Qwen Code executes.

## Test Notes

Default CI does not require Qwen credentials. Real lifecycle testing is gated with `QWEN_E2E=1 E2E_AGENT=qwen`.
