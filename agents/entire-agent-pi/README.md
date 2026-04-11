# entire-agent-pi

External agent binary that teaches the [Entire CLI](https://github.com/entireio/cli) how to work with [Pi](https://pi.dev), the AI coding agent.

## Capabilities

| Capability | Status |
|-----------|--------|
| hooks | Yes — TypeScript extension in `.pi/extensions/entire/` |
| transcript_analyzer | Yes — parses JSONL session files for prompts, files, summary |
| token_calculator | Yes — sums token usage from assistant messages |

## Installation

Build the binary and place it on your `PATH`:

```bash
cd agents/entire-agent-pi
go build -o entire-agent-pi ./cmd/entire-agent-pi
cp entire-agent-pi /usr/local/bin/
```

Or use mise:

```bash
cd agents/entire-agent-pi
mise run build
```

## Prerequisites

- [Pi](https://pi.dev) CLI installed (`pi` on PATH)
- An LLM provider API key configured for Pi (e.g., `ANTHROPIC_API_KEY`)

## How It Works

### Hooks

`install-hooks` creates a TypeScript extension at `.pi/extensions/entire/index.ts` that intercepts Pi lifecycle events and forwards them to `entire agent hook pi <event>`. It also writes `.entire/settings.local.json` with `"commit_linking": "always"` so Pi-driven git commits do not trigger an interactive Entire prompt inside the agent loop:

| Pi Event | Protocol Event |
|----------|---------------|
| `session_start` | SessionStart (type 1) |
| `before_agent_start` | TurnStart (type 2) |
| `agent_end` | TurnEnd (type 3) |
| `session_shutdown` | (cleanup, no protocol event) |

### Transcripts

Pi stores sessions as JSONL files at `~/.pi/agent/sessions/<encoded-path>/`. The binary reads these directly for transcript analysis, extracting:

- Modified files from `write` and `edit` tool calls
- User prompts from `role: "user"` messages
- Summary from the last assistant text response
- Token usage from assistant message `usage` fields

### Session Management

Session files are cached in `.entire/tmp/<session-id>.json` as required by the Entire protocol. The session ID is the UUID from the Pi session filename.

## Development

```bash
# Build
go build -o entire-agent-pi ./cmd/entire-agent-pi

# Unit tests
go test ./...

# Protocol compliance
external-agents-tests verify ./entire-agent-pi

# E2E lifecycle tests (requires entire CLI and pi on PATH)
cd ../../
E2E_AGENT=pi mise run test-e2e
```
