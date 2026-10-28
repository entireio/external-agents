# Release Gate — Initial Understanding & Intended Architecture

> Checkpoint #1 content (Phase 1). Captures intent, architecture, assumptions,
> rejected options, and open risks *before* any feature code exists.

## One sentence

Release Gate turns every pull request into a Databricks-scored, evidence-backed
release-risk decision — using **Entire Checkpoints** for *why* the code changed
and **Entire Graph** for *what it structurally touches* — instead of a diff and a
vibe.

## The user and problem

A developer, tech lead, or merging agent reviewing a PR has no reliable signal
for "is this change riskier than it looks." Git diffs show *what* changed; CI
shows *whether tests passed*. Neither shows *why* a change was made, what the
author already ruled out, or which parts of the codebase are structurally
affected. Entire Checkpoints capture the "why"; Entire Graph captures the
"what's affected." Nobody has wired that evidence into a real go/no-go workflow.

## Why Entire is essential (not decorative)

The risk model's two most important features are uncomputable without Entire:
1. **Checkpoint unresolved-risk / open-question density** (from Checkpoints).
2. **Graph blast radius / relationship count** (from Entire Graph).

Remove Entire and the product degrades to a generic CI-risk scorer with no
access to intent or structural evidence.

## Intended architecture (medallion on Databricks Free Edition)

```
GitHub PR ──> Entire CI hook (Track integration)
   ├─ entire graph impact (changed files)     -> structural evidence (JSON)
   ├─ entire checkpoint list/explain (paths)   -> intent/risk evidence (JSON)
   └─ test runner results                      -> correctness evidence
        │  evidence bundle (versioned, no secrets) over HTTPS
        ▼
Databricks Free Edition
   [Bronze] raw_evidence  (append-only)
   [Silver] graph_impact | checkpoint_signals | test_results
   [Gold]   pr_risk_features -> pr_risk_scores
   MLflow-tracked model -> Model Serving endpoint (serverless, CPU)
   Databricks Workflow: ingest -> transform -> feature-build -> score+writeback
   Writeback -> GitHub PR comment + gate label
   Databricks App / Lakeview dashboard -> judge-facing evidence drill-down
```

## Key decisions

- **Thin end-to-end slice first** (Phase 2), then deepen. Highest-leverage
  choice for a one-day build.
- **Idempotency key = (pr.number, pr.revision_sha)** for `MERGE INTO` across all
  Delta layers, so re-runs never double-count.
- **Scoring decoupled from model**: heuristic scorer first, swap to the served
  model via registry without touching the orchestration DAG.
- **Evidence-bundle schema is versioned + additive** so the Noon Curveball can
  extend it without breaking ingestion.

## Rejected options (and why)

- **Fine-tuned LLM risk scorer** — rejected: Free Edition has no GPU/provisioned
  throughput; a CPU gradient-boosted model (LightGBM/sklearn or AutoML) fits the
  time and quota budget.
- **One DLT pipeline per table** — rejected: Free Edition allows one active
  pipeline per type; use a single Bronze→Silver pipeline (or scheduled job).
- **Per-PR fan-out tasks** — rejected: ≤5 concurrent job tasks; keep the
  Workflow to ≤4 sequential/parallel tasks.
- **Shelling out to `entire` from an unrelated app** — rejected: the track
  requires a genuine integration living inside the Entire-mirrored clone.

## Assumptions (Curveball content unknown at authoring time)

- Extension seams built so almost any curveball is absorbable without a rewrite:
  (a) evidence-bundle schema versioned/additive; (b) Gold feature build is a pure
  function of Silver tables; (c) scoring swappable via model registry; (d)
  writeback format is templated, not hard-coded per field.
- Entire Graph exact subcommand/output shape confirmed live via `entire graph
  --help` during the build, not guessed.
- Checkpoint risk extraction is heuristic (keyword/open-question density),
  documented as such — not semantic, for now.

## Open risks

- VS Code Copilot Chat is not a natively auto-tracked Entire agent; checkpoint
  capture may require `entire session attach` or committed-context fallback.
- Databricks Free Edition quota exhaustion can take compute offline for the day;
  build/verify the narrow path early and keep screenshot/recording fallbacks.
- Model trained on small synthetic seed data; treat scores as directional.

## Free Edition guardrails (design-to constraints)

1 serverless 2X-Small SQL warehouse · ≤5 concurrent job tasks · 1 active
pipeline per type · no GPU · ≤3 Apps (idle auto-stop) · 1 workspace/metastore.
