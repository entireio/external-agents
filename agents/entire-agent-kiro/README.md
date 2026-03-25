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

## Testing

Kiro is validated in three places:

- **Unit tests** live in this module and cover the Kiro-specific implementation details.
- **Protocol compliance** runs in GitHub Actions through [`entireio/external-agents-tests`](https://github.com/entireio/external-agents-tests) against the built `entire-agent-kiro` binary.
- **Lifecycle tests** live in the shared repo-root [`e2e/`](../../e2e/) harness and require `entire` plus `kiro-cli-chat`.

The lifecycle suite covers:

- **SinglePromptManualCommit** — agent creates file → commit → checkpoint with trailer
- **MultiplePromptsManualCommit** — two prompts → single commit → checkpoint covers both
- **DetectAndEnable** — `entire enable` succeeds when `.kiro/` exists
- **HooksInstalledAfterEnable** — `are-hooks-installed` confirms hooks after enable
- **RewindPreCommit** — create file A → checkpoint → create file B → rewind → B is gone
- **RewindAfterCommit** — two commits → rewind to first → second file is gone
- **SessionPersistence** — session file created in `.entire/tmp/` after prompt

### Running

```bash
# From this module:
make test                    # Unit tests

# From the repo root:
make test-e2e                # Lifecycle tests
make test-e2e-lifecycle      # Explicit lifecycle target

# Run a specific test:
cd e2e && go test -tags=e2e -v -count=1 -run TestLifecycle_SinglePromptManualCommit ./...
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
