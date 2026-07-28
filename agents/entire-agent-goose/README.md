# entire-agent-goose

External agent binary that teaches [Entire CLI](https://github.com/entireio/cli)
how to work with [Goose](https://github.com/block/goose), Block's open-source
AI coding agent.

See [AGENT.md](AGENT.md) for the full research one-pager: hook mechanism,
session storage, transcript format, and protocol mapping (all verified against
goose 1.37.0).

## How it works

- **Hooks** — `install-hooks` writes a project-scope goose plugin at
  `<repo>/.agents/plugins/entire/` whose `hooks/hooks.json` forwards goose's
  native lifecycle events (`SessionStart`, `UserPromptSubmit`, `Stop`,
  `SessionEnd`) to `entire hooks goose <name>`. Goose discovers project
  plugins from its working directory automatically; no global config is
  touched.
- **Sessions** — goose stores sessions in a SQLite database
  (`~/.local/share/goose/sessions/sessions.db`). There is no per-session
  file, so `prepare-transcript` materializes one via
  `goose session export --session-id <id> --format json`.
- **Tokens** — accumulated input/output token totals come from the exported
  session JSON.

## Build

```bash
mise run build       # go build -o entire-agent-goose ./cmd/entire-agent-goose
mise run test        # go test ./...
```

## Install

Put `entire-agent-goose` on your `PATH`, then opt in to external agents in
the repo's `.entire/settings.json`:

```json
{
  "external_agents": true
}
```

## Requirements

- `goose` CLI on `PATH` (verified against 1.37.0) with a configured provider
- Entire CLI

## Research verification

`scripts/verify-goose.sh` wires a capture plugin into a throwaway probe
workspace and runs a headless `goose run -t` to capture real hook payloads.
It never modifies global goose config.
