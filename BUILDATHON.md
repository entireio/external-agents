# Bengaluru Tech Week Buildathon 2026 — Track 3

## Entire + Cline: Change-Resilient Agent Workflow

### Track

**Track 3 — Bring Entire to a New Agent or Workflow**

### Theme

> Build something that survives a change of plan.

---

## 1. Problem

Coding-agent integrations often depend on a specific transcript and lifecycle-event format. When the agent changes that format, a rigid integration can break, lose session context, or discard incomplete work.

This project brings Entire to Cline through an external-agent integration designed to tolerate transcript/lifecycle format changes while preserving useful development context.

---

## 2. Solution

The integration connects Cline with Entire through:

- a Cline plugin
- an Entire external-agent adapter
- transcript handling
- lifecycle/event handling
- Entire checkpoints
- Entire Graph analysis

The key design is a shared normalization layer.

```text
Original format ──────┐
                      │
                      ▼
              Shared normalization
                      │
New JSONL format ─────┘
                      │
                      ▼
               Common event model
                      │
                      ▼
          Entire lifecycle + checkpoints
```

---

## Final demo readiness

- State the user and problem in one sentence.
- Show the working product and the critical path, rather than a slide-only walkthrough.
- Explain why Entire is essential to the solution.
- Show one useful checkpoint and one changed or verified decision captured by the workflow.
- Explain the Noon Curveball, the behavior that changed, and the test that proves the integration handles it.
- If opting into Databricks, show the essential Databricks function and the evidence that it is working.
- Close with known limitations and the next step toward production readiness.