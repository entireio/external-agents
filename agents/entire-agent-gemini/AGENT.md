# entire-agent-gemini

External agent for Gemini CLI integration with Entire.

## Build

```bash
go build -o entire-agent-gemini ./cmd/entire-agent-gemini
```

## Test

```bash
go test ./...
```

## Protocol

Implements the Entire external agent protocol v1 with:
- Hooks (8 events: SessionStart, BeforeAgent, AfterAgent, BeforeTool, AfterTool, PreCompress, Notification, SessionEnd)
- Transcript analysis (sidecar JSONL)
- Compact transcript generation
