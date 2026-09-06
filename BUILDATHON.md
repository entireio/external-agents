# Release Gate

## One-sentence summary

Release Gate turns every pull request into a Databricks-scored, evidence-backed
release-risk decision — using **Entire Checkpoints** for *why* the code changed
and **Entire Graph** for *what it structurally touches* — surfaced through an
interactive dashboard and an AI-written review, instead of a diff and a vibe.

## Problem, intended user and why it matters

A developer, tech lead, or merging agent reviewing a PR has no reliable signal
for "is this change riskier than it looks." Git diffs show *what* changed; CI
shows *whether tests passed*. Neither shows *why* a change was made, what the
author already ruled out, or which parts of the codebase are structurally
affected. Reviewers re-derive this by hand. Release Gate wires that missing
evidence into an automated go/no-go decision posted straight onto the PR.

## What makes it different

- **The Curveball is part of the product, not a patch.** When the integrated
  agent's transcript format changed, one adaptive parser absorbed both formats at
  the ingestion boundary — no duplication, no downstream change (proven by
  `entire graph diff`), unknown events tolerated, incomplete input flagged partial.
- **A resumable development journey.** Four milestone checkpoints (initial →
  pre-noon → curveball → final); a fresh session literally reconstructs prior
  intent/decisions/risks via our own `entire release-gate handoff`.
- **A working, inspectable core.** A real PR is scored live with a posted gate
  comment + AI review, plus an interactive dashboard and 18 tests.
- **A credible continuation path.** Honest limitations and concrete next steps
  (LLM-graded risk classifier, real outcome data, drift monitoring).

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
entire release-gate dashboard --out dashboard.html --ai   # interactive HTML dashboard + AI review
entire release-gate info                       # plugin metadata
```

Plugin commands: `info | version | collect | score | handoff | ingest | dashboard`.

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
   MLflow model (databricks/ml/train_model.py) → Model Serving (graded P(incident))
     endpoint `release-gate-risk` (UC model `release_gate.gold.risk_model` v3, pyfunc
     DataFrame signature), READY; `score --use-endpoint` returns a real 0–1 score
     (model=served:release-gate-risk), heuristic scorer as automatic fallback
   Foundation Model (Llama 3.3 70B, release_gate/ai.py) → AI risk narrative (heuristic fallback)
   Databricks Workflow (databricks/bundle/databricks.yml): ingest→transform→features→score
   Writeback (integration/writeback/github_writeback.py) → PR comment + gate
   Databricks App (databricks/app) / SQL dashboard (databricks/sql) → judge-facing UI
   entire release-gate dashboard (release_gate/dashboard.py) → self-contained interactive
     HTML: risk gauge, Entire-Graph blast-radius network (vis-network), evidence cards,
     top risk drivers, checkpoint open-questions, and the AI review
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

## Live demo

Release Gate scores itself, live, on a real GitHub PR:
[`Yashas14/external-agents#1`](https://github.com/Yashas14/external-agents/pull/1).
`entire release-gate score` collected real Entire Graph + Checkpoint evidence
for that PR, computed a **blast radius of 60 impacted symbols across 22 files**,
landed a **REVIEW** gate, and posted the gate comment (evidence table + gate)
back onto the PR via `integration/writeback/github_writeback.py`, with the
Databricks Llama 3.3 70B AI review appended underneath. The same bundle also
renders via `entire release-gate dashboard` into the interactive HTML dashboard
described above.

## Noon Curveball: what changed and how we adapted

**Constraint (Track 3):** the official Track-3 fixture
(`release-gate/seed_data/track-3-agent-session.jsonl`) is an AcmeCode
agent-session **transcript** in a **new**, `event`-named format
(`session_started`, `user_prompt`, `agent_response`, `tool_call`,
`tool_result`, `file_read`, `file_changed`, `usage`, `checkpoint_created`,
`session_ended`); existing users still emit the **original**, `type`-named
format (`start`/`edit`/`test`/`checkpoint`/`end`). We must support both,
never crash on unknown events, produce **partial** (not corrupted) results
on incomplete input, and preserve existing behaviour — without duplicating
the implementation.

- **Invalidated assumption:** the workflow emits evidence/lifecycle events in
  a *single fixed format*.
- **Fresh session + reconstruction:** a new agent session reconstructed prior
  intent/risks by running our own `entire release-gate handoff` plus the pre-noon
  checkpoint doc.
- **Graph impact (before editing):** `entire graph impact --symbol build_bundle`
  showed `build_bundle` is the single assembly boundary and all downstream
  (`to_silver → features → scoring → writeback`, plus `handoff`) consumes the
  **normalized bundle** — so we adapt at the *parse boundary* and keep the bundle
  contract stable. No downstream change, no duplication.
