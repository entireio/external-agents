# Kiro External Agent Design

## Goal

Build a single external agent binary, `entire-agent-kiro`, that preserves the behavior of the existing built-in Kiro integration while moving it behind the Entire external-agent protocol.

## Decision

Use one logical `kiro` agent, not separate `kiro-cli` and `kiro-ide` agents.

`entire enable kiro` should remain the only user-facing entry point. The external agent should install and support both Kiro CLI hooks and Kiro IDE hooks in one operation.

## Why One Agent

The existing Kiro integration in `/Users/alisha/Projects/wt/kiro-oneshot` already models CLI and IDE as two native surfaces behind one agent:

- CLI hooks live in `.kiro/agents/entire.json`
- IDE hooks live in `.kiro/hooks/*.kiro.hook`
- CLI transcripts come from Kiro's SQLite database
- IDE transcripts come from Kiro workspace session JSON files
- both are normalized into `.entire/tmp/<session>.json`

Splitting the external implementation into two binaries would expose internal transport differences to users without adding useful product value.

## Implementation Language

Use Go.

This matches the current built-in adapter, minimizes porting risk, and fits the rest of the Entire CLI ecosystem.

## Binary Shape

- Binary name: `entire-agent-kiro`
- Registry name exposed to Entire: `kiro`
- Project directory: `agents/entire-agent-kiro`

## Capabilities

The external agent should declare:

- `hooks: true`
- `transcript_analyzer: true`
- `transcript_preparer: false`
- `token_calculator: false`
- `text_generator: false`
- `hook_response_writer: false`
- `subagent_aware_extractor: false`

## Protocol Mapping

### Required subcommands

- `info`
  Returns static metadata for the Kiro agent and the declared capabilities above.

- `detect`
  Returns present when the repo appears Kiro-enabled, primarily by detecting `.kiro` in the worktree. The external agent may also use binary presence as secondary evidence.

- `get-session-id`
  Returns the session ID from normalized hook input data. During active hook flow, this should align with the stable generated session ID cached by the hook parser.

- `get-session-dir --repo-path <path>`
  Returns `<repo-path>/.entire/tmp`.

- `resolve-session-file --session-dir <dir> --session-id <id>`
  Returns `<dir>/<id>.json`.

- `read-session`
  Reads the cached normalized transcript file and returns an `AgentSession`.

- `write-session`
  Writes the normalized cached transcript file for restore and rewind flows.

- `read-transcript --session-ref <path>`
  Returns raw cached transcript bytes.

- `chunk-transcript --max-size <n>`
  Uses the same approach as the built-in Kiro adapter: return one chunk when small, otherwise split safely for transport.

- `reassemble-transcript`
  Reassembles chunked transcript bytes.

- `format-resume-command --session-id <id>`
  Returns `kiro-cli chat --resume`.

### Hook-capability subcommands

- `parse-hook --hook agent-spawn`
  Creates a stable Entire session ID and returns `SessionStart`.

- `parse-hook --hook user-prompt-submit`
  Returns `TurnStart`, reading prompt text from stdin JSON first and IDE environment variables second.

- `parse-hook --hook pre-tool-use`
  Returns `null`.

- `parse-hook --hook post-tool-use`
  Returns `null`.

- `parse-hook --hook stop`
  Returns `TurnEnd` and resolves `session_ref` by caching transcript data into `.entire/tmp/<session>.json`.

- `install-hooks`
  Installs both CLI and IDE hook surfaces and configures trusted commands.

- `uninstall-hooks`
  Removes both CLI and IDE hook surfaces and removes Entire-owned trusted command entries.

- `are-hooks-installed`
  Returns true when either CLI or IDE Entire hooks are installed.

### Transcript-analyzer subcommands

- `get-transcript-position`
  Reports logical position within the cached transcript.

- `extract-modified-files`
  Extracts file modifications from normalized transcript history.

- `extract-prompts`
  Extracts prompt text from transcript history.

- `extract-summary`
  Returns the last assistant response summary.

## Runtime Architecture

`entire-agent-kiro` should have one public agent implementation with two internal native backends.

### CLI backend

- installs `.kiro/agents/entire.json`
- parses hook stdin JSON payloads
- uses a generated cached session ID during active hook processing
- queries Kiro's SQLite database for transcript data
- caches the transcript to `.entire/tmp/<session>.json`

### IDE backend

- installs `.kiro/hooks/*.kiro.hook`
- reads prompt and context from environment fallbacks when stdin is empty
- finds the correct Kiro IDE workspace session directory from the repository path
- reads the IDE transcript JSON and caches it to `.entire/tmp/<session>.json`

### Shared normalization layer

Both backends should normalize into the same external-agent protocol behavior:

- stable Entire session ID
- same session directory and session file contract
- same transcript cache location
- same transcript analyzer entrypoints
- same user-facing `kiro` agent identity

## Hook Installation Behavior

`install-hooks` should perform all of the following in one operation:

- write `.kiro/agents/entire.json` for Kiro CLI hooks
- write `.kiro/hooks/*.kiro.hook` for Kiro IDE hooks
- update `.vscode/settings.json`
- ensure Entire trusted commands are present without deleting unrelated user settings

`uninstall-hooks` should reverse only Entire-owned configuration and preserve unrelated user configuration.

## Session and Transcript Flow

1. `agent-spawn` generates and caches a stable Entire session ID.
2. `user-prompt-submit` emits `TurnStart` using that stable ID.
3. `stop` resolves the stable ID and attempts transcript capture in this order:
   - Kiro CLI SQLite transcript
   - Kiro IDE workspace session transcript
   - placeholder `{}` transcript
4. The resulting transcript is cached under `.entire/tmp/<session>.json`.
5. Transcript analysis operates only on the cached file, not directly on native storage.

This preserves the behavior already present in the built-in adapter and keeps the external-agent implementation stateless across invocations.

## Verification Strategy

The external agent should be considered complete only when all of the following are true:

- the binary responds correctly to `info`
- hook install and uninstall behavior works for both CLI and IDE surfaces
- `parse-hook` produces the expected lifecycle events
- transcript cache resolution works for CLI and IDE sources
- transcript analyzer commands extract prompts, summary, and modified files correctly
- the Entire external-agent conformance suite passes for the declared capabilities

## Non-Goals

- splitting Kiro into separate CLI and IDE user-facing agents
- adding token usage support beyond what the current built-in adapter provides
- adding text generation support
- inventing new Kiro behaviors not already represented in the worktree implementation
