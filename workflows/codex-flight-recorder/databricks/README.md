# Databricks historical-risk layer

This folder makes Databricks an analytics producer for Codebase Flight Recorder. The local Go workflow consumes a reviewed export through `--history`; its `risk_score` is included in the final risk assessment, so the Databricks analysis changes the briefing rather than living in a disconnected dashboard.

## When a workspace is available

1. Create a Databricks Free Edition workspace and a SQL notebook.
2. Paste and run [flight_recorder_analytics.sql](flight_recorder_analytics.sql).
3. Upload only approved, non-sensitive development-history rows into `development_history`.
4. Run the final query and export its result locally.
5. Invoke the workflow with a transparent source label:

```bash
go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "your change" \
  --history /path/to/databricks-history-export.json \
  --history-source "Databricks Free Edition export — <date and query run>"
```

Databricks Free Edition supports notebooks, SQL analysis, and visualizations in a quota-limited serverless workspace. [Databricks Free Edition documentation](https://docs.databricks.com/aws/en/getting-started/free-edition)

## Data contract

The table deliberately stores only: anonymous session IDs, timestamps, touched file paths, test names/results, retry/revert counts, and a non-sensitive summary. Never upload source code, prompts, secrets, customer data, or personal data.
