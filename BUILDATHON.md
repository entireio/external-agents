# Codebase Flight Recorder

## Submission summary

**Codebase Flight Recorder** gives Codex an evidence-backed **Before You Code**
briefing before a significant repository change. It combines Entire checkpoint
lineage, Entire Graph impact, and reviewed development history to make risk,
test coverage, and uncertainty visible before implementation begins.

**Track:** Track 3 - Bring Entire to a New Agent or Workflow

**Implementation branch:** `feature/codex-flight-recorder`

**Final documented commit:** `1eb7209`

## Problem

Coding agents often receive only a task and a repository snapshot. They do not
automatically know the previous failures, retries, reversions, unresolved
questions, or structural dependencies that determine whether a change is risky.
That can make a confident-looking code change unsafe.

The intended user is a developer who directs Codex through consequential code
changes and wants evidence before authorizing implementation.

## Solution

Flight Recorder is a standalone Go workflow that creates a concise Markdown or
JSON briefing:

```text
Codex task
  -> Flight Recorder
  -> Entire checkpoints + Entire Graph + reviewed history
  -> Before You Code risk decision
  -> implementation and tests
  -> native Entire/Codex checkpoint
```

The briefing reports a `LOW`, `MEDIUM`, or `HIGH` risk decision, affected files,
Graph symbols, suggested tests, checkpoint count, history findings, and visible
warnings for unavailable or unresolved evidence.

## Why Entire is essential

Codex is already a native Entire agent, so this project does not duplicate it as
an external agent adapter. Instead, it adds a new workflow around Codex:

- **Entire checkpoints** preserve session lineage and prior implementation
  context.
- **Entire Graph** supplies source search and targeted impact evidence before
  editing.
- **Native Entire/Codex hooks** remain authoritative for capturing the completed
  session and creating real checkpoints after work is done.

If a requested file cannot be resolved by Graph, the workflow emits a warning
and never presents unrelated Graph output as the requested impact.

## What was built

| Capability | Implementation | Demonstrated outcome |
|---|---|---|
| Before You Code workflow | Go CLI in `workflows/codex-flight-recorder` | Evidence-backed risk briefing before implementation |
| Entire integration | Checkpoint query plus Graph search and targeted impact | 3 real Entire checkpoints on the feature branch |
| Historical risk analysis | Reviewed JSON history normalized into briefing evidence | Failed sessions, retries, reversions, and risk score affect risk |
| Curveball compatibility | Shared normalizer for original JSON and new JSONL lifecycle input | Original, new, unknown, and incomplete cases tested |
| Databricks analytics | Delta development-history table and `component_risk` view | Live dashboard shows per-file synthetic demo risk |

## Curveball response

The Curveball changed the agent transcript/lifecycle format. Flight Recorder now
supports both the original reviewed JSON array and the new JSONL lifecycle
format without duplicating risk analysis.

| Curveball requirement | Result |
|---|---|
| Original history format remains valid | Supported and unit-tested |
| New JSONL lifecycle events | `session_started`, `file_changed`, `tool_result`, `checkpoint_created`, and `session_ended` are normalized |
| Unknown events | Safely ignored and reported; cannot crash parsing |
| Incomplete transcript | Retained as `PARTIAL` evidence with a verification warning |
| Existing checkpoints | Native Entire/Codex checkpoint behavior remains unchanged |

The organizer JSONL fixture records one failed tool result, one successful retry,
file changes, a historical checkpoint, and a completed session. The workflow
reports `Status: COMPLETE`, one matched session, one failure, and one retry.

## Databricks integration

Databricks is the optional analytics producer. The included SQL notebook:

- creates `codebase_flight_recorder.development_history` as a Delta table;
- creates `component_risk`, a per-file view aggregating sessions, failures,
  retries, and reversions;
- produces a reviewed export compatible with the Flight Recorder `--history`
  input contract.

The demo dashboard uses explicitly labelled **synthetic demo data**, not
production telemetry. Its Kilo hooks example contains two sessions, one failed
session, three retries, one revert, and a risk score of `0.84`.

No source code, prompts, secrets, credentials, personal data, or production
development history were uploaded to Databricks.

## Verification evidence

| Check | Verified result |
|---|---|
| `go test ./...` | Passed |
| Curveball JSONL run | `Status: COMPLETE`; failed `1`; retries `1` |
| Databricks-style history run | `Risk: HIGH`; 2 matched sessions; max risk score `0.90` |
| Entire Graph | Targeted impact resolved `Agent.UninstallHooks` for the Kilo hook demo |
| Entire checkpoints | 3 checkpoints, including Curveball and final documentation checkpoints |
| Git state | Clean `feature/codex-flight-recorder` branch; main untouched |

## Review and reproduce

```bash
cd workflows/codex-flight-recorder
go test ./...

go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "Assess hook-installation risk" \
  --files "agents/entire-agent-kilo/internal/kilo/hooks.go" \
  --history examples/synthetic-development-history.json \
  --history-source "SYNTHETIC DEMO DATA - Databricks-style export"

cd ../..
entire checkpoint list
git status -sb
```

The expected hook-risk briefing is `HIGH` and includes checkpoint count, Graph
impact, two matched sessions, one failure, four retries, one revert, and maximum
risk score `0.90`.

## Repository map

- [Detailed workflow README](workflows/codex-flight-recorder/README.md)
- [Two-minute demo runbook](workflows/codex-flight-recorder/DEMO.md)
- [Flight Recorder implementation](workflows/codex-flight-recorder/)
- [Databricks SQL analytics notebook](workflows/codex-flight-recorder/databricks/flight_recorder_analytics.sql)

## Limitations and next steps

- Flight Recorder consumes a reviewed export rather than directly embedding
  Databricks credentials or a live database connection.
- Graph-derived test recommendations are path heuristics and do not replace
  engineering review.
- Checkpoint and Graph availability depend on the local Entire environment;
  visible warnings are deliberate instead of silent fallbacks.

Future work could add a scheduled reviewed Databricks export, richer checkpoint
summaries, and authenticated integration-test coverage.
