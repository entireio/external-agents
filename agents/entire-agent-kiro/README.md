# entire-agent-kiro

Standalone external-agent binary for Entire's Kiro integration.

## Status

This project is currently scaffolded with protocol-shaped stubs for:

- required external-agent subcommands
- `hooks` capability subcommands
- `transcript_analyzer` capability subcommands

The scaffold is intentionally minimal but compiles, returns valid JSON, and is ready for incremental implementation.

## Development

```bash
make test
make build
go run ./cmd/entire-agent-kiro info
```

## Protocol

The scaffold follows the current Entire external-agent protocol as defined in the main CLI repository.
