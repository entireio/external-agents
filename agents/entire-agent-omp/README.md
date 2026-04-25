# entire-agent-omp

External agent binary that teaches the [Entire CLI](https://github.com/entireio/cli) how to work with [Oh My Pi](https://github.com/can1357/oh-my-pi) (`omp`), the AI coding agent.

## Capabilities

| Capability | Status |
|-----------|--------|
| hooks | Yes — TypeScript extension in `.omp/extensions/entire/` |
| transcript_analyzer | Yes — parses JSONL session files for prompts, files, summary |
| token_calculator | Yes — sums token usage from assistant messages |

## Installation

Build the binary and place it on your `PATH`:

```bash
cd agents/entire-agent-omp
go build -o entire-agent-omp ./cmd/entire-agent-omp
cp entire-agent-omp /usr/local/bin/
```

Or use mise:

```bash
cd agents/entire-agent-omp
mise run build
```

## Prerequisites

- [Oh My Pi](https://github.com/can1357/oh-my-pi) CLI installed (`omp` on PATH)
- An LLM provider API key configured for omp (e.g., `ANTHROPIC_API_KEY`)

## How It Works

### Hooks

`install-hooks` creates a TypeScript extension at `.omp/extensions/entire/index.ts` that intercepts omp lifecycle events and forwards them to `entire agent hook omp <event>`:

| omp Event | Protocol Event |
|-----------|---------------|
| `session_start` | SessionStart (type 1) |
| `before_agent_start` | TurnStart (type 2) |
| `agent_end` | TurnEnd (type 3) |
| `session_shutdown` | (cleanup, no protocol event) |

### Transcripts

omp stores sessions as JSONL files at `~/.omp/agent/sessions/<encoded-path>/`. The binary reads these directly for transcript analysis, extracting:

- Modified files from `write` and `edit` tool calls
- User prompts from `role: "user"` messages
- Summary from the last assistant text response
- Token usage from assistant message `usage` fields

omp-specific entry types (`ttsr_injection`, `session_init`, `mode_change`) are transparently ignored — they are not `message` type entries.

### Session Management

Session files are cached in `.entire/tmp/<session-id>.json` as required by the Entire protocol. The session ID is the snowflake hex ID from the omp session filename.

## Development

```bash
# Build
go build -o entire-agent-omp ./cmd/entire-agent-omp

# Unit tests
go test ./...

# Protocol compliance
external-agents-tests verify ./entire-agent-omp

# E2E lifecycle tests (requires entire CLI and omp on PATH)
cd ../../
E2E_AGENT=omp mise run test-e2e
```
