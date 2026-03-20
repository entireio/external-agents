---
name: research
description: >
  Phase 1: Assess a target agent's capabilities and produce a protocol mapping
  for building an external agent binary. Use /entire-external-agent research
  or /entire-external-agent:research when you only need the research phase.
---

# Research Procedure

Assess a target agent's capabilities and produce a protocol mapping for building an external agent binary.

## Prerequisites

Ensure the following parameters are available (from the orchestrator or user):
- `AGENT_NAME` — Human-readable agent name
- `AGENT_SLUG` — Kebab-case slug for the binary name
- `PROJECT_DIR` — Where the project will be created

## Phase 1: Understand the External Agent Protocol

Read the protocol specification to understand what subcommands and response formats the binary must implement.

**Protocol spec:**
1. Read `https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md` — full protocol spec (subcommands, JSON schemas, capabilities)
2. Read `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/types.go` — JSON response type definitions
3. Read `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/external.go` — how the CLI invokes each subcommand (args, stdin, expected stdout)

If the user provides a different protocol spec location explicitly, read that instead.

Key things to note:
- Which subcommands are always required vs. capability-gated
- The JSON schema for each response type
- The HookInput, AgentSession, and Event object formats
- Environment variables set on every invocation (`ENTIRE_REPO_ROOT`, `ENTIRE_PROTOCOL_VERSION`)

## Phase 2: Investigate the Target Agent

Ask the user: "Do you have documentation or specs for the target agent's hook/lifecycle system? Or should I auto-research?"

### If user provides docs:
Read the provided docs and extract:
- Hook/lifecycle mechanism (how to register callbacks)
- Session management (where sessions are stored, how IDs work)
- Transcript format and location
- Configuration file format and location

### If auto-research:

#### Step 1: Binary probing

Run non-destructive CLI checks. Record PASS/WARN/FAIL for each:

| Check | Command | PASS | FAIL |
|-------|---------|------|------|
| Binary present | `command -v <agent-binary>` | Found | Not found |
| Help output | `<agent-binary> --help` or `<agent-binary> help` | Available | No help |
| Version info | `<agent-binary> --version` | Available | N/A |
| Hook keywords | Scan help for: hook, lifecycle, callback, event, trigger, pre-, post-, plugin, extension | Found | None found |
| Session keywords | Scan help for: session, resume, continue, history, transcript, context | Found | None found |
| Config directory | Check `~/.<agent-slug>/`, `~/.config/<agent-slug>/`, `./<agent-slug>/`, `./.${agent-slug}/` | Found | None found |

#### Step 2: Documentation search

Use web search to find:
- The agent's official hook/plugin/extension documentation
- How to register lifecycle callbacks
- Session/transcript storage format and location
- Configuration file format

#### Step 3: Config and session directory exploration

If a config directory was found:
1. List its contents (non-destructively)
2. Look for settings files (JSON, YAML, TOML)
3. Look for session/history directories
4. Look for transcript files and determine their format

#### Step 4: Map agent concepts to protocol subcommands

For each protocol subcommand, determine:
- Whether the agent has a native concept that maps to it
- How to implement it (which native API/config/file to use)
- Whether it's straightforward or requires workarounds

Create a mapping table:

| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | — | Static metadata, always implementable | Required |
| `detect` | — | Check for binary or config | Required |
| `get-session-id` | (agent's session ID mechanism) | ... | Required |
| `get-session-dir` | (agent's session directory) | ... | Required |
| `resolve-session-file` | (how agent stores sessions) | ... | Required |
| `read-session` | (session data format) | ... | Required |
| `write-session` | (session data persistence) | ... | Required |
| `read-transcript` | (transcript file location) | ... | Required |
| `chunk-transcript` | (raw bytes, language-generic) | Base64 chunking | Required |
| `reassemble-transcript` | (reverse of chunk) | Base64 reassembly | Required |
| `format-resume-command` | (agent's resume mechanism) | ... | Required |
| `parse-hook` | (agent's native hook events) | ... | If hooks capable |
| `install-hooks` | (agent's hook config format) | ... | If hooks capable |
| `uninstall-hooks` | (reverse of install) | ... | If hooks capable |
| `are-hooks-installed` | (check hook config) | ... | If hooks capable |
| `get-transcript-position` | (file size/position) | ... | If transcript analyzer |
| `extract-modified-files` | (parse transcript for file ops) | ... | If transcript analyzer |
| `extract-prompts` | (parse transcript for user msgs) | ... | If transcript analyzer |
| `extract-summary` | (parse transcript for summaries) | ... | If transcript analyzer |

#### Step 5: Select capabilities

Based on the mapping, determine which capabilities the binary should declare:
- `hooks` — Can the agent be configured to call external commands on lifecycle events?
- `transcript_analyzer` — Does the agent produce parseable transcripts?
- `transcript_preparer` — Does the transcript need pre-processing?
- `token_calculator` — Does the transcript contain token usage data?
- `text_generator` — Can the agent's LLM be invoked for text generation?
- `hook_response_writer` — Does the agent support writing messages back via hooks?
- `subagent_aware_extractor` — Does the agent spawn subagents with their own transcripts?

## Phase 3: Verification Script

Docs tend to be out of date. Before writing the one-pager, create a verification script that captures real payloads from the target agent to confirm Phase 2 findings.

Based on Phase 2 findings, create an **agent-specific** test script:

```
<PROJECT_DIR>/scripts/verify-<AGENT_SLUG>.sh
```

The script is tailored to the specific agent's hook mechanism (not a generic template). Adapt the hook wiring section based on what Phase 2 discovered.

**Script structure:**

```bash
#!/usr/bin/env bash
set -euo pipefail

AGENT_NAME="..."
AGENT_SLUG="..."
AGENT_BIN="..."
PROBE_DIR="<PROJECT_DIR>/.probe-${AGENT_SLUG}-$(date +%s)"
```

**Required sections:**

1. **Static checks** — Re-runnable binary/version/help checks from Phase 2
2. **Hook wiring** — Create workspace-local config that intercepts the agent's hooks and dumps stdin JSON to `$PROBE_DIR/captures/<event-name>-<timestamp>.json`. Use the agent's native hook config format discovered in Phase 2.
3. **Run modes:**
   - `--run-cmd '<cmd>'` — Automated: launch agent with a non-interactive prompt, wait, collect captures
   - `--manual-live` — Interactive: user runs agent manually, presses Enter when done
4. **Capture collection** — List and pretty-print all captured payload files
5. **Cleanup** — Restore original config (unless `--keep-config`)
6. **Verdict** — PASS/WARN/FAIL per lifecycle event + summary

**Important:** The hook wiring must be non-destructive — back up any existing config before modifying, and restore it on cleanup.

### Phase 3a: Execute & Analyze

Run the script and analyze the captured payloads:

1. **Execute**: `chmod +x <PROJECT_DIR>/scripts/verify-<AGENT_SLUG>.sh && <PROJECT_DIR>/scripts/verify-<AGENT_SLUG>.sh --manual-live`
2. **For each captured payload**: show the file path, decoded JSON, and which protocol subcommand it informs
3. **Update Phase 2 findings**: If captured payloads differ from documentation (field names, nesting, missing fields), update the mapping table with ground-truth values from the captures
4. **Lifecycle mapping**: native hook name → protocol event type, validated against real captures

If the script cannot be run (agent not installed, auth required, sandbox restrictions), follow the Blocker Handling procedure and note in the one-pager that findings are doc-based only and not verified.

## Phase 4: Write the One-Pager

Create `<PROJECT_DIR>/AGENT.md` (create the directory first if needed).

**Important:** Use ground-truth values from verification script captures (Phase 3a) wherever available. If a field was verified by real payloads, mark it as `(verified)`. If based only on docs/probing, mark it as `(unverified)`.

Template:

```markdown
# <AGENT_NAME> — External Agent Research

## Verdict: COMPATIBLE / PARTIAL / INCOMPATIBLE

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS/FAIL | path |
| Help available | PASS/FAIL | |
| Version info | PASS/FAIL | version string |
| Hook keywords | PASS/FAIL | keywords found |
| Session keywords | PASS/FAIL | keywords found |
| Config directory | PASS/FAIL | path |
| Documentation | PASS/FAIL | URL |

## Binary
- Name: `<agent-binary>`
- Version: ...
- Install: ... (how to install if not present)

## Hook Mechanism
- Config file: (exact path)
- Config format: JSON / YAML / TOML
- Hook registration: (how hooks are declared)
- Hook names and protocol mapping:
  | Native Hook Name | When It Fires | Protocol Event Type |
  |-----------------|---------------|---------------------|
  | ... | ... | 1=SessionStart, 2=TurnStart, etc. |
- Hook input format: (what data is passed to hooks — stdin, env vars, args)

## Session Management
- Session directory: (where sessions are stored)
- Session ID source: (how to extract from hook input or filesystem)
- Session file format: (JSON, JSONL, binary, etc.)

## Transcript
- Location: (path pattern)
- Format: JSONL / JSON array / other
- User prompt field: (which JSON field contains user prompts)
- Modified files field: (which JSON field contains file operations)
- Token usage field: (if available)
- Example entry: `{"role": "user", "content": "..."}`

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| ... | ... | ... | ... |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true/false | ... |
| transcript_analyzer | true/false | ... |
| transcript_preparer | true/false | ... |
| token_calculator | true/false | ... |
| text_generator | true/false | ... |
| hook_response_writer | true/false | ... |
| subagent_aware_extractor | true/false | ... |

## Gaps & Limitations
- ... (anything that doesn't map cleanly or requires workarounds)

## Captured Payloads
- Verification script: `<PROJECT_DIR>/scripts/verify-<AGENT_SLUG>.sh`
- Capture directory: `<PROJECT_DIR>/.probe-<AGENT_SLUG>-*/captures/`
- Verification status: VERIFIED (script ran, payloads captured) / UNVERIFIED (doc-based only)
- Notable differences from docs: ... (any discrepancies found between docs and real payloads)

## E2E Test Prerequisites
- Entire CLI binary: (how to obtain/path — default: `entire` from PATH or `E2E_ENTIRE_BIN` env)
- Agent CLI binary: (name, path, how to install)
- Non-interactive prompt command: (exact command + flags to send a prompt without interactive UI)
- Interactive mode: (supported? exact command to launch interactive session)
- Expected prompt pattern: (regex for tmux WaitFor — what the agent's prompt looks like)
- Timeout multiplier: (1.0 for fast agents, higher for slow APIs)
- Bootstrap steps: (any auth/setup needed before tests run)
- Transient error patterns: (strings that indicate retryable API errors, e.g., "overloaded", "rate limit", "503", "529")
```

Fill in every section with concrete values from the investigation. Don't leave placeholders. If a section doesn't apply, say so explicitly.

## Phase 5: Commit

Create a git commit for the `AGENT.md` file and the verification script.

## Blocker Handling

If blocked at any point (auth, sandbox, binary not found):

1. State the exact blocker
2. Provide the exact command for the user to run manually
3. Explain what output to paste back
4. Continue with provided output

## Constraints

- **No implementation code.** This phase produces only `AGENT.md` and the verification script.
- **Non-destructive.** All probing is read-only — don't modify agent config. The verification script backs up and restores any config it touches.
- **Verify before documenting.** Prefer ground-truth from captured payloads over documentation claims. Mark unverified findings explicitly.
- **Ask, don't assume.** If the hook mechanism is unclear, ask the user.