- **Focused revision:** new `release_gate/events.py` — one version-detecting
  parser normalizes **both** formats into the same internal model;
  `events_to_bundle` builds the existing bundle. Churn comes from
  `file_changed.lines_added`/`lines_removed` (new) or `edit.added`/`removed`
  (original); unresolved risks come from `checkpoint_created.open_questions`
  (new) or `checkpoint.risks` (original); test counts come from correlating
  `tool_call` npm-test commands to their `tool_result` summary (new) versus a
  direct `test` event (original). Known-but-no-evidence events (`user_prompt`,
  `agent_response`, `file_read`, `usage`) are skipped; unknown events are
  counted (not fatal); corrupt/truncated lines tolerated; incomplete input
  yields `ingest.partial = true`, surfaced as a **PARTIAL banner** in the PR
  comment so incomplete context is never shown as authoritative. New
  `entire release-gate ingest --events <jsonl>` subcommand.
- **Tests:** `tests/test_events.py` covers **new (official fixture), original,
  unknown, incomplete**; existing 11 tests still pass (**17 at this checkpoint;
  18 now** after the later dashboard suite).
- **Why safe:** existing collection path unchanged; downstream
  (`to_silver`/`features`/`scoring`) untouched, verified via `entire graph diff`
  semantic-diff; partial results explicitly flagged. Checkpoint: `afab2f277342`
  (commit `ae1cc09`).

## Checkpoint links and what each checkpoint proves

Checkpoints are captured by attaching the driving Copilot CLI session to each
milestone commit (`entire session attach … -a copilot-cli`) and are synced to
the Entire mirror. The four required milestones:

- **Initial understanding & architecture** — fork checkpoint `f23050d26bfc`
  (commit `a6bd301`): the native `entire release-gate` plugin design; companion
  repo `01M1TGFPESE5NSHXPWD2M1DYWV` has the original skeleton + schema.
- **Pre-noon / pre-curveball stable** — `release-gate/docs/architecture/02-pre-noon-stable.md`
  (fork commit `1249061`) with as-built architecture + reconstruction notes;
  companion checkpoint `01M1TJZM9XDX6J992D523TRQS8` proves a fresh session can
  resume the project cold.
- **Curveball response** — fork checkpoint `afab2f277342` (commit `ae1cc09`):
  dual-format lifecycle-event ingestion; proves the process absorbed a real
  format change at the parse boundary without duplication.
- **Final implementation & verification** — fork checkpoint `8a8dcab6352c`
  (commit `5e74350`): final semantic-diff + full verification (18 tests, live
  Databricks medallion, graded Model Serving endpoint). Later enhancement
  checkpoints (`181d1631dc8b`, `98b06c82e857`) add the AI review, interactive
  dashboard, live PR demo, and graded-probability serving.

The companion clone `Yashas14/Buildathon_TriNexus` holds the full commit-by-commit
build history behind this consolidated fork submission.

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
entire release-gate score --use-endpoint --ai --base <sha> --pr-number 1 --pr-repo owner/name
entire release-gate dashboard --ai --out dashboard.html --base <sha> --pr-number 1 --pr-repo owner/name

# Run the whole slice locally (no Databricks/quota needed):
python scripts/run_local_slice.py                 # scores the sample bundle

# Produce a REAL evidence bundle from live Entire Graph + Checkpoints:
python integration/ci_hook/collect_evidence.py --repo . --head HEAD --base <base> \
    --pr-number 1 --pr-repo Yashas14/Buildathon_TriNexus --run-tests --out bundle.json
python scripts/run_local_slice.py bundle.json

# Preview the PR comment (no token needed):
python integration/writeback/github_writeback.py --bundle bundle.json --dry-run

# Tests:
python -m pytest -q                               # 18 passing

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
- **Model Serving** (serverless): a `release-gate-risk` endpoint (UC model
  `release_gate.gold.risk_model` v3, a pyfunc with a DataFrame signature) is
  deployed and **READY**, serving a **graded P(incident)**; `score
  --use-endpoint` / `dashboard --use-endpoint` call it live and return a real
  0–1 score (`model=served:release-gate-risk`), with the heuristic scorer as an
  automatic fallback (also used by `score_and_writeback.py`).
- **Foundation Models (Llama 3.3 70B) for the AI review** (`release_gate/ai.py`):
  a serverless Databricks-hosted foundation-model endpoint
  (`databricks-meta-llama-3-3-70b-instruct`) turns the evidence bundle into a
  short verdict/why/check narrative appended to the gate comment and the
  dashboard; heuristic-free text is skipped (fails soft) if unreachable.
