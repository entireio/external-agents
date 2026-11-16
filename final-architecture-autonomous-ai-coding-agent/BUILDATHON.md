# Autonomous Coding Agent — Buildathon Submission

## Problem Statement

Software teams want AI coding help, but a useful agent must do more than emit
code. It must understand an existing repository, choose an appropriate model,
make changes safely, validate them, explain failures, preserve a human approval
gate, and leave an auditable record of what happened.

The challenge is amplified in existing repositories. A change such as “add a
health endpoint without breaking authentication” requires project context,
change isolation, test awareness, security checks, and traceability across the
whole lifecycle.

## Proposed Solution

**Autonomous Coding Agent** is a local development workbench and LangGraph
workflow for safely executing AI-assisted coding tasks. It supports both empty
and existing repositories through one controlled flow:

```text
Request → Repository context → Analysis → Plan → Model route
       → Structured proposal → Security review → Human approval
       → Apply change → Validate → Debug/repair (bounded) → Deploy
```

The product is intentionally designed around clear responsibilities:

| Component | Responsibility |
|---|---|
| LangGraph | Controls workflow state, branch decisions, approval, and bounded retries |
| Databricks | Model-serving gateway for coding, reasoning, and fast model roles |
| Workspace tools | Safely preview/apply repository-contained changes and run argument-array commands |
| Entire | Captures Codex development lifecycle context through project hooks and Git checkpoints |
| Git | Version control, commit history, and change provenance |
| Local workbench | Human-facing request, review, configuration, and timeline UI |

## Delivered Product

The runnable application is in `autonomous-coding-agent/`.

### Local Workbench UI

Start it with:

```powershell
cd autonomous-coding-agent
npm run serve
```

Open `http://127.0.0.1:8503`.

The UI provides:

- Natural-language task and repository path input
- Proposed file-change editor
- Explicit approval gate for writes
- Optional one-attempt auto-repair toggle
- Validation-command configuration
- Databricks setup guidance without collecting credentials in the UI
- Entire + Codex hook-install option
- Docker, Vercel, and Netlify deployment selection
- Live workflow timeline and final state display

The server binds to `127.0.0.1` by default. It is a local development tool, not
a publicly exposed production API.

### Repository Intelligence

Before proposing changes, the agent scans the target repository and gathers:

- Repository files, while ignoring generated/runtime directories
- README and common package/config files
- Test-file discovery
- Project type, dependency map, and file-extension architecture map
- Git branch, recent commits, and working-tree status
- Suggested validation commands for Node and Python projects

### LangGraph Workflow

The graph contains these nodes:

```text
start_request
  → context
  → analyze
  → plan
  → route
  → code
  → test strategy
  → review
  → approval
      ├─ not approved / security blocked → stop safely
      └─ approved → Entire setup → apply → validate
                                           ├─ pass/skip → deployment adapter
                                           └─ fail → debugger
                                                        ├─ approved safe repair → apply again
                                                        └─ stop with diagnosis
```

The agent never writes a change before explicit approval. Auto-repair is
disabled by default and is limited by `maxRepairAttempts` (one by default).

### Structured Model Proposals

The coding model is asked to return JSON in this form:

```json
{
  "summary": "What will change and why",
  "changes": [
    { "path": "src/health.js", "content": "Full replacement file content" }
  ],
  "tests": ["Add a regression test for the health endpoint"]
}
```

Users can also enter reviewed `proposedChanges` in the workbench. Explicit UI
changes take precedence over generated changes.

### Safety Controls

- Change paths must remain inside the selected repository.
- Commands are string arrays executed without a shell.
- Proposed content is blocked when it contains common high-risk patterns:
  dynamic `eval`, shell execution, or embedded secrets.
- No deployment command runs unless deployment is selected, changes are
  approved, and validation reaches a non-failed state.
- Credentials are server-side environment variables only; the UI never stores
  or returns them.
- The configuration status endpoint reports booleans only, never secret values.

## Databricks Workflow

Databricks is the model gateway; LangGraph remains the orchestration layer.

```text
LangGraph task node
   → model role selection
       ├─ coding: implementation / refactoring / tests
       ├─ reasoning: debugging / failure analysis / review
       └─ fast: documentation / simple summaries
   → Databricks Model Serving endpoint
   → structured model response
   → LangGraph state + local timeline
```

The gateway calls:

```text
POST {DATABRICKS_HOST}/serving-endpoints/{endpoint}/invocations
```

It sends chat-style messages and attaches workflow/model-role metadata. One
default endpoint can serve all roles, or each role can use a dedicated endpoint.

### Databricks Configuration

Create `autonomous-coding-agent/.env` locally from `.env.example`:

```env
DATABRICKS_HOST=https://adb-<workspace-id>.<region>.azuredatabricks.net
DATABRICKS_TOKEN=<access-token>
DATABRICKS_ENDPOINT=<model-serving-endpoint-name>

# Optional role-specific endpoints
# DATABRICKS_CODING_ENDPOINT=<coding-endpoint-name>
# DATABRICKS_REASONING_ENDPOINT=<reasoning-endpoint-name>
# DATABRICKS_FAST_ENDPOINT=<fast-endpoint-name>
```

Requirements:

