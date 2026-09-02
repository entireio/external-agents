# Devin CLI External Agent

Integration notes for Devin CLI ("Devin for Terminal", Cognition), implemented
as an Entire external agent (`entire-agent-devin`). All facts below were
verified live against `devin 3000.2.17` on macOS (2026-07-27) unless marked
otherwise. This research was originally done for entireio/cli#1864 and ported
here at the maintainers' request.

## Protocol mapping

| Devin hook (Claude Code format) | `entire hooks devin <verb>` | Event |
|---|---|---|
| `SessionStart` | `session-start` | SessionStart (1) |
| `UserPromptSubmit` | `user-prompt-submit` | TurnStart (2) with prompt |
| `Stop` | `stop` | TurnEnd (3) |
| `SessionEnd` | `session-end` | SessionEnd (5) |

Payloads carry no `transcript_path`, so `session_ref` is derived from the
session ID: `<transcripts-dir>/<session_id>.json`.

Capabilities declared: `hooks`, `transcript_analyzer`, `transcript_preparer`,
`token_calculator`, `compact_transcript`, `uses_terminal`. Not declared:
`text_generator` (Entire's `checkpoint explain --generate` uses the
`claude-code` provider, not Devin itself), `hook_response_writer` (hook-driven
context injection and `systemMessage` display are not wired — see Known
limitations), and `subagent_aware_extractor` (Devin exposes no subagent hooks).

`PostToolUse` live file tracking (used by
the in-tree prototype) is NOT ported: the external protocol's Event object
does not carry `modified_files`, so per-tool ToolUse events cannot deliver
their payload. File detection falls back to Entire's git-status detection at
TurnEnd plus ATIF `tool_calls` extraction at condensation — both verified
working end-to-end.

## Hook surface

Devin CLI supports Claude Code-format hooks loaded from `.devin/hooks.v1.json`
(the hooks object is the entire file — no `{"hooks": ...}` wrapper), from
`.devin/config[.local].json` under a `"hooks"` key, and from
`.claude/settings[.local].json` (gated by `read_config_from.claude`,
default on). Entire installs into `.devin/hooks.v1.json` only.

Events (verified firing in print mode): `SessionStart`, `UserPromptSubmit`,
`PostToolUse`, `Stop`, `SessionEnd`. Also documented: `PreToolUse`,
`PermissionRequest`, `PostCompaction`. There is no `SubagentStop` and no
`PreCompact`. The `matcher` field is a regex over `tool_name`.

Verified payloads (note: **no `transcript_path`, no `cwd`** — unlike Claude Code):

```json
{"hook_event_name":"SessionStart","source":"startup","session_id":"snowy-efraasia"}
{"hook_event_name":"UserPromptSubmit","prompt":"...","session_id":"snowy-efraasia","prompt_id":"<uuid>"}
{"hook_event_name":"PostToolUse","tool_name":"write","tool_input":{"file_path":"/abs/path","content":"..."},"tool_use_id":"write_0","tool_response":{"success":true,"output":"...","error":null},"session_id":"...","prompt_id":"<uuid>"}
{"hook_event_name":"Stop","stop_hook_active":false,"session_id":"...","prompt_id":"<uuid>"}
{"hook_event_name":"SessionEnd","reason":"session_complete","session_id":"...","prompt_id":"<uuid>"}
```

- `session_id` is a word-pair (e.g. `snowy-efraasia`) — the same ID
  `devin -r <id>` accepts and `devin list` shows.
- `prompt_id` is a per-turn UUID, absent before the first prompt.
- Hook processes get exactly one Devin-set env var: `DEVIN_PROJECT_DIR`
  (project root).

## Cross-fire hazard (verified live)

Devin loads `.claude/settings.json` hooks by default, so in a repo where
Entire is enabled for Claude Code, Devin sessions invoke
`entire hooks claude-code <verb>` with Devin payloads (no transcript_path).
The transcript-path ownership guard in the CLI fails open for these (no
SessionRef), so a claude-code session-start can claim the Devin session. A
reliable CLI-side guard exists: a claude-code hook invocation with
`DEVIN_PROJECT_DIR` set and `CLAUDE_PROJECT_DIR` unset is Devin-origin.
This needs a small entireio/cli change (it cannot live in an external
agent); until then, avoid enabling claude-code and running Devin in the
same repo, or set `read_config_from.claude=false` in `.devin/config.json`.

## Session storage and transcripts

- Live session store: SQLite at `~/.local/share/devin/cli/sessions.db`
  (message-node forest). Not read by this integration.
- Canonical transcript: **ATIF** (`schema_version: "ATIF-v1.7"`) JSON written
  to `~/.local/share/devin/cli/transcripts/<session_id>.json`
  (`%APPDATA%\devin\cli\transcripts` on Windows; `$XDG_DATA_HOME` honored).
  The directory is flat — not per-project. `devin --export [PATH]` writes the
  identical document.
