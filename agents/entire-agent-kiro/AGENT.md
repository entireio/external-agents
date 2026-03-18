# Kiro - External Agent Research

## Verdict: COMPATIBLE

Kiro has enough hook, session, and transcript surface area to fit the Entire external-agent protocol with one logical `kiro` agent. The built-in adapter in the Kiro worktree already treats CLI and IDE as two native entrypoints that normalize into the same Entire lifecycle.

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | DOCS/CODE | Worktree code and docs refer to `kiro-cli-chat` as the standalone CLI used for headless auth and prompt runs |
| Help available | DOCS/CODE | Worktree code and docs show CLI support for `chat`, `whoami`, `login`, and `--no-interactive` usage |
| Version info | DOCS/CODE | Version output was not live-probed here; protocol mapping only requires the binary be discoverable through CLI tooling |
| Hook keywords | PASS | CLI hooks, IDE hooks, prompt submit, tool use, stop |
| Session keywords | PASS | session ID, session ref, resume, transcript, workspace sessions |
| Config directory | PASS | `.kiro/`, `.vscode/`, and Kiro IDE workspace storage under the home directory |
| Documentation | PASS | `/Users/alisha/Projects/wt/kiro-oneshot/docs/architecture/external-agent-protocol.md` and Kiro adapter code |

## Binary
- Name: `kiro-cli-chat` for headless CLI usage; `kiro-cli` is the desktop wrapper that forces browser OAuth
- Version: not required for protocol mapping
- Install: use the Kiro CLI/desktop installation that provides `kiro-cli-chat`; the adapter expects the binary to be on `PATH`

## Hook Mechanism
- CLI hook config file: `.kiro/agents/entire.json`
- CLI hook config format: JSON with `name`, `tools`, and nested `hooks`
- CLI hook names and protocol mapping:
  | Native Hook Name | When It Fires | Protocol Event Type |
  |-----------------|---------------|---------------------|
  | `agentSpawn` | CLI session starts | `SessionStart` |
  | `userPromptSubmit` | User submits a prompt | `TurnStart` |
  | `preToolUse` | Before a tool call | no lifecycle event |
  | `postToolUse` | After a tool call | no lifecycle event |
  | `stop` | CLI turn ends | `TurnEnd` |
- CLI hook payload: JSON on stdin with `hook_event_name`, `cwd`, `prompt`, `tool_name`, `tool_input`, and `tool_response`
- IDE hook config files: `.kiro/hooks/*.kiro.hook`
- IDE hook config format: JSON with `enabled`, `name`, `description`, `version`, `when`, and `then`
- IDE trigger types installed by Entire: `promptSubmit`, `agentStop`, `preToolUse`, `postToolUse`
- IDE hook input: the adapter tolerates empty stdin and falls back to environment variables such as `USER_PROMPT`

## Session Management
- Session ID source: a stable Entire session ID is normally generated at `agentSpawn` and cached in `.entire/tmp/kiro-active-session`; if that cache is missing, including IDE flow where there is no IDE `agentSpawn` hook, the adapter falls back to generating and caching one during `userPromptSubmit`
- Session directory: `.entire/tmp/` under the repo root
- Session file format: cached JSON transcript, one file per session ID
- Session file path: `.entire/tmp/<session-id>.json`
- Native CLI session lookup: SQLite database at `~/Library/Application Support/kiro-cli/data.sqlite3` on macOS or `~/.local/share/kiro-cli/data.sqlite3` on Linux
- Native CLI lookup key: the current working directory is queried against `conversations_v2`
- Native IDE session lookup: `~/Library/Application Support/Kiro/User/globalStorage/kiro.kiroagent/workspace-sessions/<base64(cwd)>/sessions.json` on macOS or the matching `~/.config/...` path on Linux
- IDE transcript source: the most recent entry in `sessions.json`, then `<sessionId>.json` in the same directory

