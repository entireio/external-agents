# Codebase Flight Recorder

Codebase Flight Recorder is an evidence-first Codex workflow for **Entire Track
3: Bring Entire to a New Agent or Workflow**. Before a significant code change,
it creates a concise **Before You Code** briefing that explains what is risky,
what code may be affected, which tests are relevant, and what prior development
evidence should influence the next decision.

> Codex already has native Entire support. This project deliberately does not
> create a duplicate `entire-agent-codex` adapter. Instead, it adds the missing
> pre-change risk-and-context workflow while native Entire/Codex hooks retain
> the resulting session and checkpoint.

## Why this exists

An agent can receive a task without the engineering context that makes the task
safe or dangerous: previous attempts, failed tests, retries, reversions,
unresolved questions, and the structural impact of the target code. That leads
to agents treating every request as a green-field edit.

Flight Recorder turns that missing context into an explicit decision before
implementation starts:

```text
Codex task
  -> Codebase Flight Recorder
  -> Entire checkpoints + Entire Graph + reviewed history
  -> Before You Code briefing
  -> implementation and tests
  -> native Entire/Codex checkpoint
```

The goal is not to block the developer. The goal is to make risk, provenance,
and uncertainty visible at the moment they matter.

## What the workflow produces

The CLI prints Markdown by default and can also emit JSON with `--format json`.
Its briefing includes:

- a `LOW`, `MEDIUM`, or `HIGH` risk decision;
- the task and affected files;
- Entire Graph symbols and targeted impact evidence when resolvable;
- test files suggested from repository paths;
- recent Entire checkpoint count;
- matched historical sessions, failures, retries, reversions, and risk score;
- visible findings and warnings when evidence is incomplete or unavailable.

The workflow never fabricates telemetry. If an Entire command, Graph lookup, or
history source is unavailable, invalid, or unrelated to the requested file, the
briefing says so instead of substituting a plausible-looking answer.

## Evidence sources

### 1. Entire checkpoints

The workflow runs `entire checkpoint list --json --no-pager` to surface the
session lineage available in the local checkout. Native Codex hooks remain the
authoritative way to capture post-change work and create real checkpoints.

### 2. Entire Graph

The workflow runs a Graph search for the task and then attempts targeted impact
analysis for the requested file. A requested file that cannot be resolved is
reported as a warning; an unrelated Graph result is not presented as its impact.

### 3. Reviewed development history

`--history` accepts a local, reviewed export. It can come from an approved
Databricks query, a CI export, or another verified source. `--history-source`
is displayed verbatim so reviewers can see where the evidence came from.

The core reviewed record format is a JSON array such as:

```json
[
  {
    "session_id": "reviewed-session-001",
    "files_touched": ["path/to/file.go"],
    "tests": ["go test ./..."],
    "test_result": "failed",
    "retries": 2,
    "revert_count": 1,
    "risk_score": 0.80,
    "summary": "A reviewed, non-sensitive description of the development outcome."
  }
]
```

Only upload approved non-sensitive telemetry. Do not include source code,
prompts, secrets, credentials, customer data, or personal data.

## Quick start

Run from the workflow module:

```bash
cd workflows/codex-flight-recorder

go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "Explain the impact of changing external-agent hook installation" \
  --files "agents/entire-agent-kilo/internal/kilo/hooks.go"
```

Important flags:

| Flag | Purpose |
|---|---|
| `--repo` | Path to the repository whose Entire evidence and Graph are queried. |
| `--task` | Required plain-language description of the proposed change. |
| `--files` | Comma-separated candidate paths to focus Graph impact and historical matching. |
| `--history` | Optional reviewed JSON-array or JSONL lifecycle export. |
| `--history-source` | Provenance label shown in the final briefing. |
| `--format json` | Machine-readable output for UI, CI, or analytics consumers. |

## Risk assessment

The risk decision is evidence-backed rather than a substitute for engineering
review. Historical test failures, retries, reversions, and the maximum reviewed
`risk_score` increase the final risk level. Graph findings help identify the
structural blast radius, while warnings preserve uncertainty when a source does
not resolve.

For example, the committed synthetic demo history has two sessions touching the
Kilo hook installation code. One failed, required retries, and was reverted;
the second passed after follow-up validation. When the tool matches that history
to the requested file, it reports `HIGH` risk with the provenance label intact.

## Curveball: transcript-format compatibility

The Curveball introduced a new JSONL lifecycle/transcript format. Flight
Recorder normalizes both the original reviewed JSON array and the new JSONL
format into the same history record model, so risk analysis is not duplicated.

Supported JSONL lifecycle signals include:

- `session_started` for session identity and agent context;
- `file_changed` for touched-file evidence and change summaries;
- `tool_result` for failures and successful retries;
- `checkpoint_created` for historical intent, summary, and open questions;
- `session_ended` for a completed session.

