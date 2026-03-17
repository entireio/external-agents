# entire-agent-kiro

Standalone external-agent binary for Entire's Kiro integration.

## Status

Implemented and validated against the Entire external-agent protocol for:

- required subcommands
- `hooks` capability subcommands
- `transcript_analyzer` capability subcommands

The binary exposes one logical `kiro` agent and supports both Kiro CLI and Kiro IDE integration paths.

## Capabilities

`entire-agent-kiro info` declares:

- `hooks: true`
- `transcript_analyzer: true`
- `transcript_preparer: false`
- `token_calculator: false`
- `text_generator: false`
- `hook_response_writer: false`
- `subagent_aware_extractor: false`

## Build

```bash
make build
```

This produces `./entire-agent-kiro`.

## Install

Entire discovers external agents on `PATH` by binary name, so install the built binary somewhere on `PATH`.

Example:

```bash
make build
cp ./entire-agent-kiro ~/.local/bin/entire-agent-kiro
```

After installation, Entire should discover the agent as `kiro`.

## Hook Install Side Effects

`install-hooks` updates repo-local Kiro configuration in three places:

- `.kiro/agents/entire.json`
- `.kiro/hooks/*.kiro.hook`
- `.vscode/settings.json`

The trusted command entry added to VS Code settings is:

- production: `entire hooks *`
- local dev: `go run ${KIRO_PROJECT_DIR}/cmd/entire/main.go hooks *`

`uninstall-hooks` removes Entire-owned hook entries and trusted-command entries while preserving unrelated user settings.

## Development

```bash
make test
make build
go run ./cmd/entire-agent-kiro info
```

## Validation

The passing validation flow used:

1. `make build`
2. a temporary git repository with:
   - `ENTIRE_REPO_ROOT=<temp-repo>`
   - `ENTIRE_PROTOCOL_VERSION=1`
3. direct checks for all required protocol subcommands
4. capability checks for `hooks` and `transcript_analyzer`

The validated surface was:

- `info`
- `detect`
- `get-session-id`
- `get-session-dir`
- `resolve-session-file`
- `read-session`
- `write-session`
- `read-transcript`
- `chunk-transcript`
- `reassemble-transcript`
- `format-resume-command`
- `parse-hook`
- `install-hooks`
- `are-hooks-installed`
- `uninstall-hooks`
- `get-transcript-position`
- `extract-modified-files`
- `extract-prompts`
- `extract-summary`

Latest validation result:

- required checks: 10/10 PASS
- capability checks: 12/12 PASS
- total: 22 PASS, 0 FAIL

## Protocol

This project follows the Entire external-agent protocol implemented in the main CLI repository.