- **Databricks Workflows / Asset Bundles** (`databricks/bundle/databricks.yml`):
  a single 4-task Workflow, tagged `project=release-gate`.
- **Databricks App** (`databricks/app`, Streamlit) — **deployed and running**, a
  dynamic judge-facing dashboard that reads the **live** Gold tables (all scored
  PRs, KPIs, and a per-PR evidence drill-down); the **Lakeview/SQL dashboard**
  (`databricks/sql/dashboard_queries.sql`) is the fallback surface.

**Free Edition guardrails respected:** one serverless SQL warehouse; ≤5
concurrent job tasks (we use 4 sequential); one pipeline per type; no GPU
(CPU model); ≤3 Apps (we use 1); one workspace/metastore.

**Verified live state (at submission):** live and inspectable in the workspace —
Bronze `release_gate.bronze.raw_evidence`, Silver `release_gate.silver.graph_impact`,
Gold `release_gate.gold.pr_risk_features` + `release_gate.gold.pr_risk_scores`
(populated); the `release-gate-risk` serving endpoint is **READY on v3** and
returns a graded score end-to-end; the Llama 3.3 70B review endpoint responds.
The remaining Silver tables (`checkpoint_signals`, `test_results`) are defined by
the medallion jobs and materialize when the full Workflow runs — the demo load
populated the representative subset above. The **Streamlit Databricks App is
deployed and running** (serverless; idle auto-stops on Free Edition) as the
dynamic dashboard over these live tables.

**Data provenance:** `seed_data/pr_history.jsonl` is **100% synthetic and clearly
labeled** (generated by `seed_data/generate_seed.py`, 500 rows, thresholded
labels with 12% flip noise), used only for cold-start model training. Live
evidence comes from this repo's own PRs, checkpoints, and graph output during
the event. No secrets are stored anywhere; tokens are read from the environment
/ Databricks secret scopes only.

**How the Curveball affected the Databricks workflow:** the new transcript format
is normalized at the parse boundary (`release_gate/events.py`) into the *same*
evidence bundle, so Bronze → Silver → Gold → Model Serving score it **unchanged**
— no pipeline, schema, or job edit. A partial/incomplete transcript lands as a
**flagged partial** row (`ingest.partial = true`), never a corrupted or dropped
one, so the lakehouse never presents incomplete evidence as authoritative.

## Provenance & attribution

- **Prior work forked:** `entireio/external-agents` (the Track 3 repo, a Go
  agent-integration codebase). Release Gate is added under `release-gate/`; the
  upstream Go agents are untouched.
- **AI-generated components:** application code was authored by GitHub Copilot
  CLI / Claude agent sessions (captured verbatim in the Entire checkpoints); the
  runtime AI review is generated by the Databricks-hosted **Llama 3.3 70B**
  foundation model. All generated code was reviewed, tested, and is team-owned.
- **External services:** Databricks Free Edition (Delta, MLflow, Unity Catalog,
  Model Serving, Foundation Models), the GitHub REST API (PR comment), and the
  Entire mirror (India region).
- **Open-source dependencies:** `jsonschema`, `requests`, `pytest`,
  `scikit-learn`, `mlflow`, `databricks-sdk`, and `vis-network` (via CDN).
- **Synthetic data:** `seed_data/pr_history.jsonl` and the transcript fixtures
  under `seed_data/`, all clearly labeled synthetic.

## Known limitations and next steps

- **Checkpoint risk extraction is heuristic** (keyword/open-question matching),
  not a semantic classifier — next step: an LLM-graded checkpoint classifier.
- **Model is trained on synthetic seed data**; treat scores as directional until
  real historical PR outcomes are available.
- **Model Serving returns graded probabilities.** The `release-gate-risk`
  endpoint (Unity Catalog model `release_gate.gold.risk_model`, pyfunc v3 with a
  DataFrame signature) serves a graded P(incident); `entire release-gate score
  --use-endpoint` returns `model=served:release-gate-risk` with a real 0–1 score.
  The deterministic heuristic remains the default and the fallback, so the gate
  stays reliable if the endpoint is scaled to zero or unavailable.
- **Copilot CLI hooks do not auto-capture** sessions in non-interactive mode, so
  checkpoints are created by attaching the driving session — reliable, but a step
  we perform explicitly.
- **Monitoring is minimal**; next step is Lakehouse Monitoring drift alerts on
  the feature distributions vs. training time.
