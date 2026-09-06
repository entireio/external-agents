# Release Gate — Noon Curveball Response (Track 3)

> Checkpoint #3 content. Written by a fresh agent session reconstructing
> context from `entire release-gate handoff` +
> `02-pre-noon-stable.md`, then verifying the blast radius with
> `entire graph impact --symbol build_bundle` before making any change.

## 1. Invalidated assumption

The pre-noon design assumed the integrated workflow emits evidence /
lifecycle events in a **single fixed format** (the original `type`-based
JSON lines consumed by `release_gate/collect.py`). The Noon Curveball
introduces a **new** transcript/lifecycle-event format (`event` +
`payload`-based JSON lines) — while existing integrations continue to emit
the **original** format unchanged. Both must be supported simultaneously;
neither can be assumed away.

## 2. Graph impact (run before editing)

`entire graph impact --symbol build_bundle --repo .` confirms:

- `build_bundle` (`release_gate/collect.py:220`) is the **single assembly
  boundary** — 4 callers (`ci_hook/collect_evidence.py:main`,
  `plugin.py:_bundle_from`/`main`), 6 callees, 5 data flows out, 0 type
  consumers.
- `collect_checkpoint_signals` and `collect_graph_impact` feed both the
  scoring path (via `build_bundle` → `silver` → `features` → `scoring`) and
  the handoff brief (`entire release-gate handoff` reads the same
  checkpoint/graph signals).
- Every downstream stage (`to_silver` → `features` → `scoring` →
  `writeback`) consumes the **normalized evidence bundle**, not raw
  event/transcript data.

Conclusion: the safe fix is to **adapt at the parse boundary** (before a
bundle exists) and keep the evidence-bundle contract completely stable.
Nothing downstream of the bundle needs to know a second format exists.

## 3. Design change

Added `release_gate/events.py` — additive, no existing file modified:

- **One version-detecting parser** (`parse_events`) normalizes both:
  - **v1** (original, `type`-based: `pr` / `graph_impact` / `checkpoint` /
    `test`), and
  - **v2** (new, `event`/`payload`-based: `pull_request.*` / `graph.*` /
    `checkpoint.*` / `test.*`)

  into **one internal event model** (`{pr, graph[], checkpoint[], test[]}`).
  There is no per-format duplication of bundle-assembly logic.
- `events_to_bundle` assembles the **existing** evidence-bundle contract
  (`schema_version`, `pr`, `graph_impact`, `checkpoint_signals`,
  `test_results`) from the normalized model, so `silver.py` / `features.py`
  / `scoring.py` / `writeback.py` / `handoff.py` are untouched.
- **Unknown events are counted, not fatal** — an unrecognized `type`/`event`
  is appended to an `unknown` list and parsing continues.
- **Corrupt/truncated lines are tolerated** — a line that fails
  `json.loads` increments `parse_errors` and is skipped, not raised.
- **Incomplete input yields a PARTIAL bundle** — `events_to_bundle` sets
  `ingest.partial = true` whenever the event stream is missing required
  signal, has parse errors, or contains unknown events. This flag is
  surfaced by `writeback.py` as a **PARTIAL banner** in the PR comment, so
  an incomplete evidence bundle is never presented as an authoritative,
  complete risk decision.
- New CLI subcommand: **`entire release-gate ingest --events <jsonl>`**
  (wired in `release_gate/plugin.py`) reads a lifecycle-event JSONL file,
  runs it through `parse_events` / `events_to_bundle`, and produces a
  schema-valid evidence bundle exactly like `build_bundle` does from git +
  `entire graph`/`entire checkpoint` output — same contract, different
  source.

## 4. Tests

`tests/test_events.py` (new) covers:

1. Original (v1) `type`-based event format parses correctly.
2. New (v2) `event`/`payload`-based format parses correctly.
3. Unknown event kinds are counted, not fatal — no exception raised.
4. Incomplete input (partial stream / parse errors) yields
   `ingest.partial == true` in the resulting bundle.

Existing 11 tests (`test_slice.py`, `test_scoring.py`, `test_plugin.py`,
`test_features.py`, `conftest.py` fixtures) still pass unmodified — **16
total** after adding the events suite.

## 5. Why this is safe

- **Existing behaviour preserved.** `build_bundle` and the
  `entire graph`/`entire checkpoint`-driven collection path
  (`collect.py`) are completely unchanged; the CI hook and native plugin
  callers of `build_bundle` are unaffected.
- **Downstream untouched.** `silver.py`, `features.py`, `scoring.py`,
  `writeback.py`, and `handoff.py` never see raw events — only the same
  evidence-bundle shape they already validate against
  `evidence_schemas/evidence_bundle.schema.json`.
- **Partial results are clearly flagged**, not silently treated as
  complete: `ingest.partial` propagates from the parser through the bundle
  to the PR-comment banner, so a truncated or mixed-format event stream
  degrades gracefully instead of producing a false-confidence gate
  decision.
