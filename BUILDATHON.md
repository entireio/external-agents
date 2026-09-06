# Release Gate

## One-sentence summary

Release Gate turns every pull request into a Databricks-scored, evidence-backed
release-risk decision — using **Entire Checkpoints** for *why* the code changed
and **Entire Graph** for *what it structurally touches* — instead of a diff and a
vibe.

## Problem, intended user and why it matters

A developer, tech lead, or merging agent reviewing a PR has no reliable signal
for "is this change riskier than it looks." Git diffs show *what* changed; CI
shows *whether tests passed*. Neither shows *why* a change was made, what the
author already ruled out, or which parts of the codebase are structurally
affected. Reviewers re-derive this by hand. Release Gate wires that missing
evidence into an automated go/no-go decision posted straight onto the PR.

## Selected Entire track and why Entire is essential

**Track 3 — Bring Entire to a New Agent or Workflow** (a CI / pull-request
workflow that uses checkpoint context). The integration is a **native Entire CLI
plugin**: an `entire-release-gate` executable that the Entire CLI discovers on
PATH and exposes as **`entire release-gate`** (exactly like `entire graph`). So
Release Gate *is* an Entire command — it does not merely call `entire` from an
unrelated app, which the guide states "is not sufficient by itself."

```
entire release-gate score --base <sha> --pr-number 1 --pr-repo owner/name --run-tests
entire release-gate collect > bundle.json     # emit the evidence bundle
entire release-gate info                       # plugin metadata
```

(For Track 3 the designated fork is `entireio/external-agents`, which hosts
`entire-agent-<name>` *session-capture* binaries; Release Gate is a CI/PR
workflow, whose correct Entire extension type is a **CLI plugin** `entire-<name>`
per the Agent Integration Protocol docs. The plugin lives in this Entire-mirrored
clone.)

Entire is essential, not decorative: the risk model's two most important
features are **uncomputable without Entire** —

1. **Unresolved-risk density** from **Entire Checkpoints** (`entire checkpoint
   list/explain`) — the "why / what's still open."
2. **Structural blast radius** from **Entire Graph** (`entire graph diff/impact`)
   — the "what this change actually touches."

Remove Entire and the product degrades to a generic CI-risk scorer with no
access to intent or structural evidence.

## Architecture and main workflow

```
GitHub PR ──> Entire CI hook  (integration/ci_hook/collect_evidence.py)
   ├─ entire graph diff --base --head   → changed symbols (PR-wide)
   ├─ entire graph impact --symbol …    → structural blast radius
   ├─ entire checkpoint list/explain    → intent + unresolved-risk signals
   └─ pytest                            → correctness + impacted-symbol coverage
        │  versioned evidence bundle (evidence_schemas/evidence_bundle.schema.json)
        ▼
Databricks Free Edition  (databricks/jobs/*.py, medallion Delta)
   [Bronze] raw_evidence      MERGE on bundle_id (append-only, audit)
   [Silver] graph_impact | checkpoint_signals | test_results
   [Gold]   pr_risk_features  →  pr_risk_scores   (MERGE on pr+sha,
                                 CHECK risk_score BETWEEN 0 AND 1, OPTIMIZE ZORDER)
   MLflow model (databricks/ml/train_model.py) → Model Serving (heuristic fallback)
   Databricks Workflow (databricks/bundle/databricks.yml): ingest→transform→features→score
   Writeback (integration/writeback/github_writeback.py) → PR comment + gate
   Databricks App (databricks/app) / SQL dashboard (databricks/sql) → judge-facing UI
```

The pure scoring logic lives in `release_gate/` (bundle → silver → features →
scoring → writeback) and is imported **unchanged** by both the local runner
(`scripts/run_local_slice.py`) and the Databricks Spark jobs, so the exact same
logic is unit-tested locally and executed on the lakehouse.

## Entire Graph findings and verification

- **Definition lookup:** `entire graph def build_gold_features` → confirmed the
  function signature/location before building features on it.
- **Impact analysis (before a high-risk change):** `entire graph impact --symbol
  score_features` reported **8 callers** (5 direct incl. `run`, plus 4 test
  functions; 3 transitive) and **1 callee** (`_cap`). Verified against source in
  `release_gate/scoring.py` and `tests/test_scoring.py` — the callers match the
  real call sites, so it is safe to change the scorer's internals as long as the
  input/output contract holds.
- **PR-wide impact for scoring:** on the current branch the collector measured a
  real **blast radius of 47 impacted symbols across 10 files (max depth 2)**,
  which drove a **REVIEW** gate at risk 40/100.
- **Final semantic-diff of the submission:** recorded via `entire graph diff
  --base <base> --head <head>` (entity-level change list) at submission time.

Graph output is treated as evidence, not an oracle: findings were spot-checked
against source and tests, and every evidence block in the bundle carries an
`available` flag so a graph/checkpoint failure degrades to an "evidence
unavailable" state instead of crashing CI.

## Noon Curveball: what changed and how we adapted

