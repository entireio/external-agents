# Bengaluru Tech Week Buildathon 2026 - Track 3

## One-sentence summary

Entire + Cline provide a change-resilient agent workflow that preserves useful development context when transcript or lifecycle formats change.

## Problem, intended user and why it matters

Coding-agent integrations often depend on a specific transcript and lifecycle-event format. When the agent changes that format, a rigid integration can break, lose session context, or discard incomplete work.

The intended user is a developer using Cline with Entire who needs agent work to remain traceable and recoverable as workflows change.

## Selected Entire track and why Entire is essential

**Track 3 - Bring Entire to a New Agent or Workflow**

The integration brings Entire to Cline through an external-agent adapter. Entire is essential because it provides lifecycle handling, checkpoints, and Graph analysis that preserve and verify development context across agent changes.

## Architecture and main workflow

The integration connects Cline with Entire through:

- a Cline plugin
- an Entire external-agent adapter
- transcript handling
- lifecycle/event handling
- Entire checkpoints
- Entire Graph analysis

The key design is a shared normalization layer:

```text
Original format -------+
                      |
                      v
              Shared normalization
                      ^
New JSONL format ------+
                      |
                      v
               Common event model
                      |
                      v
          Entire lifecycle + checkpoints
```

## Entire Graph findings and verification

Document the relevant Entire Graph finding here, including the code path or relationship it verified and how the finding supports the change-resilient workflow.

## Noon Curveball: what changed and how we adapted

Document the Noon Curveball here: describe the behavior that changed, the adaptation made in the integration, and the test that proves the new behavior works.

## Checkpoint links and what each checkpoint proves

Add links to the relevant checkpoints and describe what each one proves, including at least one changed or verified decision.

## Setup, run and test instructions

Add the commands needed to set up, run, and test the Cline external agent integration.

## Databricks use, data sources and limitations (if applicable)

If Databricks is used, document the essential Databricks function, data sources, and evidence that it is working. Otherwise, state that Databricks is not used by this project.

## Known limitations and next steps

Document known limitations and the next step toward production readiness.