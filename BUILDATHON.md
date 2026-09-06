# Codebase Flight Recorder

## One-sentence summary

Codebase Flight Recorder gives Codex an evidence-backed risk and context briefing before a significant code change.

## Problem and intended user

Developers and coding agents inherit a diff without the earlier decisions, failures, and structural impact that make a change risky. The intended user is a developer directing Codex through a consequential repository change.

## Selected Entire track and why Entire is essential

Track 3 — Bring Entire to a New Agent or Workflow. Codex is already natively supported by Entire, so the project adds a new workflow rather than duplicating Codex lifecycle support. It reads Entire checkpoint information and invokes Entire Graph before Codex edits; native Codex hooks preserve the post-change session and checkpoint.

## Architecture and main workflow

`Codex task → Codebase Flight Recorder → Entire checkpoint query + Entire Graph search/impact + optional reviewed history export → Before You Code briefing → Codex change → tests → native Entire Codex checkpoint`.

The implementation is in `workflows/codex-flight-recorder`. It intentionally uses no invented telemetry: an unavailable checkpoint, Graph, or history source becomes a visible warning.

## Initial checkpoint context

- **Implemented architecture:** a standalone Go workflow gathers Entire checkpoint and Graph evidence with an optional, reviewed history export, then produces a Before You Code briefing; native Codex hooks retain the post-change session and checkpoint.
- **Completed P0/P1 work:** P0 delivered the briefing command, evidence model, Graph impact lookup, and unit coverage. P1 prioritised explicitly requested files for impact analysis and added a visibly labelled synthetic-history demo with caller-supplied provenance.
- **Verification performed:** the workflow has focused unit tests for combined evidence, unavailable sources, and required task input; its pre-change briefing was run against this documentation change.
- **Known limitations:** checkpoint/history availability depends on the local environment; history is a reviewed JSON input, Graph-derived test suggestions are path heuristics, and the demo data is synthetic.
- **Curveball recovery:** when checkpoints, Graph, or history are unavailable, invalid, or do not resolve the requested file, surface the warning, avoid substituting example data or unrelated impact, verify the cited source/tests directly, and proceed only with the evidence that is available.

## Setup, run, and test

```bash
cd workflows/codex-flight-recorder
go test ./...
go run ./cmd/codex-flight-recorder --repo ../.. --task "your significant coding task"
```

## Databricks use, data sources, and limitations

Databricks is an optional analytics producer, not a required runtime dependency. The repository now includes a Databricks SQL notebook that calculates a per-file `risk_score` from non-sensitive development-history rows; that score is consumed by `--history` and influences the Before You Code risk level. A workspace has not been configured in this checkout. The included demo fixture is explicitly synthetic and is not presented as real history. No Databricks credentials or data are stored in this repository.

## Known limitations and next steps

The initial version consumes a reviewed JSON export rather than calling Databricks directly, and its test recommendations are Go-test path heuristics. The next iteration should add a documented Databricks export job, richer checkpoint summaries, and a small integration test that exercises an authenticated Entire mirror.
