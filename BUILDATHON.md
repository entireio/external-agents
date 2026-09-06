@"
# Entire + Cline

## Project

entire-agent-cline

## Problem

Cline is an AI coding agent. Its coding sessions contain useful context such
as user prompts, agent responses, tool activity, modified files, and session
state. We want to make this context available to Entire.

When a coding task is interrupted or the requirements change, developers
should be able to recover the important context instead of starting again
from scratch.

## Solution

Build a native Entire external-agent integration for Cline.

The integration will connect Cline's session lifecycle and session data to
Entire's external-agent protocol.

The adapter will provide:

- Cline detection
- session identification
- session metadata
- transcript access
- prompt extraction
- modified-file extraction
- hook/event handling
- resume information
- token information

## Architecture

Cline
  |
  v
Cline hooks + session data
  |
  v
entire-agent-cline
  |
  v
Entire external-agent protocol
  |
  v
Entire checkpoint
  |
  v
Recoverable development context

## Track

Track 3 — Bring Entire to a New Agent or Workflow.

Entire is essential because the project uses Entire's external-agent
protocol to connect Cline sessions with Entire's checkpoint workflow.

## Implementation Plan

1. Verify Cline CLI behavior.
2. Verify Cline session storage.
3. Verify Cline lifecycle hooks.
4. Implement Cline detection.
5. Implement session identification.
6. Implement transcript handling.
7. Implement prompt and modified-file extraction.
8. Implement hook installation and parsing.
9. Implement resume support.
10. Add tests.
11. Verify a real Cline session with Entire.
12. Use Entire Graph for impact analysis.
13. Demonstrate recovery after the Buildathon Curveball.
"@ | Set-Content BUILDATHON.md