# Implement Procedure

Replace stub implementations with real logic for each subcommand, working in dependency order.

## Prerequisites

Ensure the following are available:
- `AGENT_NAME`, `AGENT_SLUG`, `LANGUAGE`, `PROJECT_DIR` — from orchestrator or user
- `<PROJECT_DIR>/AGENT.md` — research one-pager
- Scaffolded project that compiles and responds to `info`

## Implementation Order

Subcommands are organized into tiers by dependency. Implement each tier fully before moving to the next.

| Tier | Subcommands | Description |
|------|------------|-------------|
| 1 | `info`, `detect` | Identity and detection |
| 2 | `get-session-id`, `get-session-dir`, `resolve-session-file`, `read-session`, `write-session` | Session core |
| 3 | `read-transcript`, `chunk-transcript`, `reassemble-transcript`, `format-resume-command` | Transcript and resume |
| 4 | `parse-hook`, `install-hooks`, `uninstall-hooks`, `are-hooks-installed` | Hooks capability |
| 5 | `get-transcript-position`, `extract-modified-files`, `extract-prompts`, `extract-summary` | Transcript analysis |
| 6 | Remaining optional capabilities | As declared |

## Per-Subcommand Cycle

For each subcommand:

### Step 1: Read the spec

Read the protocol spec section for this subcommand:
- Read the relevant section of `https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md`
- Read how `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/external.go` calls it (what args it passes, what stdin format, what it expects on stdout)
- Read the response type from `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/types.go`

### Step 2: Read agent-specific notes

Check `<PROJECT_DIR>/AGENT.md` for:
- How the agent's native concept maps to this subcommand
- Specific file paths, formats, or APIs to use
- Known gaps or workarounds

### Step 3: Implement real logic

Replace the stub with a working implementation. Follow these guidelines:

- **Parse all arguments** — use the language's flag/arg parser
- **Read stdin when required** — parse JSON or raw bytes as specified
- **Return the exact JSON schema** — field names, types, and structure must match `types.go`
- **Handle errors** — write error messages to stderr and exit non-zero
- **Use env vars** — read `ENTIRE_REPO_ROOT` and `ENTIRE_PROTOCOL_VERSION` from the environment

### Step 4: Manual test

Test each subcommand manually:

```bash
# Subcommands with no stdin:
./entire-agent-<slug> info
./entire-agent-<slug> detect
./entire-agent-<slug> get-session-dir --repo-path /tmp/test-repo

# Subcommands with JSON stdin:
echo '{"hook_type":"stop","session_id":"test-123","session_ref":"/tmp/transcript.jsonl","timestamp":"2026-01-01T00:00:00Z"}' | ./entire-agent-<slug> get-session-id
echo '{"hook_type":"stop","session_id":"test-123","session_ref":"/tmp/transcript.jsonl","timestamp":"2026-01-01T00:00:00Z"}' | ./entire-agent-<slug> read-session

# Subcommands with raw bytes stdin:
echo 'raw transcript content' | ./entire-agent-<slug> chunk-transcript --max-size 1024

# Verify JSON output:
./entire-agent-<slug> info | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin), indent=2))"
```

### Step 5: Verify JSON schema

Check that the output matches the expected schema:
- All required fields are present
- Field types are correct (string, int, bool, array, object)
- Optional fields use the correct zero values or are omitted

## Tier-Specific Guidance

### Tier 1: `info` and `detect`

**`info`:** Already returns valid JSON from scaffolding. Update with real values:
- `name` — the agent's registry name (what `entire enable` uses)
- `type` — human-readable agent type name
- `description` — one-line description
- `protected_dirs` — directories the agent uses that shouldn't be touched (e.g., `.cursor`)
- `hook_names` — list of native hook names the agent supports
- `capabilities` — match what AGENT.md declared

**`detect`:** Check whether the agent is available:
- Look for the agent binary on `$PATH`
- Check for the agent's config directory
- Return `{"present": true}` or `{"present": false}`

### Tier 2: Session Core

