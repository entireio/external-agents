# Codebase Flight Recorder

Codebase Flight Recorder is a Codex workflow for Track 3. Before a significant change, it combines three evidence sources into a concise **Before You Code** briefing:

- Entire checkpoints: decisions, attempts, and unresolved work from prior sessions.
- Entire Graph: source search plus impact evidence for the requested change.
- A reviewed development-history export: optional historical failure, retry, and revert signals. This may originate in Databricks, but the command never fabricates history and labels its provenance.

Codex is already a native Entire agent, so this is deliberately not another `entire-agent-codex` adapter. The repository's existing Codex hooks create and preserve the resulting session/checkpoint; this workflow adds the missing pre-change risk-and-context step.

## Run

```bash
cd workflows/codex-flight-recorder
go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "Explain the impact of changing external-agent hook installation" \
  --files agents/entire-agent-kilo/internal/kilo/hooks.go
```

For optional historical evidence, pass either a reviewed JSON array with `files_touched`, `test_result`, `retries`, `revert_count`, and `summary` fields, or a JSONL lifecycle/transcript export:

```bash
go run ./cmd/codex-flight-recorder --repo ../.. --task "..." --history history-export.json
```

The JSONL Curveball format normalizes `session_started`, `file_changed`, `tool_result`, `checkpoint_created`, and `session_ended` into the same historical risk pipeline as the reviewed array. Unknown event types are listed as ignored instead of failing the run. A stream without `session_ended` is retained as `PARTIAL` evidence and emits a verification warning. JSONL `checkpoint_created` records are historical context only: the native Entire/Codex hooks remain responsible for creating real checkpoints.

Use `--format json` for a UI, CI, or Databricks consumer. The command reports unavailable Entire/Graph/history sources as warnings; it never substitutes example data.

For a real Databricks-backed export, use the [Databricks analytics notebook](databricks/README.md). Its computed `risk_score` is consumed by the briefing and increases the warning level for high-risk components.

For a concise judge-facing walkthrough and verification checklist, see [DEMO.md](DEMO.md).

### Local demo without Databricks

Use the committed fixture only for a demo; it is deliberately marked synthetic and must never be presented as real development history:

```bash
go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "Explain the impact of changing external-agent hook installation" \
  --files agents/entire-agent-kilo/internal/kilo/hooks.go \
  --history examples/synthetic-development-history.json \
  --history-source "SYNTHETIC DEMO DATA — not production or Databricks history"
```

## Codex workflow

1. Run the command before editing a high-impact area.
2. Verify Graph findings against the listed source and tests.
3. Give Codex the generated briefing with the implementation request.
4. Run the recommended tests, inspect `entire graph diff` for the final semantic evidence, and commit.
5. Let native Entire Codex hooks capture the session and resulting checkpoint.
