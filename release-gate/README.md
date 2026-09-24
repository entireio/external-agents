# Release Gate — `entire release-gate`

Track 3 integration for the Entire CLI, built as a **native Entire plugin**.
It ships an `entire-release-gate` executable that the Entire CLI discovers on
`PATH` and exposes as `entire release-gate`, exactly like a built-in command
(e.g. `entire graph`). This is **not** a standalone app that shells out to
`entire` — it is a first-class Entire subcommand that reuses Entire's own
data (Graph + Checkpoints) in-process.

## Install

```bash
cd release-gate
pip install -e .
entire plugin install "$(command -v entire-release-gate)"

entire release-gate score \
  --base <sha> --pr-number <N> --pr-repo owner/name --run-tests
```

`pip install -e .` registers the `entire-release-gate` console script
(see `pyproject.toml`); `entire plugin install` tells the Entire CLI to
discover and wire it up as `entire release-gate`.

## Architecture

```
Entire Graph (blast radius)  ┐
Entire Checkpoints (unresolved-risk) ┘→ versioned evidence bundle (JSON, schema-validated)
        → Databricks medallion Delta tables
              bronze → silver → gold   (MERGE keyed on pr_number + sha)
        → MLflow model (with a heuristic fallback when no model is served)
        → PR comment + pass/warn/block gate
```

- **Evidence bundle**: `release_gate/collect.py` pulls blast-radius signal
  from Entire Graph and unresolved-risk signal from Entire Checkpoints,
  validated against `evidence_schemas/evidence_bundle.schema.json`.
- **Medallion Delta**: `databricks/jobs/*.py` ingest bronze, transform silver,
  and build gold features; writes `MERGE` on `(pr_number, sha)` for idempotent
  re-runs.
- **Scoring**: `release_gate/scoring.py` calls an MLflow-served model and
  falls back to a deterministic heuristic when no model endpoint is
  available, so `score` always produces a result.
- **Gate**: `release_gate/writeback.py` renders the PR comment and the CLI
  exits non-zero on a hard `BLOCK` so CI can fail the check.

## Subcommands

| Command | Purpose |
|---|---|
| `entire release-gate info` | Plugin metadata (JSON) |
| `entire release-gate version` | Plugin version |
| `entire release-gate collect` | Build and print the evidence bundle (JSON) |
| `entire release-gate score` | collect → score → print the gate + PR comment |

## Build history

The full build history and the four milestone checkpoints for this
integration also live in the companion mirror repo, alongside this fork.
