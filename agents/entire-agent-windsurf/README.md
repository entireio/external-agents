# Windsurf External Agent for Entire CLI

> **Preview** — this integration is functional but not yet stable. Hook names and payload shapes may change as Windsurf's hooks API evolves.

Enables Entire CLI checkpoints, rewind, and transcript capture for [Windsurf](https://windsurf.com) Cascade coding sessions. Once installed, Entire automatically tracks your Windsurf sessions — creating checkpoints on commits and capturing transcripts for review.

## Prerequisites

- **Entire CLI** installed and on `PATH`
- **Windsurf** installed
- **Go 1.26+** (to build from source)

## Quick Start

### 1. Build the binary

```bash
cd agents/entire-agent-windsurf
go build -o entire-agent-windsurf ./cmd/entire-agent-windsurf
```

Or install directly:

```bash
go install ./cmd/entire-agent-windsurf
```

### 2. Verify the agent is discoverable

```bash
entire-agent-windsurf info
```

Should print JSON describing the agent's capabilities.

### 3. Enable the agent in your project

```bash
cd /path/to/your/project
entire enable
```

Entire discovers `entire-agent-windsurf` on your `PATH` and installs the hooks automatically.

### 4. Verify hooks are installed

```bash
entire-agent-windsurf are-hooks-installed
```

Should return `{"installed": true}`.

### 5. Start using Windsurf

Entire will now automatically capture checkpoints and transcripts during your Windsurf Cascade sessions.

## What Gets Installed

When you run `entire enable --agent windsurf`, the agent installs hooks in:

| Location | Purpose |
|----------|---------|
| `.windsurf/hooks.json` | Workspace-level Cascade hook configuration |

The config uses Windsurf's native top-level `"hooks"` format with three lifecycle hooks:

```json
{
  "hooks": {
    "pre_user_prompt":       [{ "command": "entire hooks windsurf pre_user_prompt" }],
    "post_write_code":       [{ "command": "entire hooks windsurf post_write_code" }],
    "post_cascade_response": [{ "command": "entire hooks windsurf post_cascade_response" }]
  }
}
```

On Windows, `"powershell"` is used instead of `"command"`.

## Hook Lifecycle

| Windsurf hook | Entire event | What it records |
|---|---|---|
| `pre_user_prompt` | TurnStart (type 2) | Session ID from `trajectory_id`, user prompt text |
| `post_write_code` | *(no event)* | File path written to turn transcript |
| `post_cascade_response` | TurnEnd (type 3) | Response text + `session_ref` path |

Each Windsurf conversation (`trajectory_id`) maps to one Entire session. Transcripts are stored as JSONL at `.entire/tmp/<trajectory_id>.json`.

## Capabilities

| Capability | Supported | Description |
|------------|-----------|-------------|
| `hooks` | Yes | Installs and manages Windsurf Cascade lifecycle hooks |
| `transcript_analyzer` | Yes | Extracts modified files, prompts, and summaries from transcripts |
| `compact_transcript` | Yes | Produces compact JSONL for checkpoint storage |
| `transcript_preparer` | No | — |
| `token_calculator` | No | — |
| `text_generator` | No | — |
| `hook_response_writer` | No | — |
| `subagent_aware_extractor` | No | — |

## Supported Subcommands

All subcommands required by the [external agent protocol](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md):

**Core:** `info`, `detect`, `get-session-id`, `get-session-dir`, `resolve-session-file`, `read-session`, `write-session`, `format-resume-command`

**Hooks:** `parse-hook`, `install-hooks`, `are-hooks-installed`, `uninstall-hooks`

**Transcript:** `read-transcript`, `chunk-transcript`, `reassemble-transcript`, `compact-transcript`, `get-transcript-position`, `extract-modified-files`, `extract-prompts`, `extract-summary`

## Development

```bash
# From agents/entire-agent-windsurf:
go build ./...          # Build
go test ./...           # Run unit tests
go build -o entire-agent-windsurf ./cmd/entire-agent-windsurf  # Produce binary

# Run directly without installing:
go run ./cmd/entire-agent-windsurf info
```

## Testing

Windsurf is validated in two places:

- **Unit tests** live in this module (`internal/windsurf/*_test.go`) and cover hook parsing, hook install/uninstall, transcript extraction, and compact output.
- **Protocol compliance** runs in GitHub Actions through [`entireio/external-agents-tests`](https://github.com/entireio/external-agents-tests) against the built `entire-agent-windsurf` binary.

Windsurf is an IDE agent with no automatable CLI, so the repo-root `e2e/` lifecycle harness does not run it by default. It is registered only when `E2E_AGENT=windsurf` is set explicitly.

### Running unit tests

```bash
# From this module:
go test ./...

# With verbose output:
go test -v ./...
```

## Troubleshooting

**Agent not discovered by Entire**
- Verify the binary is on your `PATH`: `which entire-agent-windsurf`
- Confirm external-plugin discovery is enabled: `external_agents: true` must be set in your repo's `.entire/settings.json`
- Check detection: `ENTIRE_REPO_ROOT=$PWD entire-agent-windsurf detect`

**`entire enable` doesn't install hooks**

Install hooks directly:

```bash
cd /path/to/your/project
ENTIRE_REPO_ROOT=$PWD entire-agent-windsurf install-hooks --force
```

Add `--local-dev` to use the local-dev hook command form (references `${WINDSURF_PROJECT_DIR}` instead of `entire`).

**Hooks not firing**
- Verify `.windsurf/hooks.json` exists in your project
- Confirm Windsurf has workspace hooks enabled (they are on by default for workspace-level config)
- Check that `entire` is on your `PATH` when Windsurf spawns the hook shell

## Protocol

This agent implements the [Entire external agent protocol](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md).
