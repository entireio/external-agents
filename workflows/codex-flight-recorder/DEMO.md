# Codebase Flight Recorder demo

## The pitch

Codebase Flight Recorder gives a coding agent an evidence-backed **Before You
Code** briefing. It combines prior Entire checkpoints, Entire Graph impact,
and reviewed development history so a risky change is visible before code is
written.

## Live proof

This build was demonstrated on `feature/codex-flight-recorder` with:

- an Entire checkpoint before implementation and a final checkpoint attached
  to the Curveball commit;
- Entire Graph impact for `Agent.UninstallHooks`;
- a Curveball JSONL transcript with a failed tool result, retry, and historical
  checkpoint context;
- a Databricks `component_risk` view and dashboard using explicitly labelled
  synthetic telemetry only.

## Two-minute walkthrough

1. Run the briefing with the Curveball fixture. Point out `Status: COMPLETE`,
   `failed: 1`, `retries: 1`, and the preserved historical checkpoint context.
2. Run the hook-risk briefing with the synthetic Databricks-style export. Point
   out the `HIGH` risk decision, Entire checkpoint count, Graph impact, and
   `max risk score: 0.90`.
3. Open the Databricks dashboard. Explain that the component score is computed
   from sessions, failures, retries, and reverts; the demo row scores `0.84`.
4. Show `entire checkpoint list` and `git status -sb` to prove the checkpoint
   lineage and clean feature branch.

## Verification commands

```bash
cd workflows/codex-flight-recorder
go test ./...

go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "Assess historical risk for coupon validation changes" \
  --files "src/checkout/apply_coupon.ts,tests/checkout/apply_coupon.test.ts" \
  --history internal/briefing/testdata/track-3-agent-session.jsonl \
  --history-source "Organizer Track 3 JSONL fixture"

go run ./cmd/codex-flight-recorder \
  --repo ../.. \
  --task "Assess hook-installation risk" \
  --files "agents/entire-agent-kilo/internal/kilo/hooks.go" \
  --history examples/synthetic-development-history.json \
  --history-source "SYNTHETIC DEMO DATA — Databricks-style export"
```

## Curveball guarantees

The workflow supports both the original reviewed JSON array and the new JSONL
lifecycle format. Unknown events are reported instead of crashing the run; an
incomplete JSONL transcript remains available as `PARTIAL` evidence with a
verification warning. Native Entire/Codex hooks remain the source of real
checkpoints.

## Transparency

The Databricks dashboard contains synthetic demo telemetry, not production
history. The workflow accepts a reviewed export rather than directly reading
Databricks, so provenance stays visible in every briefing.
