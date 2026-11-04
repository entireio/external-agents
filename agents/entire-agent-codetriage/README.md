# entire-agent-codetriage

Python Entire CLI external agent that gates commits with a CodeTriage blast-radius check.

The binary implements the [Entire external agent protocol](https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md) and installs `start`, `stop`, and `commit` hooks. On `commit`, it:

1. Collects the files the agent is about to change
2. Runs a level-tracked BFS over Entire Graph reverse dependencies
3. Assigns ESI Level 1 (CRITICAL) when depth >= 3 or impacted files >= 10
4. Rejects the commit through the protocol when ESI is Level 1
5. Logs ESI level, impacted count, depth, and blocked status to Databricks MLflow

## Build and test

```bash
cd agents/entire-agent-codetriage
mise run build
mise run test
external-agents-tests verify ./entire-agent-codetriage
```

On Windows, `entire-agent-codetriage.cmd` is the PATH-friendly launcher.

## Enable in a repo

```bash
# copy entire-agent-codetriage onto PATH
entire enable --agent codetriage --telemetry=false
```

Set `external_agents: true` in the repo's untracked `.entire/settings.local.json`.

## Databricks MLflow

Copy `.env.example` to `.env` in the target repo (or this agent directory):

```
DATABRICKS_HOST=https://<workspace>.cloud.databricks.com
DATABRICKS_TOKEN=<personal-access-token>
MLFLOW_TRACKING_URI=databricks
MLFLOW_EXPERIMENT_NAME=codetriage-esi
```

Telemetry is skipped when credentials are absent so protocol tests stay offline.

## Optional graph fixture

If `entire graph` is unavailable, place reverse-dependency JSON at `.codetriage/graph.json`:

```json
{
  "reverse_dependencies": {
    "src/core.py": ["src/api.py", "src/jobs.py"]
  }
}
```
