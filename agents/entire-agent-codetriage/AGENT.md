# CodeTriage External Agent

| Field | Value |
| --- | --- |
| Agent name | CodeTriage |
| Slug | `codetriage` |
| Binary | `entire-agent-codetriage` |
| Language | Python 3.10+ |
| Protocol version | 1 |

## Target

CodeTriage is a pre-commit gatekeeper, not a coding agent that writes files. It teaches Entire how to capture start/stop/commit lifecycle events and how to reject high blast-radius commits.

## Protocol mapping

| Subcommand | Behavior |
| --- | --- |
| `info` | Declares hooks + hook response writer; hook names `start`, `stop`, `commit` |
| `detect` | Always present once the binary is on PATH |
| Session helpers | Store sessions under a hashed OS temp dir; marker under `.entire/tmp` |
| `parse-hook --hook start` | Event type 1 (`SessionStart`) |
| `parse-hook --hook stop` | Event type 5 (`SessionEnd`) |
| `parse-hook --hook commit` | Event type 3 (`TurnEnd`); runs ESI BFS; exit 1 + `response_message` on Level 1 |
| `install-hooks` | Writes `.codetriage/hooks.json` (idempotent; `--local-dev` is a no-op) |
| `write-hook-response` | JSON `{"message": "..."}` |

## Commit gate

1. Files come from hook `modified_files` / `tool_input` / session / `git diff`.
2. Reverse edges come from `entire graph edges|snapshot` NDJSON, or `.codetriage/graph.json`.
3. BFS is level-tracked. ESI Level 1 if depth >= 3 **or** impacted files >= 10.
4. Level 1 returns a protocol rejection (`metadata.decision=block`, non-zero exit).

## Telemetry

`mlflow` SDK logs `esi_level`, `impacted_count`, `depth`, and `blocked` after each commit evaluation. Credentials load from `.env`.

## Tests

- Unit tests: `mise run test`
- Protocol compliance: `external-agents-tests verify ./entire-agent-codetriage`
- Lifecycle: requires Entire CLI + this binary on PATH (`E2E_AGENT=codetriage`)
