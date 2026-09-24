# Release Gate — Final Verification (Track 3)

> Checkpoint #4 (final). Documents the submitted implementation and its
> verification, closing out the curveball response in
> `03-curveball-response.md`.

## 1. Final semantic-diff

`entire graph diff --base 1249061 --head HEAD` shows the curveball was a
**focused, additive revision**:

- **New file**: `release_gate/events.py` — `parse_events`,
  `events_to_bundle`, `_normalize`, `_norm_pr_v1`, `_norm_pr_v2`.
- **Body-changed only** (no signature changes):
  - `plugin.py::main` — added the `ingest` subcommand dispatch.
  - `writeback.py::render_comment` — added the `PARTIAL` banner.
- **No existing function signatures changed.**
- **No downstream functions modified** — `to_silver`, `features.py`, and
  `scoring.py` are untouched by the diff.

This confirms existing behaviour is preserved end-to-end and there is no
duplicated bundle-assembly logic between the original collection path and
the new events-ingest path.

## 2. Verification

- **Tests**: 16 pass (`python -m pytest -q`) — 11 original preserved
  (`test_slice.py`, `test_scoring.py`, `test_plugin.py`,
  `test_features.py`) + 5 curveball tests in `test_events.py` (original
  v1 format, new v2 format, unknown events, incomplete/partial input, plus
  parse-error tolerance).
- **Plugin surface**: `entire release-gate` exposes `info`, `version`,
  `collect`, `score`, `handoff`, and `ingest` — the CLI contract is
  additive only.
- **Live Databricks medallion**: refreshed against catalog `release_gate`
  — `bronze` / `silver` / `gold` schemas populated, including
  `gold.pr_risk_scores`.
- **MLflow**: model `release_gate.gold.risk_model` version 1 registered;
  serving endpoint `release-gate-risk` is `READY`, with a heuristic
  fallback path when the endpoint is unavailable.

## 3. Entire usage

- **Graph**: `entire graph search` / `def` lookups and
  `entire graph impact --symbol build_bundle` performed before the
  high-risk curveball change (see `03-curveball-response.md` §2), plus
  this final `entire graph diff` semantic-diff to verify the shipped
  change matches the intended blast radius.
- **Checkpoints**: initial architecture (`00-initial-understanding.md`),
  pre-curveball stable (`02-pre-noon-stable.md`), curveball response
  (`03-curveball-response.md`), and this final verification checkpoint.

## 4. Known limitations

- Checkpoint risk extraction is **heuristic**, not a learned signal.
- The MLflow model is trained on **synthetic seed data**
  (`seed_data/`), not production history.
- The serving endpoint returns a **binary class**, mapped to a
  representative score rather than a calibrated probability.
