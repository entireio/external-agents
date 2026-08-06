# Hermes Agent — External Agent Research

## Verdict: COMPATIBLE (preview)

Hermes Agent v0.20.0 has a public Python plugin system with synchronous lifecycle hooks in both CLI and gateway sessions. The integration can avoid Hermes's private SQLite state entirely by installing a standalone observer plugin into an explicitly selected `HERMES_HOME`. That observer can write a minimal, repository-keyed JSONL transcript and forward normalized lifecycle events to Entire.

The live model lifecycle remains unverified because the safe verification profile intentionally contains no copied credentials. Static CLI probing, installed-source inspection, official documentation, and synthetic hook fixtures are sufficient to build and test the preview without touching the real Hermes profile.

## Static Checks

| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `/home/ninalyx/.hermes/hermes-agent/venv/bin/hermes` |
| Help available | PASS | `hermes --help`; one-shot mode is `hermes -z <prompt>` |
| Version info | PASS | Hermes Agent v0.20.0 (2026.8.3) |
| Hook keywords | PASS | `hooks`, `plugins`, `pre_tool_call`, `post_tool_call`, `pre_llm_call`, `post_llm_call`, and session hooks |
| Session keywords | PASS | `--resume`, `--continue`, and `sessions` are exposed by the CLI |
| Config directory | PASS | Explicit `$HERMES_HOME`; the real `~/.hermes` profile was not inspected |
| Documentation | PASS | [Event Hooks](https://hermes-agent.nousresearch.com/docs/user-guide/features/hooks), [Plugins](https://hermes-agent.nousresearch.com/docs/user-guide/features/plugins), and [Build a Plugin](https://hermes-agent.nousresearch.com/docs/developer-guide/plugins) |

## Binary

- Name: `hermes`
- Version: `Hermes Agent v0.20.0 (2026.8.3)`
- Installed source: `/home/ninalyx/.hermes/hermes-agent`
- External adapter binary: `entire-agent-hermes`
- Non-interactive execution: `hermes -z '<prompt>'`
- Interactive execution: `hermes --cli`
- Resume: `hermes --resume <session_id>`

## Hook Mechanism

- Plugin directory: `$HERMES_HOME/plugins/entire-observer/`
- Manifest: `$HERMES_HOME/plugins/entire-observer/plugin.yaml`
- Entry point: `$HERMES_HOME/plugins/entire-observer/__init__.py` with `register(ctx)`
- Activation: append only `entire-observer` to `plugins.enabled` in `$HERMES_HOME/config.yaml`; remove only that name from `plugins.disabled`
- Repository registration: `$HERMES_HOME/plugins/entire-observer/repositories.json`, containing canonical repo paths and the Entire binary used for each repo
- Profile requirement: `install-hooks`, `uninstall-hooks`, and `are-hooks-installed` require an explicit `HERMES_HOME`; they never fall back to the real default profile
- Failure behavior: Hermes isolates plugin callback exceptions. The observer additionally catches filesystem and Entire subprocess failures, returns no directive, and therefore fails open.

### Native hook mapping

| Native Hook Name | When It Fires | Protocol Event Type |
|------------------|---------------|---------------------|
| `on_session_start` | First turn of a newly created Hermes session | 1 = SessionStart |
| `pre_llm_call` | Once before the tool-calling loop for the current user turn | 2 = TurnStart |
| `pre_tool_call` | Immediately before a tool executes | Observer-only; resolves canonical workdir/cwd and path-like args, then activates Entire and projects buffered SessionStart + TurnStart before mutation-capable tools |
| `post_tool_call` | After a tool returns | Observer-only; records tool name/status and repo-relative modified paths only in the repositories resolved from that tool's args, never raw args/results |
| `post_llm_call` | After the turn's final response is available | Observer-only; records and forwards the sanitized current response to every repository touched in that turn |
| `on_session_end` | At the end of each `run_conversation` call in v0.20.0 | 3 = TurnEnd for every repository touched in the turn |
| `on_session_finalize` | CLI/gateway teardown of the active session | 5 = SessionEnd for all repository projections of the Hermes session |

The v0.20.0 installed source is authoritative for `on_session_end`: despite its name, it fires per completed `run_conversation` call, so the adapter maps it to TurnEnd and reserves SessionEnd for `on_session_finalize`.

### Hook input

Plugin callbacks receive keyword arguments in-process. Relevant source-verified values are:

- `on_session_start`: `session_id`, `model`, `platform`
- `pre_llm_call`: `session_id`, `user_message`, `conversation_history`, `model`, plus task/turn/platform/parent/sender identifiers
- `pre_tool_call`: `tool_name`, `args`, and task/session/tool/turn/request identifiers
- `post_tool_call`: the pre-tool fields plus `result`, duration, and status/error fields
- `post_llm_call`: `session_id`, `user_message`, `assistant_response`, `conversation_history`, `model`, and platform
- `on_session_end`: `session_id`, completion status, model, platform, and task/turn identifiers
- `on_session_finalize`: at minimum `session_id`

The observer deliberately ignores `conversation_history`, `result`, `platform`, sender/task/turn/request/tool-call IDs, parent IDs, and every unknown keyword. Raw `args` are inspected transiently only for canonical path containment and are never copied into in-memory projection state, transcripts, or forwarded payloads. It forwards only a minimal sanitized payload to `entire hooks hermes <hook>` over stdin.

## Session Management

- Session ID source: the public `session_id` hook argument
- Session directory: `$HERMES_HOME/entire/transcripts/<sha256(canonical-repo-path)-prefix>/`
- Session file: `<sha256(session-id)>.jsonl`
- Native Hermes storage: `state.db` exists according to official documentation and installed source, but was not read and is not required by this integration
- Resume command: `hermes --resume <session_id>`

## Transcript

- Location: `$HERMES_HOME/entire/transcripts/<repo-hash>/<session-id>.jsonl`
- Format: one sanitized JSON object per line, versioned with `v: 1`
- User prompt: `type: "user"`, `content`
- Assistant response/summary source: `type: "assistant"`, `content`
- Modified files: `type: "tool"`, `modified_files` containing only normalized repo-relative paths
- Tool data retained: tool name, coarse status, and modified paths
- Token usage: not retained
- Raw tool input/result: never retained
- Example: `{"v":1,"type":"user","timestamp":"2026-08-06T12:00:00Z","content":"Create hello.txt"}`

The observer applies bounded text sanitization before persistence and the Go adapter sanitizes again while reading/compacting. It does not register API-request, memory, system-prompt, middleware, or gateway-dispatch observers.

## Data Storage Verification

- Native session files contain actual assistant content: NOT INSPECTED — prohibited for this task
- Primary native storage: `$HERMES_HOME/state.db` according to official documentation; the integration does not open it
- Secondary Entire storage: observer-owned sanitized JSONL under `$HERMES_HOME/entire/transcripts/`
- Secondary storage format: repository-scoped JSONL written from public plugin hooks
- Cross-reference key: public Hermes `session_id`; repositories are resolved by strict canonical containment of explicit `workdir`/`cwd` and path-like tool fields, using the longest registered match; current process CWD is only the no-argument fallback
- Hook data flow verified: SOURCE-VERIFIED; synthetic callback verification is performed from temp homes/fixtures during implementation
- Verification method: official docs, installed v0.20.0 source, `hermes --help`, temp-home plugin discovery, and source/unit fixtures

## Protocol Mapping

| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|----------------|----------------------|-------------|
| `info` | — | Static preview metadata and capabilities | Required |
| `detect` | Hermes CLI | `exec.LookPath("hermes")` | Required |
| `get-session-id` | Hook `session_id` | Echo normalized HookInput session ID | Required |
| `get-session-dir` | Observer transcript root | Explicit `HERMES_HOME` + canonical repo hash | Required |
| `resolve-session-file` | Observer JSONL | Safe session ID under supplied session dir | Required |
| `read-session` | Observer JSONL | Read safe native bytes and derive file lists | Required |
| `write-session` | Observer JSONL | Atomic mode-0600 write to supplied session ref | Required |
| `read-transcript` | Observer JSONL | Read only the requested file | Required |
| `chunk-transcript` | Raw bytes | Byte chunking with positive-size validation | Required |
| `reassemble-transcript` | Raw bytes | Concatenate decoded chunks | Required |
| `compact-transcript` | Sanitized JSONL | Emit compact user/assistant/tool metadata without raw results | compact_transcript |
| `format-resume-command` | Hermes resume | `hermes --resume <quoted-id>` | Required |
| `parse-hook` | Plugin lifecycle payload | Normalize four lifecycle hooks; observer-only hooks return null | hooks |
| `install-hooks` | User plugin | Embedded plugin + registry + config merge under explicit `HERMES_HOME` | hooks |
| `uninstall-hooks` | User plugin | Remove current repo registration; remove plugin only after last repo | hooks |
| `are-hooks-installed` | User plugin | Validate embedded markers, enablement, and current repo registration | hooks |
| `get-transcript-position` | JSONL byte size | Return file byte size, zero when missing | transcript_analyzer |
| `extract-modified-files` | Tool metadata | Parse complete lines after byte offset | transcript_analyzer |
| `extract-prompts` | User entries | Return sanitized user content after byte offset | transcript_analyzer |
| `extract-summary` | Assistant entries | Last non-empty sanitized assistant response | transcript_analyzer |

## Selected Capabilities

| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | Hermes provides public CLI/gateway plugin hooks |
| transcript_analyzer | true | Observer JSONL has stable prompt, response, and modified-file entries |
| transcript_preparer | false | Observer writes the final parseable JSONL directly |
| token_calculator | false | Token fields are intentionally not captured |
| compact_transcript | true | Safe compact JSONL is derived from the already-sanitized observer transcript |
| text_generator | false | Avoids credential/model invocation inside the protocol adapter |
| hook_response_writer | false | Observer hooks are non-intervening and fail open |
| subagent_aware_extractor | false | Child summaries and child transcripts are intentionally not captured |

## Safety Invariants

- Never read Hermes `state.db`.
- Never install into an implicit/default Hermes home.
- Never copy or inspect Hermes credentials, session databases, memory, or profile files beyond the explicit temp/config paths under test.
- Never persist system/developer prompts, memory, full conversation history, platform/sender/task/turn/request/tool-call IDs, environment dumps, secrets, raw tool inputs, or raw tool results.
- Apply Hermes' `redact_sensitive_text(..., force=True, redact_url_credentials=True)` when available, then apply the adapter's independent fallback patterns and allowlists before persistence.
- A running gateway must restart after plugin installation, update, or final removal because Hermes' process-wide plugin manager does not dynamically rescan.
- Start the Entire session and turn synchronously from `pre_tool_call` before a repository-scoped tool executes; any lifecycle forwarding failure is swallowed so Hermes continues.
- Resolve only strict canonical containment in registered repositories, prefer the longest match for nested repositories, reject traversal/sensitive/symlink-escape targets, and use current CWD only when tool args provide no path/workdir evidence.
- Buffer only the sanitized current prompt/model in memory until the session first touches a registered repository; maintain projections per `(session, repository)` and keep tool/file evidence scoped to the repository resolved for that tool.
- Restrict transcript reads/writes to the explicit observer transcript root, with read-only access to adapter-owned `testdata` fixtures for shared protocol compliance.
- Preserve unrelated plugin directories and unrelated `plugins.enabled`/`plugins.disabled` entries.
- Support multiple canonical repo registrations in one Hermes profile.

## Gaps & Limitations

- Live model lifecycle is not verified in the isolated profile because no credentials were copied into it.
- `terminal` commands are not parsed. Repository selection uses an explicit `workdir`/`cwd` when supplied and otherwise the registered current-CWD fallback; Git status supplies repository-relative changed paths after the tool returns.
- Modified-file extraction can include files already dirty in the repository; Entire's own checkpoint baseline remains authoritative for content changes.
- Hermes may not deliver `on_session_finalize` after a hard kill. TurnEnd still fires from `on_session_end` on normal turn completion.
- Secret redaction is defense in depth, not a substitute for avoiding sensitive hook fields; the plugin's primary protection is strict field allowlisting.

## Captured Payloads

- Verification script: `agents/entire-agent-hermes/scripts/verify-hermes.sh`
- Capture directory: the script's temp `$HERMES_HOME/entire/transcripts/`
- Verification status: STATIC VERIFIED / LIVE UNVERIFIED
- Notable source finding: in Hermes v0.20.0, `on_session_end` is a per-turn hook; `on_session_finalize` is the teardown hook

## E2E Test Prerequisites

- Entire CLI: `entire` from `PATH` or `E2E_ENTIRE_BIN`; probed version 0.9.0
- Hermes CLI: `hermes` from `PATH`; probed version 0.20.0
- Explicit isolated profile: `HERMES_HOME=$(mktemp -d)`; never the real profile
- External binary: `entire-agent-hermes` on `PATH`
- Non-interactive prompt: `hermes -z '<prompt>' --yolo --ignore-rules`
- Interactive mode: `hermes --cli --yolo --ignore-rules`
- Expected prompt pattern: `❯`
- Timeout multiplier: 2.0
- Bootstrap: an isolated profile must be configured through environment-only test credentials or a disposable config; no real profile files may be copied
- Transient errors: `overloaded`, `rate limit`, `429`, `500`, `503`, `529`, `ECONNRESET`, `ETIMEDOUT`, `timeout`
