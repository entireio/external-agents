# Grok Build External Agent

Grok Build exposes command hooks in `.grok/hooks/*.json` and stores sessions under `~/.grok/sessions/<encoded-cwd>/<session-id>/`. This integration uses Grok hooks as the lifecycle source of truth and writes an Entire-owned sidecar transcript under a repo-scoped OS temp directory so Entire can read stable session data even if Grok's native transcript evolves.

## Prerequisites

| Check | Status | Notes |
|-------|--------|-------|
| Binary present | Optional | `grok` on `PATH`; protocol commands still run without it |
| Help available | PASS | Grok Build documents `grok`, `--continue`, and headless prompts |
| Hook system | PASS | Project hooks in `.grok/hooks/*.json` |
| Config directory | PASS | Workspace `.grok/hooks/entire.json` |

## Protocol Mapping

| Subcommand | Source | Notes |
|------------|--------|-------|
| `info` | Static metadata | Name `grok`, type `Grok Build`, preview |
| `detect` | CLI availability | Checks `grok` on `PATH` |
| `get-session-id` | Hook stdin | `session_id` or `sessionId` |
| `get-session-dir` | Repo hash | `/tmp/entire-grok/<hash>` |
| `resolve-session-file` | Session id | `<session-dir>/<id>.jsonl` sidecar |
| `read-session` / `write-session` | Sidecar JSONL | Entire-owned transcript |
| `read-transcript` | Sidecar path | Raw bytes |
| `parse-hook` | Grok hook JSON | Maps lifecycle to Entire events |
| `install-hooks` | `.grok/hooks/entire.json` | Read-modify-write command hooks while preserving user hook entries |
| `are-hooks-installed` | `.grok/hooks/entire.json` | Requires all Entire Grok hooks |
| `uninstall-hooks` | `.grok/hooks/entire.json` | Removes only Entire hook entries |
| `format-resume-command` | Grok CLI | `grok --continue` |

## Hook Events

Installed hooks: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `Stop`, `StopFailure`, `SessionEnd`, `PreCompact`, `PostCompact`, `PostToolUse`, `PostToolUseFailure`, `Notification`, `PermissionDenied`, `SubagentStart`, `SubagentStop`.

## Lifecycle Testing

Default CI does not require Grok credentials. Real lifecycle testing is gated with `GROK_E2E=1 E2E_AGENT=grok`.