_To be completed when the official Curveball is received at 12:00 noon._ The
build was designed with four extension seams so the constraint can be absorbed
without a rewrite: (a) the evidence-bundle schema is versioned and additive;
(b) Gold features are a pure function of Silver; (c) scoring is decoupled from
the model via the registry; (d) the PR-comment writeback is data-driven.

## Checkpoint links and what each checkpoint proves

Checkpoints are captured by attaching the driving Copilot CLI session to each
milestone commit (`entire session attach … -a copilot-cli`) and are synced to
the Entire mirror.

- **Checkpoint 1 — Initial architecture** (`01M1TGFPESE5NSHXPWD2M1DYWV`): repo
  skeleton, evidence-bundle schema, and Entire+Graph setup; proves the intended
  design and the decisions/rejected options behind it.
- **Checkpoint 2 — Pre-noon stable** (`01M1TJZM9XDX6J992D523TRQS8`): as-built
  architecture and reconstruction notes; proves a fresh session can resume the
  project cold.
- **Checkpoint 3 — Curveball response** (_pending noon_).
- **Checkpoint 4 — Final implementation & verification** (_pending_).

## Setup, run and test instructions

> **Repository layout:** this is a fork of `entireio/external-agents` (the Track 3
> designated repo). The Release Gate implementation lives under **`release-gate/`**;
> run the commands below from that directory.

```bash
# From a clean checkout (Python 3.12):
cd release-gate
pip install -r requirements.txt

# Install Release Gate as a native Entire CLI plugin:
pip install -e .                                  # builds entire-release-gate
entire plugin install "$(python -c 'import shutil;print(shutil.which("entire-release-gate"))')"
entire release-gate score --base <sha> --pr-number 1 --pr-repo owner/name --run-tests

# Run the whole slice locally (no Databricks/quota needed):
python scripts/run_local_slice.py                 # scores the sample bundle

# Produce a REAL evidence bundle from live Entire Graph + Checkpoints:
python integration/ci_hook/collect_evidence.py --repo . --head HEAD --base <base> \
    --pr-number 1 --pr-repo Yashas14/Buildathon_TriNexus --run-tests --out bundle.json
python scripts/run_local_slice.py bundle.json

# Preview the PR comment (no token needed):
python integration/writeback/github_writeback.py --bundle bundle.json --dry-run

# Tests:
python -m pytest -q                               # 9 passing

# Train the risk model (MLflow-tracked):
python databricks/ml/train_model.py               # cv_auc ~0.85

# Databricks deploy (needs `databricks auth login`):
databricks bundle deploy -t dev -p release-gate
databricks bundle run release_gate_pipeline -t dev -p release-gate
```

## Databricks use, data sources and limitations

**Capabilities used (each essential to the core workflow):**
- **Delta Lake medallion** (`databricks/jobs/*.py`): Bronze `raw_evidence`
  (audit), Silver typed tables, Gold `pr_risk_features` + `pr_risk_scores`. Uses
  `MERGE INTO` keyed on `(pr_number, revision_sha)` for idempotent re-scoring,
  a `CHECK (risk_score BETWEEN 0 AND 1)` constraint, and `OPTIMIZE … ZORDER`.
- **MLflow tracking + Model Registry** (`databricks/ml/train_model.py`):
  CPU-friendly gradient-boosted model, logged params/metrics
  (cv_auc ≈ 0.85, holdout_auc ≈ 0.82).
- **Model Serving** (serverless): `score_and_writeback.py` calls the endpoint
  when configured, with the heuristic scorer as an automatic fallback.
- **Databricks Workflows / Asset Bundles** (`databricks/bundle/databricks.yml`):
  a single 4-task Workflow, tagged `project=release-gate`.
- **Databricks App** (`databricks/app`) or **Lakeview/SQL dashboard**
  (`databricks/sql/dashboard_queries.sql`) as the judge-facing surface.

**Free Edition guardrails respected:** one serverless SQL warehouse; ≤5
concurrent job tasks (we use 4 sequential); one pipeline per type; no GPU
(CPU model); ≤3 Apps (we use 1); one workspace/metastore.

**Data provenance:** `seed_data/pr_history.jsonl` is **100% synthetic and clearly
labeled** (generated by `seed_data/generate_seed.py`, 500 rows, thresholded
labels with 12% flip noise), used only for cold-start model training. Live
evidence comes from this repo's own PRs, checkpoints, and graph output during
the event. No secrets are stored anywhere; tokens are read from the environment
/ Databricks secret scopes only.

## Known limitations and next steps

- **Checkpoint risk extraction is heuristic** (keyword/open-question matching),
  not a semantic classifier — next step: an LLM-graded checkpoint classifier.
- **Model is trained on synthetic seed data**; treat scores as directional until
  real historical PR outcomes are available.
- **Copilot CLI hooks do not auto-capture** sessions in non-interactive mode, so
  checkpoints are created by attaching the driving session — reliable, but a step
  we perform explicitly.
- **Monitoring is minimal**; next step is Lakehouse Monitoring drift alerts on
  the feature distributions vs. training time.