## Transcript
- CLI transcript format: JSON object with `conversation_id` and `history`
- CLI history entries: paired user and assistant messages
- User prompt shape: `history[].user.content` containing a tagged union such as `{"Prompt":{"prompt":"..."}}`
- Assistant tool-call shape: `history[].assistant` containing `{"ToolUse": {...}}`
- Assistant response shape: `history[].assistant` containing `{"Response": {...}}`
- IDE transcript format: JSON object with `history`, where each entry contains a `message` object
- IDE message shape: Anthropic-style `role` plus `content`
- CLI transcript capture: fetched from SQLite at turn end and cached to `.entire/tmp/<session-id>.json`
- IDE transcript capture: copied from the workspace session file into `.entire/tmp/<session-id>.json`

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | adapter metadata | static JSON describing `kiro` and declared capabilities | Required |
| `detect` | `.kiro` repository config | presence is inferred from repo-local Kiro state | Required |
| `get-session-id` | cached stable session ID | use the cached Entire session ID, not Kiro's transient DB state | Required |
| `get-session-dir` | Entire cache directory | return `<repo>/.entire/tmp` | Required |
| `resolve-session-file` | normalized cache path | return `<repo>/.entire/tmp/<id>.json` | Required |
| `read-session` | cached transcript file | read the normalized JSON cache and synthesize `AgentSession` | Required |
| `write-session` | normalized transcript cache | write the cached transcript back for rewind/resume | Required |
| `read-transcript` | cached transcript bytes | return raw bytes from `.entire/tmp/<id>.json` | Required |
| `chunk-transcript` | raw transcript bytes | chunk the cached JSON for transport; no native Kiro format change needed | Required |
| `reassemble-transcript` | chunk reassembly | reverse `chunk-transcript` | Required |
| `format-resume-command` | Kiro resume command | return `kiro-cli chat --resume` | Required |
| `parse-hook` | CLI/IDE hook payloads | map CLI stdin JSON or IDE env-based input to lifecycle events | If hooks capable |
| `install-hooks` | `.kiro/agents/entire.json`, `.kiro/hooks/*.kiro.hook`, `.vscode/settings.json` | install both CLI and IDE support in one operation | If hooks capable |
| `uninstall-hooks` | reverse hook installation | remove Entire-owned CLI and IDE hook files plus trusted commands | If hooks capable |
| `are-hooks-installed` | config presence check | true when either CLI hooks or IDE hooks are present | If hooks capable |
| `get-transcript-position` | transcript length | CLI uses history entry count; IDE uses history count from cached JSON | If transcript analyzer |
| `extract-modified-files` | transcript tool-use history | parse tool calls and file edits from cached transcript | If transcript analyzer |
| `extract-prompts` | user prompt history | return prompt text from transcript history | If transcript analyzer |
| `extract-summary` | last assistant response | return the final assistant response text | If transcript analyzer |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | Kiro has both CLI hook config and IDE hook files |
| transcript_analyzer | true | Kiro transcripts are JSON and can be parsed for prompts and file changes |
| transcript_preparer | false | the adapter only needs to read cached transcripts, not pre-process live files |
| token_calculator | false | the worktree adapter does not expose a token-counting path |
| text_generator | false | Kiro's CLI is used for agent execution, not metadata generation |
| hook_response_writer | false | the adapter does not depend on structured hook responses |
| subagent_aware_extractor | false | the worktree code does not model a subagent transcript tree |

## Gaps & Limitations
- Kiro CLI and Kiro IDE do not share the same native storage format, so Entire must normalize them into `.entire/tmp/<session-id>.json`
- `kiro-cli` is not the right binary for headless support; the adapter uses `kiro-cli-chat` because it supports device-flow or SIGV4-based non-interactive use
- The CLI transcript is only reliable at turn end because Kiro's SQLite conversation record is populated late
- IDE prompt input may arrive through environment variables instead of stdin, so hook parsing must tolerate empty stdin
- The native session ID from Kiro is not stable early in the turn, so Entire needs its own cached session ID file for hook coherence
