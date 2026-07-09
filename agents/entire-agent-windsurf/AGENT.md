# Windsurf - External Agent Research

## Verdict: COMPATIBLE

Windsurf Cascade exposes three lifecycle hooks that map cleanly onto the Entire turn lifecycle. Because Windsurf is an IDE (not a headless CLI), session management is fully driven by hook payloads rather than a native CLI or database.

## Static Checks

| Check | Result | Notes |
|-------|--------|-------|
| Binary present | IDE-only | Windsurf is a desktop IDE; no headless CLI equivalent exists |
| Hook keywords | PASS | `pre_user_prompt`, `post_write_code`, `post_cascade_response` |
| Session keywords | PASS | `trajectory_id` (stable per-conversation ID in all hook payloads) |
| Config directory | PASS | `.windsurf/hooks.json` (workspace-level hook config) |
| Documentation | PASS | https://docs.windsurf.com/windsurf/cascade/cascade-hooks |

## Hook Mechanism

- Config file: `.windsurf/hooks.json`
- Config format: top-level `"hooks"` object with per-hook arrays of entries
- Hook command field: `"command"` on Unix/macOS, `"powershell"` on Windows
- Hook payload: JSON on stdin (or empty for hooks that fire without payload)

| Native Hook Name | When It Fires | Protocol Event Type |
|-----------------|---------------|---------------------|
| `pre_user_prompt` | Before Cascade processes a user message | `TurnStart` (type 2) |
| `post_write_code` | After Cascade writes a file | *(no lifecycle event — file path recorded to transcript)* |
| `post_cascade_response` | After Cascade produces a response | `TurnEnd` (type 3) |

Hook payload fields:
- `trajectory_id` — stable UUID for the current Cascade conversation (used as session ID)
- `timestamp` — ISO 8601 timestamp
- `tool_info.user_prompt` — user prompt text (in `pre_user_prompt`)
- `tool_info.file_path` — file written (in `post_write_code`)
- `tool_info.response` — assistant response text (in `post_cascade_response`)

## Session Management

- Session ID source: `trajectory_id` from the hook payload; falls back to cached session ID file, then generates a UUID
- Session ID cache: `.entire/tmp/windsurf-active-session` — written on `pre_user_prompt`, read when subsequent hooks arrive without `trajectory_id`
- Session directory: `.entire/tmp/` under the repo root
- Session file path: `.entire/tmp/<trajectory_id>.json`
- Session file format: JSONL — one record per line (`{"v":1,"type":"prompt"|"file"|"response",...}`)

## Transcript

- Format: JSONL appended incrementally across hook invocations
- Record types:
  - `{"v":1,"type":"prompt","content":"<user prompt>","ts":"<ISO8601>"}` — written on `pre_user_prompt`
  - `{"v":1,"type":"file","path":"<file path>"}` — written on `post_write_code`
  - `{"v":1,"type":"response","content":"<assistant response>","ts":"<ISO8601>"}` — written on `post_cascade_response`
- Transcript position: line count (used as offset for incremental extraction)
- No external binary or database required — transcript is built entirely from hook events

## Protocol Mapping

| Subcommand | Native Concept | Implementation Notes |
|-----------|---------------|---------------------|
| `info` | adapter metadata | declares `hooks`, `transcript_analyzer`, `compact_transcript` |
| `detect` | `.windsurf` directory | returns present=true when `.windsurf/` exists in repo root |
| `get-session-id` | cached stable session ID | reads `.entire/tmp/windsurf-active-session` |
| `get-session-dir` | Entire cache directory | returns `<repo>/.entire/tmp` |
| `resolve-session-file` | normalized cache path | returns `<dir>/<id>.json` |
| `read-session` | JSONL transcript file | reads `.entire/tmp/<id>.json` |
| `write-session` | JSONL transcript file | writes bytes back to `.entire/tmp/<id>.json` |
| `read-transcript` | raw JSONL bytes | return raw bytes from session file |
| `chunk-transcript` | raw bytes | split into base64 chunks for transport |
| `reassemble-transcript` | chunk reassembly | reverse `chunk-transcript` |
| `format-resume-command` | Windsurf resume | returns `"windsurf"` (user opens the IDE; no CLI resume path) |
| `parse-hook pre_user_prompt` | turn start | emits `EventJSON{Type:2}` with session ID and prompt |
| `parse-hook post_write_code` | file write | returns nil (records file path to transcript only) |
| `parse-hook post_cascade_response` | turn end | emits `EventJSON{Type:3}` with session ref |
| `install-hooks` | `.windsurf/hooks.json` | writes all three lifecycle hooks; idempotent |
| `uninstall-hooks` | reverse install | removes `.windsurf/hooks.json` |
| `are-hooks-installed` | config presence check | true when `.windsurf/hooks.json` exists with Entire entries |
| `get-transcript-position` | JSONL line count | returns number of lines in session file |
| `extract-modified-files` | file records in JSONL | deduplicates `"file"` records from offset |
| `extract-prompts` | prompt records in JSONL | returns `"prompt"` record content from offset |
| `extract-summary` | last response | returns last `"response"` record content |
| `compact-transcript` | JSONL → base64 JSONL | emits `user`/`assistant` lines; skips `file` records |

## Selected Capabilities

| Capability | Declared | Justification |
|-----------|----------|---------------|
| `hooks` | true | Windsurf exposes three workspace-level Cascade lifecycle hooks |
| `transcript_analyzer` | true | JSONL transcript is parsed for prompts, files, and summaries |
| `compact_transcript` | true | JSONL is compacted to base64-encoded user/assistant pairs |
| `transcript_preparer` | false | transcript is built incrementally from hooks, not pre-processed |
| `token_calculator` | false | no token-counting path needed |
| `text_generator` | false | not used |
| `hook_response_writer` | false | not used |
| `subagent_aware_extractor` | false | Windsurf Cascade does not expose a subagent model |

## Lifecycle Tests

Windsurf is an IDE agent with no automatable CLI. Lifecycle E2E tests cannot run in CI without a live Windsurf session. The agent is excluded from the default E2E suite and is opt-in via `E2E_AGENT=windsurf`. Protocol compliance is verified through `external-agents-tests verify ./entire-agent-windsurf`.

## Gaps & Limitations

- `format-resume-command` returns `"windsurf"` — there is no CLI resume path; the user must open the IDE
- `post_write_code` does not emit a lifecycle event (only records the file path to the transcript)
- Hook payload shape may change as Windsurf's hooks API evolves (noted in README with preview warning)
- Truly concurrent Cascade conversations in the same project would share the session ID cache; this is best-effort and matches the constraint that Windsurf's `trajectory_id` is always present in the payload when available
