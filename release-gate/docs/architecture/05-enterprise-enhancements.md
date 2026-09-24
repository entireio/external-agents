# Release Gate — Enterprise Enhancements (Final Polish)

> Checkpoint #5. Documents the post-curveball enhancement round on top of
> `04-final-verification.md`: an AI risk narrative, an interactive dashboard,
> an agent-handoff bridge, Model Serving status, a live self-scored demo PR,
> and the provenance/attribution updates to `BUILDATHON.md`.

## 1. AI risk narrative (`release_gate/ai.py`)

- `risk_narrative()` calls a **Databricks-hosted foundation model**,
  `databricks-meta-llama-3-3-70b-instruct` (Llama 3.3 70B), via
  `WorkspaceClient.api_client.do(... /serving-endpoints/{model}/invocations)`.
- The system prompt treats the gate decision (`PASS`/`REVIEW`/`BLOCK`) as
  **final and authoritative** — the model explains the verdict, it never
  overrides it — and returns a fixed `Verdict / Why / Check` structure built
  from the evidence bundle's blast radius, unresolved checkpoint risks, test
  results, and churn.
- **Fails soft**: any exception (auth, network, missing SDK) is caught and
  `None` is returned, so the gate, writeback, and dashboard all continue to
  work with a heuristic-only review when the endpoint is unreachable.

## 2. Interactive self-contained HTML dashboard (`release_gate/dashboard.py`)

- `render_dashboard()` produces a single offline-capable HTML page — no
  server, no build step — with:
  - a **risk gauge** and gate badge (color-coded PASS/REVIEW/BLOCK),
  - an **Entire-Graph blast-radius network** rendered with `vis-network`
    (loaded from a CDN), nodes colored/sized by impact distance from the PR
    change,
  - **evidence cards**, **top risk drivers** (from `score["top_factors"]`),
    **open questions** (unresolved checkpoint risks), and the **AI review**
    narrative from `ai.py`.
- Exposed via the CLI as `entire release-gate dashboard` (writes to
  `--out`, default `release_gate_dashboard.html`).

## 3. Agent handoff bridge (`entire release-gate handoff`)

- `handoff.py::build_handoff` / `render_handoff` reconstruct prior intent,
  decisions, and open risks from the Entire checkpoint history so a fresh
  agent session can resume the project cold — the same mechanism used
  across the project's own milestone checkpoints.

## 4. Model Serving status

- The Unity Catalog model `release_gate.gold.risk_model` is deployed to the
  serving endpoint `release-gate-risk`, state **READY**.
- `plugin.py::_score_via_endpoint` will call this endpoint, but the
  **heuristic scorer (`scoring.py`) is the graded primary path**; the
  endpoint is an opportunistic enhancement with fallback to the heuristic
  on any failure.
- **Documented next step**: the endpoint currently returns a binary class
  mapped to a representative score. Serving a genuine graded pyfunc
  probability (not just a class label) is not yet done and is called out
  here as future work rather than claimed as complete.

## 5. Live demo PR

- Release Gate scored its **own** demo pull request end-to-end:
  <https://github.com/Yashas14/external-agents/pull/1>.
- Result: gate **REVIEW**, blast radius **60 symbols across 22 files**,
  with an AI review narrative attached — evidence that collection, graph
  impact, scoring, writeback, and the AI narrative all work together on a
  real PR, not just seed data.

## 6. Provenance & attribution

- `BUILDATHON.md` now documents **provenance and attribution**: which parts
  are human/agent-authored vs. AI-generated (the runtime review text is
  produced by the Databricks-hosted Llama 3.3 70B foundation model, with
  all generated code reviewed/tested and team-owned), the external services
  used, and the open-source dependencies (including `vis-network` via CDN).
- The explicit **Databricks-curveball note** was added: the new transcript
  format is normalized at the parse boundary (`release_gate/events.py`)
  into the same shape consumed by the existing Databricks medallion jobs,
  so the curveball required no changes to the Bronze/Silver/Gold pipeline.

## 7. Verification

- **Final semantic-diff**: `entire graph diff` over this enhancement round
  shows **only additive changes**:
  - **New files**: `release_gate/ai.py`, `release_gate/dashboard.py`.
  - **Body-changed only** (no signature changes): `plugin.py::main`,
    `plugin.py::_info`, `plugin.py::_score_via_endpoint`.
  - **No existing function signatures changed** anywhere else in the
    codebase — the enhancement round is a pure addition on top of the
    curveball-response baseline.
- **Tests**: `python -m pytest -q` → **18 passed** (the 16 from
  `04-final-verification.md` plus 2 new dashboard tests in
  `test_dashboard.py`).
