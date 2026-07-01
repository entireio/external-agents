# Grok Build External Agent

Grok Build exposes command hooks in `.grok/hooks/*.json` and stores sessions under `~/.grok/sessions/<encoded-cwd>/<session-id>/`. Entire hooks Grok lifecycle events and reads the native `chat_history.jsonl` transcript for checkpoints, prompts, file changes, and summaries.

## Prerequisites

| Check | Status | Notes |
|-------|--------|-------|
| Binary present | Optional | `grok` on `PATH`; protocol commands still run without it |
| Help available | PASS | Grok Build documents `grok`, `--continue`, and headless prompts |
| Hook system | PASS | Project hooks in `.grok/hooks/*.json` |
| Native sessions | PASS | `~/.grok/sessions/<encoded-cwd>/<session-id>/chat_history.jsonl` |

## Protocol Mapping

| Subcommand | Source | Notes |
|------------|--------|-------|
| `info` | Static metadata | Name `grok`, type `Grok Build` |
| `detect` | CLI availability | Checks `grok` on `PATH` |
| `get-session-id` | Hook stdin | `session_id` or `sessionId` |
| `get-session-dir` | Native store | `~/.grok/sessions/<encoded-cwd>` |
| `resolve-session-file` | Session id | `<session-dir>/<id>/chat_history.jsonl` |
| `read-session` / `write-session` | Native transcript | Raw `chat_history.jsonl` bytes |
| `parse-hook` | Grok hook JSON | Maps lifecycle to Entire events |
| `install-hooks` | `.grok/hooks/entire.json` | Read-modify-write command hooks while preserving user hook entries |
| `are-hooks-installed` | `.grok/hooks/entire.json` | Requires all Entire Grok hooks |
| `uninstall-hooks` | `.grok/hooks/entire.json` | Removes only Entire hook entries |
| `format-resume-command` | Grok CLI | `grok --continue` |

## Hook Events

Installed hooks: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `Stop`, `StopFailure`, `SessionEnd`, `PreCompact`, `PostCompact`, `PostToolUse`, `PostToolUseFailure`, `Notification`, `PermissionDenied`, `SubagentStart`, `SubagentStop`.

## Lifecycle Testing

Default CI does not require Grok credentials. Real lifecycle testing is gated with `GROK_E2E=1 E2E_AGENT=grok`.