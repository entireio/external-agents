# Kiro External Agent for Entire CLI

Enables Entire CLI checkpoints, rewind, and transcript capture for [Kiro](https://kiro.dev) coding sessions. Once installed, Entire automatically tracks your Kiro sessions — creating checkpoints on commits and capturing transcripts for review.

## Prerequisites

- **Entire CLI** installed and on `PATH`
- **Kiro** (IDE or `kiro-cli-chat` CLI) installed
- **Go 1.26+** (to build from source)

## Quick Start

### 1. Build the binary

```bash
cd agents/entire-agent-kiro
make build
```

This produces `./entire-agent-kiro` in the current directory.

### 2. Install to PATH

```bash
cp ./entire-agent-kiro ~/.local/bin/
```

Or use Go install:

```bash
go install ./cmd/entire-agent-kiro
```

### 3. Verify the agent is discoverable

```bash
entire-agent-kiro info
```

This should print JSON describing the agent's capabilities.

### 4. Enable the agent in your project

```bash
cd /path/to/your/project
entire enable
```

### 5. Verify hooks are installed

```bash
entire-agent-kiro are-hooks-installed
```

Should return `{"installed": true}`.

### 6. Start using Kiro

Entire will now automatically capture checkpoints and transcripts during your Kiro sessions.

## What Gets Installed

When you run `entire enable --agent kiro`, the agent installs hooks in three locations:

| Location | Purpose |
|----------|---------|
| `.kiro/agents/entire.json` | Agent configuration for Kiro CLI |
| `.kiro/hooks/*.kiro.hook` | Lifecycle hooks (start, stop, commit) |
| `.vscode/settings.json` | Trusted command entry for Kiro IDE |

During a session, these hooks fire on lifecycle events (session start, stop, commit), allowing Entire to create checkpoints and capture what the AI agent did.

## Capabilities

| Capability | Supported | Description |
|------------|-----------|-------------|
| `hooks` | Yes | Installs and manages Kiro lifecycle hooks |
| `transcript_analyzer` | Yes | Extracts modified files, prompts, and summaries from transcripts |
| `transcript_preparer` | No | — |
| `token_calculator` | No | — |
| `text_generator` | No | — |
| `hook_response_writer` | No | — |
| `subagent_aware_extractor` | No | — |

## Supported Subcommands

All subcommands required by the [external agent protocol](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md):

**Core:** `info`, `detect`, `get-session-id`, `get-session-dir`, `resolve-session-file`, `read-session`, `write-session`, `format-resume-command`

**Hooks:** `parse-hook`, `install-hooks`, `are-hooks-installed`, `uninstall-hooks`

**Transcript:** `read-transcript`, `chunk-transcript`, `reassemble-transcript`, `get-transcript-position`, `extract-modified-files`, `extract-prompts`, `extract-summary`

## Development

```bash
make build    # Build the binary
make test     # Run unit tests
make clean    # Remove built binary

# Run directly without installing:
go run ./cmd/entire-agent-kiro info
```

## E2E Tests

E2E tests live in the shared `e2e/` directory at the repo root (not inside this agent's directory). The harness auto-discovers and builds all agents, then runs tests against each.

### Subcommand tests (`e2e/kiro_test.go`)

Exercise each protocol subcommand directly — no external dependencies needed:

- **Identity**: `info`, `detect` (present/absent)
- **Sessions**: `get-session-id`, `get-session-dir`, `resolve-session-file`, `write-session`/`read-session` round-trip
- **Transcript**: `read-transcript`, `chunk-transcript`/`reassemble-transcript` round-trip
- **Hooks**: `parse-hook` (spawn, prompt-submit, pre-tool-use, stop), `install-hooks`/`uninstall-hooks`/`are-hooks-installed`, idempotent install
- **Transcript analysis**: `get-transcript-position`, `extract-modified-files`, `extract-prompts`, `extract-summary`
- **Other**: `format-resume-command`, unknown subcommand handling

### Lifecycle tests (`e2e/kiro_lifecycle_test.go`)

Full integration tests requiring `entire` CLI and `kiro-cli-chat`:

- **SinglePromptManualCommit** — agent creates file → commit → checkpoint with trailer
- **MultiplePromptsManualCommit** — two prompts → single commit → checkpoint covers both
- **DetectAndEnable** — `entire enable` succeeds when `.kiro/` exists
- **HooksInstalledAfterEnable** — `are-hooks-installed` confirms hooks after enable
- **RewindPreCommit** — create file A → checkpoint → create file B → rewind → B is gone
- **RewindAfterCommit** — two commits → rewind to first → second file is gone
- **SessionPersistence** — session file created in `.entire/tmp/` after prompt

### Running

```bash
# From the repo root:
make test-e2e                # All E2E tests (lifecycle tests skip if deps missing)
make test-e2e-lifecycle      # Lifecycle tests only (fails if deps missing)

# Run a specific test:
cd e2e && go test -tags=e2e -v -count=1 -run TestKiro_Info ./...
```

## Troubleshooting

**Agent not discovered by Entire**
- Verify the binary is on your `PATH`: `which entire-agent-kiro`
- Check detection: `entire-agent-kiro detect` (requires `ENTIRE_REPO_ROOT` to be set)

**Hooks not firing**
- Verify `.kiro/agents/entire.json` exists in your project
- Check that `.kiro/hooks/` contains `*.kiro.hook` files
- For Kiro IDE: verify `.vscode/settings.json` has the trusted command entry

**IDE vs CLI differences**
- Kiro IDE uses VS Code's trusted command mechanism — hooks fire via `.vscode/settings.json`
- Kiro CLI (`kiro-cli-chat`) reads hooks directly from `.kiro/hooks/`
- Both paths are configured by `install-hooks`

## Protocol

This agent implements the [Entire external agent protocol](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md).