**`get-session-id`:** Parse HookInput JSON from stdin, extract `session_id` field.

**`get-session-dir`:** Return the directory where the agent stores sessions. Use `--repo-path` arg and the agent's session directory convention from AGENT.md.

**`resolve-session-file`:** Given `--session-dir` and `--session-id`, return the path to the session's transcript file.

**`read-session`:** Parse HookInput from stdin, construct and return an AgentSession JSON object. Use AGENT.md for how to populate each field.

**`write-session`:** Read AgentSession JSON from stdin. Persist it as needed (or no-op if the agent doesn't support session writing). Exit 0 on success.

### Tier 3: Transcript and Resume

**`read-transcript`:** Read the file at `--session-ref` and write raw bytes to stdout.

**`chunk-transcript`:** Read raw bytes from stdin, split into chunks of at most `--max-size` bytes, base64-encode each chunk, return JSON `{"chunks": [...]}`.

**`reassemble-transcript`:** Read JSON `{"chunks": [...]}` from stdin, base64-decode each chunk, concatenate, write raw bytes to stdout.

**`format-resume-command`:** Return the command to resume the agent with `--session-id`. Use the agent's native resume mechanism from AGENT.md.

### Tier 4: Hooks (if `hooks` capability declared)

**`parse-hook`:** This is the most complex subcommand. It must:
1. Read the `--hook` argument (native hook name)
2. Read raw hook payload from stdin
3. Map the native hook to a protocol Event type:
   - 1 = SessionStart
   - 2 = TurnStart
   - 3 = TurnEnd
   - 4 = Compaction
   - 5 = SessionEnd
   - 6 = SubagentStart
   - 7 = SubagentEnd
4. Parse agent-specific fields from the payload into the Event JSON
5. Return the Event JSON, or `null` if the hook is irrelevant

Use AGENT.md's "Hook Mechanism" section for the hook name → Event type mapping.

**`install-hooks`:** Configure the target agent to invoke `entire hooks <agent-name> <hook-verb>` on lifecycle events. Read AGENT.md for the config file format and location. Handle `--local-dev` and `--force` flags. Return `{"hooks_installed": N}`.

**`uninstall-hooks`:** Remove the hooks installed by `install-hooks`. Exit 0 on success.

**`are-hooks-installed`:** Check if hooks are currently installed. Return `{"installed": true/false}`.

### Tier 5: Transcript Analysis (if `transcript_analyzer` capability declared)

**`get-transcript-position`:** Return the byte size of the transcript file at `--path`. Return `{"position": <size>}`.

**`extract-modified-files`:** Parse the transcript at `--path` starting from `--offset` bytes. Extract file paths that were modified by the agent. Return `{"files": [...], "current_position": <pos>}`.

**`extract-prompts`:** Parse the transcript at `--session-ref` starting from `--offset` bytes. Extract user prompt strings. Return `{"prompts": [...]}`.

**`extract-summary`:** Parse the transcript at `--session-ref`. Look for AI-generated summaries. Return `{"summary": "...", "has_summary": true/false}`.

### Tier 6: Remaining Capabilities

For each remaining declared capability, implement the corresponding subcommands following the same per-subcommand cycle. Reference the protocol spec for exact schemas.

## Standing Instructions

- **Check AGENT.md first** for agent-specific information. If AGENT.md doesn't cover what you need, search external docs — but always update AGENT.md with anything new you discover.
- **Preserve unknown config keys** when modifying agent configuration files (read-modify-write pattern).
- **Validate JSON output** after each implementation — malformed JSON will cause the CLI to skip the agent.
- **Handle missing files gracefully** — return appropriate error messages to stderr rather than panicking.

## Commit Strategy

After completing each tier:
1. Build and test all subcommands in the tier
2. Run `mise run fmt && mise run lint` (if applicable to the language)
3. Create a git commit

## Output

After all tiers are implemented, summarize:
- Subcommands implemented (list each with a brief note)
- Manual test results (which subcommands produce correct output)
- Any subcommands that need further work
- Commands to build and test the binary
