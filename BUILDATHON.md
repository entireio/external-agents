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

## Setup, run, and test

```bash
cd workflows/codex-flight-recorder
go test ./...
go run ./cmd/codex-flight-recorder --repo ../.. --task "your significant coding task"
```

## Databricks use, data sources, and limitations

Databricks is an optional analytics producer, not a required runtime dependency. A future Databricks job may export non-sensitive development-history records for `--history`; the current implementation labels this source and requires it to be explicitly supplied. The included demo fixture is explicitly synthetic and is not presented as real history. No Databricks credentials or data are stored in this repository.

## Known limitations and next steps

The initial version consumes a reviewed JSON export rather than calling Databricks directly, and its test recommendations are Go-test path heuristics. The next iteration should add a documented Databricks export job, richer checkpoint summaries, and a small integration test that exercises an authenticated Entire mirror.
