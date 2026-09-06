# BTW Buildathon — Track 3 Submission

> Template filled for **CodeTriage**, a custom Entire external agent that uses Entire Graph context to enforce ESI commit gating.

## Team

| Field | Value |
| --- | --- |
| Project | CodeTriage |
| Track | Track 3 — Entire external agent / graph-aware tooling |
| Repository | https://github.com/HotaroOreki-art/CodeTriage |
| Agent binary | `entire-agent-codetriage` |

## Problem

AI coding agents can land a one-line change that ripples through auth, billing, or shared utilities. Entire already checkpoints *what* an agent did. Teams still need a deterministic answer to: **how dangerous is this commit, and should it be allowed?**

## Solution

CodeTriage is a pre-commit CLI gatekeeper implemented as an Entire external agent:

1. Entire invokes the `commit` hook through the public external-agent protocol.
2. The agent lists files the session is modifying.
3. It walks Entire Graph **reverse dependencies** with a level-tracked BFS.
4. It computes an Emergency Severity Index (ESI).
5. ESI Level 1 (depth >= 3 **or** impacted files >= 10) **blocks the commit** and returns a protocol rejection.
6. Databricks MLflow records ESI level, impacted count, depth, and blocked status.

## Architecture (Track 3)

```
Entire CLI  →  entire-agent-codetriage
                 ├─ start / stop / commit hooks
                 ├─ Entire Graph reverse-dependency BFS
                 ├─ ESI Level 1 commit gate
                 └─ mlflow SDK telemetry (.env credentials)
```

Details: [`docs/checkpoints/checkpoint_1.md`](docs/checkpoints/checkpoint_1.md)

## Entire integration

| Requirement | How we meet it |
| --- | --- |
| External agent protocol | All required subcommands + hooks capability |
| Hook names | `start`, `stop`, `commit` |
| Graph context | `entire graph edges/snapshot` NDJSON, file-level reverse BFS |
| Lifecycle discovery | `agents/entire-agent-codetriage/mise.toml` (`build`, `test`) |
| Rejection | Event `metadata.decision=block` + non-zero `parse-hook` exit |

## Databricks / MLflow

| Variable | Purpose |
| --- | --- |
| `DATABRICKS_HOST` | Workspace URL |
| `DATABRICKS_TOKEN` | PAT |
| `MLFLOW_TRACKING_URI` | Usually `databricks` |
| `MLFLOW_EXPERIMENT_NAME` | Default `codetriage-esi` |

Loaded from `.env` via `python-dotenv`. Offline runs disable telemetry automatically.

## Demo script

```bash
cd agents/entire-agent-codetriage
mise run build
mise run test
# optional: external-agents-tests verify ./entire-agent-codetriage

# In a target repo with external_agents enabled:
entire enable --agent codetriage --telemetry=false
# Trigger a high-radius commit — Entire receives a Level 1 rejection
```

## What judges should look at

1. `agents/entire-agent-codetriage/src/entire_agent_codetriage/blast_radius.py` — ESI rules
2. `agents/entire-agent-codetriage/src/entire_agent_codetriage/hooks.py` — commit rejection
3. `agents/entire-agent-codetriage/src/entire_agent_codetriage/telemetry.py` — MLflow
4. This file + Checkpoint 1 for the Track 3 write-up
