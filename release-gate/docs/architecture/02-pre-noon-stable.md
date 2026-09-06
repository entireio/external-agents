# Release Gate — Pre-Noon Stable State

> Checkpoint #2 content. Captures the last known-good, fully-verified state of
> the project immediately *before* the Noon Curveball is revealed. Written so a
> fresh agent session (with no memory of this one) can reconstruct intent,
> as-built structure, what's done, what's not, and where the deliberate
> extension seams are — without re-deriving any of it from scratch.

## 1. Current intent

Release Gate turns every pull request into a **Databricks-scored,
evidence-backed release-risk decision**, using:

- **Entire Checkpoints** for *why* the code changed (intent, ruled-out
  options, open questions/unresolved risk density), and
- **Entire Graph** for *what it structurally touches* (blast radius, impacted
  symbols, call/inheritance relationships of the diff).

Neither signal is derivable from a plain git diff or a green CI run. Combining
them into features for a trained risk model — with a deterministic heuristic
fallback — is the product's core value proposition. This intent is unchanged
from checkpoint 1 (`00-initial-understanding.md`); this document records what
has actually been built against that intent, not a revision of the intent
itself.

## 2. As-built architecture

```
GitHub PR / local diff
        │
        ▼
integration/ci_hook/collect_evidence.py   (Entire CI hook)
   ├─ `entire graph diff` + `entire graph impact`   -> structural evidence
   ├─ `entire checkpoint list` + `entire checkpoint explain` -> intent evidence
   └─ pytest run                                     -> correctness evidence
        │
        ▼  versioned evidence bundle (JSON), validated against
        │  evidence_schemas/evidence_bundle.schema.json
        ▼
release_gate/  (pure, framework-agnostic Python package)
   ├─ bundle.py    — load/validate evidence bundle against schema
   ├─ silver.py    — normalize bundle into silver-shaped records
   ├─ features.py  — pure function: silver records -> gold feature vector
   ├─ scoring.py   — heuristic-v1 scorer (+ model-registry seam, see §6)
   ├─ model.py     — shared feature contract used by both heuristic and
   │                 trained model paths, so features never drift between them
   └─ writeback.py — template-driven PR comment / gate label renderer
        │
        ├── consumed directly by scripts/run_local_slice.py
        │   (local, no-Databricks end-to-end runner used for dev + demo)
        │
        └── consumed by databricks/jobs/*.py (Spark medallion, same logic)
                 ├─ ingest_bronze.py        -> raw_evidence (append-only,
                 │                            MERGE keyed on bundle_id)
                 ├─ transform_silver.py     -> graph_impact, checkpoint_signals,
                 │                            test_results
                 ├─ build_gold_features.py  -> pr_risk_features
                 └─ score_and_writeback.py  -> pr_risk_scores
                                               (MERGE keyed on pr + sha,
                                                CHECK risk BETWEEN 0 AND 1)
                                               + GitHub PR writeback via
                                               integration/writeback/github_writeback.py

databricks/ml/train_model.py
   — sklearn GradientBoostingClassifier trained on gold features + synthetic
     seed labels, tracked via MLflow (cv_auc ≈ 0.85 on synthetic seed data).
     Not yet wired to a Model Serving endpoint (see §4).

databricks/bundle/databricks.yml
   — Databricks Asset Bundle wiring the medallion pipeline job and the
     train_model job as deployable, versioned jobs.

databricks/app/  (Streamlit)  + databricks/sql/dashboard_queries.sql
   — judge-facing evidence drill-down; the SQL file is a fallback view of the
     same gold tables in case the App isn't deployed/reachable.
```

Key structural properties preserved from intent:
- `release_gate/` has zero Databricks/Spark imports — it is the single source
  of truth for bundle→silver→features→score→writeback logic, unit-tested in
  isolation and reused verbatim by both the local runner and the Spark jobs.
- Idempotency key `(pr.number, pr.revision_sha)` is used for every `MERGE
  INTO` across bronze/gold layers (bronze keys on `bundle_id`, gold keys on
  `pr` + `sha`), so re-running the pipeline never double-counts.
- `pr_risk_scores.risk` has a `CHECK` constraint enforcing `0 <= risk <= 1`.

## 3. Completed work

- **Phase 1** — initial understanding/architecture doc, evidence-bundle
  schema, repo scaffold, Entire+Graph setup.
- **Phase 2** — thin end-to-end slice: evidence collector, medallion core
  (bronze/silver/gold), heuristic-v1 scorer, GitHub PR writeback, first tests.
- **Phase 3** — PR-wide `entire graph diff`, real impacted-symbol test
  coverage (not mocked), synthetic seed data for model training.