1. A Databricks workspace URL.
2. A deployed Model Serving endpoint compatible with chat-style messages.
3. A personal-access-token or OAuth identity with **Can Query** permission on
   the endpoint.
4. HTTPS network access from the local service to the workspace.

Verify setup without exposing credentials:

```powershell
Invoke-RestMethod http://127.0.0.1:8503/api/config
```

The response reports configuration presence only. `.env` is ignored by Git.

### Databricks Evaluation and Memory

The current MVP records a local evaluation score for requirement coverage,
security review, validation, and approval. Project memory is opt-in and stored
locally in `.entire/autonomous-agent/memory.jsonl`.

The architecture keeps these interfaces separate so a follow-up deployment can
export model traces, evaluation runs, latency, token usage, and project memory
to Databricks/MLflow without changing orchestration behavior.

## Entire + Codex Integration

Entire is used as the source of development lifecycle context, not as an LLM
or workflow engine.

```text
Codex project hook
  → Entire captures session / prompt / turn / tool activity
  → Git commit
  → Entire checkpoint links transcript, file changes, tokens, and commit history
```

For Codex, the integration flow is:

```powershell
# First-time Entire setup in a Git repository
entire enable --agent codex

# Add or refresh Codex hooks
entire agent add codex

# Verify capture setup
entire agent list
entire status --detailed
```

Entire writes Codex project hooks to `.codex/hooks.json`. Start a new Codex
session after hook installation so the session loads the updated configuration.
After a file-changing session, commit the change and inspect checkpoints:

```powershell
git add .
git commit -m "Describe the change"
entire checkpoint list
```

In the workbench, selecting **Install Entire’s Codex hooks** and approving the
workflow runs `entire agent add codex`, then verifies with `entire agent list`
and `entire status --detailed`.

### Entire Requirements

- The target workspace must be a Git repository.
- The Entire CLI must be installed and available on `PATH`.
- Codex must be installed and authenticated.
- The user must explicitly approve hook setup in the workbench.

The app’s local JSONL timeline complements Entire but does not replace it. A
future `entire-agent-autonomous` binary is required if this standalone
application itself must be registered as an Entire external agent rather than
using Codex’s native integration.

## Deployment Workflow

Deployment is intentionally opt-in:

```text
Approved change + validation passes
  → Deployment adapter
      ├─ Docker: docker build -t <image> .
      ├─ Vercel: vercel [--prod]
      ├─ Netlify: netlify deploy [--prod]
      └─ Explicit custom argument-array command
```

The relevant CLI must be installed and authenticated. The application does not
create cloud accounts, credentials, or production deployments automatically.

## Testing and Quality

Run the complete suite:

```powershell
cd autonomous-coding-agent
npm test
```

Current result: **19 tests passing**.

### Critical Behavior Covered

| Behavior | Test coverage |
|---|---|
| Model routing | Coding, reasoning, and fast task roles |
| Databricks request routing | Role-specific endpoint is selected when configured |
| Secret safety | Configuration response never includes token values |
| Approval gate | No writes occur before approval |
| Workspace protection | Change operations are repository-contained |
| Validation | Passed commands and terminal results are captured |
| Deployment gate | Deployment requires explicit enablement |
| Entire setup | Codex hook installation command and verification commands |
| UI/API | Health endpoint, local workbench, run API, and timeline delivery |

### Curveball Behavior Covered

| Curveball | Expected behavior |
|---|---|
| Validation fails | Reasoning model provides diagnosis; workflow stops safely by default |
| Safe auto-repair enabled | Exactly one bounded repair can be applied and revalidated |
| Generated shell execution, `eval`, or embedded secret | Security review blocks the change before write |
| No validation command available | Workflow records validation as skipped rather than inventing a shell command |
| Empty/missing repository | Context layer identifies a new project without crashing |
| No Databricks config | Gateway provides deterministic local demo output rather than exposing or fabricating credentials |
| Entire not requested | No CLI command or hook configuration is attempted |
| Deployment provider absent | Deployment is recorded as not requested |

## Demo Script

1. Start the workbench: `npm run serve`.
2. Open `http://127.0.0.1:8503`.
3. Enter a request and repository path.
4. Add a proposed source file and optional `npm test` validation command.
5. Run with approval disabled to show planning, routing, review, and the safe
   approval stop.
6. Enable approval to show a reviewed change, validation result, evaluation,
   and timeline events.
7. If Databricks is configured, show role routing through the serving endpoint.
8. If Entire and Codex are configured in a Git repository, enable Codex hooks,
   start a new Codex session, commit a change, and show `entire checkpoint list`.

## Current Limitations and Next Steps

- The repository map is lightweight/static; full multi-language AST and
  semantic-impact analysis is a future extension.
- Regression tests are requested as part of structured model proposals; a
  language-specific test-generation library is the next enhancement.
- Evaluation and memory are local interfaces today; MLflow/Databricks export is
  the next production integration.
- Deployment adapters invoke installed CLIs; managed cloud credential setup is
  intentionally outside the app.
- Native Entire capture for this app requires a future
  `entire-agent-autonomous` protocol adapter.

## Security and Secrets

This document contains no workspace URLs, tokens, endpoint names, account
identifiers, repository-private data, or deployment credentials. Never commit
`.env`, access tokens, OAuth secrets, or generated runtime timeline files.
