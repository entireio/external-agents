# Checkpoint 1 — Track 3 Architecture

**Project:** CodeTriage  
**Track:** BTW Buildathon Track 3 — custom Entire external agent  
**Repo:** [HotaroOreki-art/CodeTriage](https://github.com/HotaroOreki-art/CodeTriage#external-agents-for-entire-cli)

## Goal

Build a pre-commit CLI gatekeeper as an Entire external agent. Entire already records AI coding sessions; CodeTriage adds a safety valve: if an agent commit has a dangerous blast radius, Entire must refuse it before the change lands.

## Why a custom external agent

Entire discovers `entire-agent-*` binaries on `PATH` and talks to them through a versioned JSON subcommand protocol (`info`, `detect`, session helpers, hooks). Track 3 is not another coding model. It is a policy agent:

- `start` / `stop` keep session lifecycle aligned with Entire checkpoints
- `commit` is the enforcement point
- rejection is a first-class protocol response, not an out-of-band script

That lets any Entire-enabled coding agent inherit the same ESI gate.

## Architecture

```
AI coding agent
    │  (write / edit / commit)
    ▼
Entire CLI  ── parse-hook --hook commit ──►  entire-agent-codetriage
                                                 │
                                                 ├─ identify modified files
                                                 ├─ Entire Graph reverse edges
                                                 ├─ level-tracked BFS
                                                 ├─ ESI classification
                                                 ├─ Databricks MLflow telemetry
                                                 └─ allow or reject (protocol)
```

### 1. Protocol surface

Implemented in Python at `agents/entire-agent-codetriage/` so the `mlflow` SDK is native and protocol checks can run without a Go toolchain.

Declared hooks: `start`, `stop`, `commit`.  
Capabilities: `hooks`, `hook_response_writer`.

### 2. Blast radius (commit hook)

1. Collect files from hook payload, session records, or `git diff`.
2. Load reverse dependents:
   - live: `entire graph edges` / `snapshot` NDJSON (`relation` records)
   - offline: `.codetriage/graph.json`
3. BFS from each seed file. Queue entries carry depth.
4. **ESI Level 1 (CRITICAL)** when `depth >= 3` or `impacted_files >= 10`.
5. On Level 1:
   - stdout Event with `metadata.decision=block` and `response_message`
   - non-zero exit so Entire treats the commit as rejected
   - stderr carries the same rejection text

### 3. Telemetry

Each commit evaluation logs to Databricks MLflow via the official SDK:

| Field | Kind |
| --- | --- |
| `esi_level` | param + metric |
| `impacted_count` | metric |
| `depth` | metric |
| `blocked` | param + metric |

Credentials come from `.env` (`DATABRICKS_HOST`, `DATABRICKS_TOKEN`, `MLFLOW_TRACKING_URI`). Missing credentials skip logging; the gate still runs.

## Discovery

`mise.toml` exposes `build` and `test` so the repo lifecycle harness auto-discovers `agents/entire-agent-codetriage/`.

## Verification for this checkpoint

- Unit tests cover BFS depth, fan-out, hook install, and protocol CLI shapes
- Commit fixture with a 3-hop reverse chain is rejected as ESI Level 1
- MLflow calls are skipped offline so CI does not need Databricks