- **Phase 6** — MLflow-tracked risk model (sklearn GBT), shared feature
  contract (`release_gate/model.py`) guaranteeing heuristic and trained-model
  paths consume identical features.
- **Phase 7/8** — Databricks Asset Bundle (`databricks/bundle/databricks.yml`)
  wiring pipeline + train jobs; Streamlit app (`databricks/app/`); SQL
  dashboard fallback (`databricks/sql/dashboard_queries.sql`).
- **Tests**: `pytest -q` → **9 passed**, 0 failed (covers scoring, features,
  and the local end-to-end slice).
- **Real end-to-end verification (not mocked)**: running the live Entire
  Graph against this repo's actual diff produced a blast radius of **47
  impacted symbols**, which fed through the real feature/scoring path and
  correctly resolved to a **REVIEW** gate decision — confirming the pipeline
  reacts to genuine graph evidence, not fixture data.

## 4. Unresolved work

- **Live Databricks deploy is pending OAuth.** The Asset Bundle
  (`databricks/bundle/databricks.yml`) and jobs are written and locally
  validated, but have not been deployed to a live Databricks Free Edition
  workspace because OAuth authentication to the workspace is not yet
  completed in this environment.
- **No Model Serving endpoint deployed.** `databricks/ml/train_model.py`
  trains and logs the model to MLflow, but scoring in the medallion jobs
  currently runs through `release_gate/scoring.py`'s heuristic-v1 path, not a
  served model — the swap-in seam exists (see §6) but hasn't been exercised
  against a live endpoint.
- **Checkpoint risk extraction is heuristic, not semantic.** Intent/risk
  signal from `entire checkpoint explain` is currently derived via
  keyword/open-question density matching, not embedding- or LLM-based
  semantic understanding of checkpoint content. This is a known, documented
  simplification, not an oversight.

## 5. Named technical risks

- **Copilot CLI hooks don't auto-capture checkpoints.** Unlike natively
  auto-tracked Entire agents, this environment requires attaching sessions to
  Entire manually (e.g. `entire session attach` / equivalent) rather than
  relying on automatic hook-based capture — a process risk if a session is
  forgotten before checkpointing.
- **Databricks Free Edition quota constraints.** 1 serverless 2X-Small SQL
  warehouse, ≤5 concurrent job tasks, 1 active pipeline per type, no GPU, ≤3
  Apps with idle auto-stop, 1 workspace/metastore. Quota exhaustion can take
  compute offline for the remainder of the day; this shaped every design
  decision (single sequential job DAG, CPU-only sklearn model, one pipeline).
- **Model trained on synthetic seed data only.** `cv_auc ≈ 0.85` is measured
  against `seed_data/`, not real historical PR outcomes. Scores should be
  treated as directional/demo-quality, not production-calibrated, until
  retrained on real labeled data.

## 6. Extension seams for the Curveball

These were deliberately built into the architecture *before* the curveball
content was known, specifically so it can be absorbed without a rewrite:

- **Versioned, additive evidence-bundle schema**
  (`evidence_schemas/evidence_bundle.schema.json`). New evidence fields can be
  added without breaking existing bronze ingestion or older bundle producers.
- **Gold features are a pure function of silver.** `release_gate/features.py`
  takes normalized silver records in and returns a feature vector out, with no
  side effects and no Spark/Databricks dependency — new features or a new
  evidence source can be added by extending this function and its unit tests,
  independent of the pipeline plumbing.
- **Scoring is decoupled from the model via a registry-style seam.**
  `release_gate/scoring.py` (heuristic-v1) and `databricks/ml/train_model.py`
  (trained model) both consume the same feature contract
  (`release_gate/model.py`); swapping the heuristic for the served model is a
  change to *which scorer is called*, not to the orchestration DAG,
  feature-build jobs, or writeback logic.
- **Writeback is data-driven, not hard-coded per field.**
  `release_gate/writeback.py` / `integration/writeback/github_writeback.py`
  render the PR comment and gate label from a template plus the score/feature
  payload, so new fields surfaced by a curveball extension can appear in the
  writeback without editing string-formatting logic per field.

## How to reconstruct this state from scratch

1. `pytest -q` from repo root should show `9 passed`.
2. `release_gate/` has no Databricks imports — verify with
   `entire graph search` or a grep for `pyspark`/`databricks` inside
   `release_gate/`; it should return nothing.
3. `scripts/run_local_slice.py` runs the same bundle→silver→features→score→
   writeback path as the Databricks jobs, without needing a workspace —
   this is the fastest way to smoke-test the whole pipeline offline.
4. `databricks/bundle/databricks.yml` is written and structurally valid but
   **not deployed** — do not assume a live workspace exists; OAuth to
   Databricks is the blocking step for any live-deploy verification.
