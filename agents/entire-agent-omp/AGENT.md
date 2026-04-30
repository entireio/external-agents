# oh-my-pi (omp) — External Agent Research

## Verdict: COMPATIBLE (DERIVED, UNVERIFIED)

This wrapper is mechanically derived from `entire-agent-pi`. [oh-my-pi](https://github.com/can1357/oh-my-pi) is a fork of [badlogic/pi-mono](https://github.com/badlogic/pi-mono) and inherits the same TypeScript extension surface, lifecycle hook names, and JSONL session format. Source-level differences from upstream pi-mono that drove the fork:

| Difference | Impact on this wrapper |
|-----------|-----------------------|
| Binary renamed `pi` → `omp` | `Detect()`/`FormatResumeCommand()` updated |
| Config dir renamed `.pi` → `.omp` | `ProtectedDirs` and extension path updated |
| Extension API package: `@mariozechner/pi-coding-agent` → `@oh-my-pi/pi-coding-agent` | Generated extension `import` updated |
| Launch flags `--no-prompt-templates` and `--no-themes` removed | E2E test agent (`e2e/agents/omp.go`) drops these |

End-to-end protocol behaviour against the live `omp` binary has **not** been independently re-verified. If oh-my-pi diverges from pi-mono's JSONL schema (transcript entry shape, `usage` field names, tool-call structure), the transcript analyzer and compact-transcript output will silently misbehave. Run `scripts/verify-omp.sh --run-cmd 'omp -p "say hello"'` to capture live payloads and compare against the assumptions in `internal/omp/transcript.go`.

## Inherited from pi-mono (presumed unchanged)

The following come from pi-mono and are reproduced here only to document what this wrapper *assumes* is still true in oh-my-pi:

- Session file: `<base>/sessions/<encoded-path>/<ISO-timestamp>_<uuid>.jsonl` where `<base>` is `~/.omp/agent` (or `$PI_CODING_AGENT_DIR`)
- Path encoding: absolute path with `/` → `-`, wrapped in `--` prefix/suffix
- Session ID: UUID after the last `_` in the filename
- Resume: `omp --continue` (most recent) or `omp --session <path>`
- Transcript: JSONL tree (`id`, `parentId`); active branch resolves from last message backwards
- Entry types: `session`, `model_change`, `thinking_level_change`, `message`
- Message roles: `user`, `assistant`, `toolResult`
- Tool calls in assistant content: `{type: "toolCall", id, name, arguments}`; file-modifying tool names are `write` and `edit` with `arguments.path`
- Usage fields on assistant messages: `input`, `output`, `cacheRead`, `cacheWrite`

## Hook Mapping

Same as upstream pi:

| Native event | Protocol event |
|--------------|----------------|
| `session_start` | 1 = SessionStart |
| `before_agent_start` | 2 = TurnStart |
| `agent_end` | 3 = TurnEnd |
| `session_shutdown` | (cleanup, no protocol event) |

The generated extension imports `@oh-my-pi/pi-coding-agent` and forwards events to `entire hooks omp <event>`.
