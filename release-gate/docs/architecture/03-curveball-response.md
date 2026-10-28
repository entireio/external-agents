# Release Gate — Noon Curveball Response (Track 3)

> Checkpoint #3 content. Written by a fresh agent session reconstructing
> context from `entire release-gate handoff` +
> `02-pre-noon-stable.md`, then verifying the blast radius with
> `entire graph impact --symbol build_bundle` before making any change.

## 1. Invalidated assumption

The pre-noon design assumed the integrated workflow emits evidence /
lifecycle events in a **single fixed format** (the original `type`-named
JSON lines: `start`/`edit`/`test`/`checkpoint`/`end`, consumed by
`release_gate/collect.py`). The Noon Curveball delivers the **official
Track-3 fixture** (`release-gate/seed_data/track-3-agent-session.jsonl`) — an
AcmeCode **agent-session transcript** in a **new**, `event`-named format
(`session_started`, `user_prompt`, `agent_response`, `tool_call`,
`tool_result`, `file_read`, `file_changed`, `usage`, `checkpoint_created`,
`session_ended`) — while existing integrations continue to emit the
**original** format unchanged. Both must be supported simultaneously;
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

- **One version-detecting parser** (`parse_events`) normalizes both
  transcript formats into **one internal event model**
  (`{meta, changes[], checkpoints[], tests, ended}`):
  - **new** (official Track-3 fixture, `event`-named): `session_started`,
    `file_changed`, `checkpoint_created`, `session_ended`, plus `tool_call`
    / `tool_result` pairs correlated by `call_id`.
  - **original** (`type`-named): `start`, `edit`, `test`, `checkpoint`, `end`.

  There is no per-format duplication of bundle-assembly logic.
- `events_to_bundle` assembles the **existing** evidence-bundle contract
  (`schema_version`, `pr`, `graph_impact`, `checkpoint_signals`,
  `test_results`) from the normalized model, so `silver.py` / `features.py`
  / `scoring.py` / `writeback.py` / `handoff.py` are untouched.
- **Churn** comes from `file_changed.lines_added`/`lines_removed` (new) or
  `edit.added`/`removed` (original), summed per file into `pr.churn`.
- **Unresolved risks** come from `checkpoint_created.open_questions` (new)
  or `checkpoint.risks` (original) — both map to
  `checkpoint_signals.unresolved_risk_count`.
- **Test counts** are derived differently per format: the new transcript has
  no direct test event, so `tool_call` commands containing `test` (e.g.
  `npm test -- ...`) are correlated by `call_id` to their `tool_result`
  summary (`"8 passed"`, `"1 failed, 7 passed"`) and parsed into
  passed/failed/skipped counts; the original format reports a `test` event
  directly with those counts already structured.
- **Known-but-no-evidence events are skipped, not unknown**: `user_prompt`,
  `agent_response`, `file_read`, and `usage` are recognized but contribute no
  bundle signal.
- **Unknown event types are counted, not fatal** — an unrecognized
  `type`/`event` is appended to an `unknown` list and parsing continues.
- **Corrupt/truncated lines are tolerated** — a line that fails
  `json.loads` increments `parse_errors` and is skipped, not raised.
- **Incomplete input yields a PARTIAL bundle** — `events_to_bundle` sets
  `ingest.partial = true` whenever the event stream is missing required
  signal, has parse errors, or contains unknown events. This flag is
  surfaced by `writeback.py` as a **PARTIAL banner** in the PR comment, so
  an incomplete evidence bundle is never presented as an authoritative,
  complete risk decision.
- New CLI subcommand: **`entire release-gate ingest --events <jsonl>`**
  (wired in `release_gate/plugin.py`) reads an agent-session transcript
  JSONL file, runs it through `parse_events` / `events_to_bundle`, and
  produces a schema-valid evidence bundle exactly like `build_bundle` does
  from git + `entire graph`/`entire checkpoint` output — same contract,
  different source.

## 4. Tests

`tests/test_events.py` (new) covers, against three fixtures —
`seed_data/track-3-agent-session.jsonl` (**official** new-format transcript),
`agent-session-original.jsonl` (original format), and
`agent-session-incomplete.jsonl` (truncated/unknown-event stream):

1. New format — the official Track-3 fixture parses correctly (churn, risks,
   correlated test counts, `formats == ["new"]`, `partial == False`).
2. Original format parses correctly (`formats == ["original"]`,
   `partial == False`).
3. Unknown event kinds are counted, not fatal — no exception raised.
4. Incomplete input (parse errors + unknown events) yields
   `ingest.partial == true` and still produces a scorable bundle.

Existing 11 tests (`test_slice.py`, `test_scoring.py`, `test_plugin.py`,
`test_features.py`, `conftest.py` fixtures) still pass unmodified — **17
total** after adding the events suite (6 tests in `test_events.py`).

## 5. Why this is safe

- **Existing behaviour preserved.** `build_bundle` and the
  `entire graph`/`entire checkpoint`-driven collection path
  (`collect.py`) are completely unchanged; the CI hook and native plugin
  callers of `build_bundle` are unaffected.
- **Downstream untouched.** `silver.py`, `features.py`, `scoring.py`,
  `writeback.py`, and `handoff.py` never see raw events — only the same
  evidence-bundle shape they already validate against
  `evidence_schemas/evidence_bundle.schema.json`. Confirmed with
  `entire graph diff` (entity-level semantic diff): `to_silver`,
  `build_gold_features`, and `score_features` show zero changes.
- **Partial results are clearly flagged**, not silently treated as
  complete: `ingest.partial` propagates from the parser through the bundle
  to the PR-comment banner, so a truncated or mixed-format event stream
  degrades gracefully instead of producing a false-confidence gate
  decision.