- **Flush timing (important):** the transcript file is written on session
  exit, not per turn. In print mode it is written between the `Stop` and
  `SessionEnd` hooks (Devin awaits Stop hooks before proceeding — a Stop
  hook that waits ~2s observes the file landing right after it gives up).
  In interactive mode it is only written when the session ends (`/exit`),
  verified via pty-driven multi-turn run. The framework hard-requires the
  transcript file to exist at TurnEnd, so `PrepareTranscript` polls briefly
  and then MATERIALIZES a minimal valid ATIF stub when the file is still
  missing (the sanctioned OpenCode pattern — lazily created transcripts).
  Devin overwrites the file with the complete transcript at session exit,
  and the eager condensation at SessionEnd (endSessionNow) captures that
  full version. Mid-session checkpoints may therefore carry a stub or a
  previous run's transcript — the documented graceful degradation for v1.
- Resumed sessions append to the same transcript file (verified: 11 steps →
  14 steps after `devin -r`), so every checkpoint stores the full history.

## ATIF format (verified)

```json
{
  "schema_version": "ATIF-v1.7",
  "session_id": "almond-cylinder",
  "agent": {"name":"devin","version":"3000.2.17","model_name":"SWE-1.7","extra":{...}},
  "steps": [
    {"step_id":7,"timestamp":"...","source":"user","message":"<prompt>","extra":{...}},
    {"step_id":9,"source":"agent","message":"","model_name":"SWE-1.7",
     "reasoning_content":"...",
     "tool_calls":[{"tool_call_id":"read_0","function_name":"read","arguments":{"file_path":"/abs"}}],
     "metrics":{"prompt_tokens":12896,"completion_tokens":254,"cached_tokens":12430}}
  ],
  "final_metrics": {"total_prompt_tokens":65296,"total_completion_tokens":769,"total_cached_tokens":62771,"total_steps":14}
}
```

- Token mapping: `prompt_tokens` includes cache reads, so
  `InputTokens = prompt_tokens - cached_tokens`,
  `CacheReadTokens = cached_tokens`, `OutputTokens = completion_tokens`.
- File modifications appear as `tool_calls` with `function_name` `write`,
  `edit`, or `notebook_edit` and an `arguments.file_path` (absolute).
  PostToolUse hooks carry the same data live (used for ToolUse events).
  Caveat: when hooks.v1.json contains multiple PostToolUse matcher groups,
  Devin has been observed to run secondary groups' commands without piping
  the payload — post-tool-use parsing is therefore fully best-effort and
  never returns an error.

## Resume

`devin -r <session_id>` resumes a session (same session ID fires in
SessionStart), `devin -c` continues the most recent one in the cwd.
Session IDs are word-pairs and pass `validation.ValidateSessionID`.

## Verified end-to-end (clean repo, devin 3000.2.17, in-tree prototype)

- `entire enable --agent devin` installs hooks into `.devin/hooks.v1.json`,
  preserving pre-existing user hooks.
- Interactive session: per-turn Stop checkpoint creates the shadow branch
  mid-session; a mid-session `git commit` received the `Entire-Checkpoint`
  trailer.
- Commit-after-exit flow: condensation re-reads the canonical transcript at
  condense time, so `entire/checkpoints/v1` gets the complete ATIF document
  with real token usage (verified: 10 steps, input/cache/output tokens and
  API call count extracted from per-step `metrics`).
- `entire checkpoint explain`, `entire session list` (agent, model, tokens),
  `entire session resume` and `entire checkpoint rewind --to` all verified.

## Known v1 limitations

- **Summary generation is limited to the first checkpoint of a session
  (experimental).** `session_ref` is Devin's canonical ATIF transcript — a
  single JSON document. Entire scopes an external agent's transcript for
  `checkpoint explain --generate` by *line* offset (`transcript.SliceFromLine`)
  starting at the checkpoint's transcript-start (a step index). For any
  checkpoint after the first, that line-slices the JSON document into an
  unparseable fragment, so `compact-transcript` fails and Entire reports
  "transcript has no content to summarize." The proper fix is to materialize
  an Entire-owned JSONL transcript (one ATIF step per line) so line index ==
  step index — the same approach `entire-agent-kilo` uses — but it reworks the
  live-verified flush-timing path and needs validation against a real `devin`
  binary, so it is deferred. Tracked as a follow-up. Checkpointing,
  attribution, transcript analysis, and token accounting are unaffected; the
  agent is `is_preview: true`.
- Rewind restores the ATIF transcript file, but Devin resumes conversations
  from its SQLite store — a rewound transcript does not truncate Devin's own
  conversation memory, and cross-machine resume requires the session to exist
  in the local `sessions.db`. (Devin has an internal node-import used for
  cloud handoff; no public import command existed at integration time.)
- Interactive-mode checkpoints lag the live turn (see flush timing above).
  Consequence: a commit made mid-session condenses with the stub (or a
  previous run's) transcript for that checkpoint entry; the complete
  transcript lands in v1 with the next condensation that happens after the
  session run ends (each checkpoint stores the full session, so a later
  checkpoint heals the history). A session that only ever condenses
  mid-session keeps the stub for its final entry — a candidate framework
  follow-up is refreshing the cached transcript at SessionEnd before the
  eager condense.
- No subagent hooks (`SubagentStop` does not exist in Devin).
- No live per-tool file tracking (see Protocol mapping above — the external
  protocol Event object carries no modified_files).
- Context injection (`hookSpecificOutput.additionalContext`) and
  `systemMessage` display are parsed by Devin's Claude-compat layer per
  binary inspection, but not yet verified live — not wired in v1.