Compatibility guarantees:

| Situation | Behaviour |
|---|---|
| Original reviewed JSON array | Continues to parse unchanged. |
| New JSONL lifecycle export | Normalized into the existing risk pipeline. |
| Unknown event type | Ignored safely and reported in `Ignored JSONL events`. |
| JSONL ends before `session_ended` | Evidence is retained with `Status: PARTIAL` and a verification warning. |
| JSONL `checkpoint_created` event | Used as historical context only; it does not replace native Entire checkpoints. |

The organizer fixture is committed at
`internal/briefing/testdata/track-3-agent-session.jsonl` and is covered by unit
tests along with original-format, unknown-event, and incomplete-stream cases.

## Databricks analytics

Databricks is an optional analytics producer, not a runtime dependency of the
CLI. The SQL notebook at
[databricks/flight_recorder_analytics.sql](databricks/flight_recorder_analytics.sql)
creates:

- `codebase_flight_recorder.development_history`, a Delta table for approved
  development telemetry;
- `codebase_flight_recorder.component_risk`, a per-file view that aggregates
  sessions, failed sessions, retries, and reversions;
- a final export query whose JSON fields match the `--history` contract.

The risk model is:

```text
risk_score = min(1.0,
  failed_sessions * 0.40 + retries * 0.08 + reverts * 0.20)
```

For the Buildathon demo, a live Databricks dashboard was created with explicitly
labelled **synthetic** rows only. Its Kilo hooks example shows two sessions, one
failed session, three retries, one revert, and a component risk score of `0.84`.
This is a transparent demonstration of the analytics model, not production
telemetry. See [databricks/README.md](databricks/README.md) for setup and export
details.

## Run the verified demos

### Test the workflow

```bash
cd workflows/codex-flight-recorder
go test ./...
```

Expected result: the `internal/briefing` package passes with no `FAIL` output.

### Demonstrate the Curveball JSONL input

```bash
go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "Assess historical risk for coupon validation changes" \
  --files "src/checkout/apply_coupon.ts,tests/checkout/apply_coupon.test.ts" \
  --history internal/briefing/testdata/track-3-agent-session.jsonl \
  --history-source "Organizer Track 3 JSONL fixture"
```

Expected signals: `Status: COMPLETE`, `Matched sessions: 1`, `failed: 1`, and
`retries: 1`. The fixture refers to an external sample checkout, so a Graph
warning about unresolved requested files is expected and proves the workflow
does not invent unrelated impact.

### Demonstrate Databricks-style historical risk

```bash
go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "Assess hook-installation risk" \
  --files "agents/entire-agent-kilo/internal/kilo/hooks.go" \
  --history examples/synthetic-development-history.json \
  --history-source "SYNTHETIC DEMO DATA - Databricks-style export"
```

Expected signals: `Risk: HIGH`, an Entire Graph impact focused on
`Agent.UninstallHooks`, and historical evidence with two matched sessions, one
failure, four retries, one revert, and maximum risk score `0.90`.

### Confirm checkpoint lineage and clean branch

```bash
cd ../..
entire checkpoint list
git status -sb
```

The demonstrated branch is `feature/codex-flight-recorder`. It contains three
Entire checkpoints, including the Curveball implementation checkpoint and the
final documentation checkpoint. Main was not modified.

## Repository guide

| Path | Purpose |
|---|---|
| `cmd/codex-flight-recorder/main.go` | CLI flags and Markdown/JSON rendering. |
| `internal/briefing/briefing.go` | Evidence collection, history normalization, and risk analysis. |
| `internal/briefing/briefing_test.go` | Unit coverage for evidence and Curveball behavior. |
| `internal/briefing/testdata/` | Organizer JSONL fixture used by tests. |
| `examples/synthetic-development-history.json` | Clearly labelled local demo history. |
| `databricks/` | Databricks SQL analytics notebook and setup notes. |
| `DEMO.md` | Two-minute judge demo and verification checklist. |

## Limitations and next steps

- The CLI consumes a reviewed history export instead of directly authenticating
  to Databricks. This keeps provenance explicit and avoids embedding credentials.
- Test recommendations are path-based heuristics, not a replacement for owner
  review or complete test planning.
- Graph and checkpoint availability depend on the local Entire environment;
  warnings are deliberate evidence, not silent fallbacks.
- A production follow-up could add a documented scheduled Databricks export,
  richer checkpoint summaries, and authenticated integration-test coverage.

## Judge walkthrough

For a short presentation, use [DEMO.md](DEMO.md). It provides the exact
two-minute sequence: Curveball briefing, hook-risk briefing, Databricks
dashboard, and Entire checkpoint proof.
