---
name: implement
description: >
  Phase 3: Build the external agent binary using strict E2E-first TDD.
  Use /entire-external-agent implement or /entire-external-agent:implement
  when you only need the implementation phase.
---

# Implement Procedure

Build the external agent binary using strict E2E-first TDD. E2E tests drive development at every step — run each tier, watch it fail, implement the minimum fix, repeat. Unit tests are written only after all E2E tiers pass, using real data from E2E runs as golden fixtures.

> **Warning:** This phase involves iterative E2E test cycles with real agent invocations. Expect this to take 2-4 hours depending on agent complexity and API response times.

## Prerequisites

Ensure the following are available:
- `AGENT_NAME`, `AGENT_SLUG`, `LANGUAGE`, `PROJECT_DIR` — from orchestrator or user
- `<PROJECT_DIR>/AGENT.md` — research one-pager with E2E test prerequisites
- Scaffolded project that compiles and responds to `info`
- E2E test harness at `<PROJECT_DIR>/e2e/` that compiles

## Core Principle: E2E-First TDD

1. **E2E tests are the spec.** The `e2e/` test harness defines what "working" means. You implement until tests pass.
2. **Watch it fail first.** Every E2E tier starts by running the test and observing the failure. If you haven't seen the failure, you don't understand what needs fixing.
3. **Minimum viable fix.** At each failure, implement only the code needed to make that specific assertion pass. Don't anticipate future tiers.
4. **No unit tests during Steps 3-9.** Unit tests are written in Step 11 after all E2E tiers pass, using real data from E2E runs as golden fixtures.
5. **Format and lint, don't unit test.** Between E2E tiers, run format/lint to keep code clean. No unit tests between tiers.
6. **If you didn't watch it fail, you don't know if it tests the right thing.**

**Do NOT write unit tests during Steps 3-9.** All unit test writing is consolidated in Step 11.

## Procedure

### Step 1: Read Protocol Spec + AGENT.md

Read these files before writing any code:

1. Read `https://github.com/entireio/cli/blob/main/docs/architecture/external-agent-protocol.md` — full protocol spec
2. Read `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/types.go` — JSON response types
3. Read `https://github.com/entireio/cli/blob/main/cmd/entire/cli/agent/external/external.go` — how the CLI calls each subcommand
4. Read `<PROJECT_DIR>/AGENT.md` — agent-specific hook mechanism, transcript format, config structure, E2E prerequisites

### Step 2: Verify Baseline

Build the binary and run the first E2E test to confirm it fails for the right reason (agent behavior, not harness bug).

```bash
make build && make install
make test:e2e:run TEST=TestHookInstallAndDetect
```

**Expected:** Test fails because the agent binary returns stub data. If the test fails for a different reason (harness compilation error, missing binary, broken assertion), fix the harness first.

### Step 3: E2E Tier 1 — `TestHookInstallAndDetect`

**What it exercises:**
- `detect` — agent binary detection
- `install-hooks` — hook installation via `entire enable`
- `are-hooks-installed` — hook presence detection
- Basic binary invocation and JSON response format

**Cycle:**

1. Run: `make test:e2e:run TEST=TestHookInstallAndDetect`
2. **Watch it FAIL** — read the failure output carefully
3. Read the failure — what subcommand/behavior is missing?
4. Implement the MINIMUM code to fix the failure
5. Re-run until PASS
6. `make build`
7. Commit

### Step 4: E2E Tier 2 — `TestSingleSessionManualCommit`

The foundational test. Exercises the full agent lifecycle: start session → agent prompt → agent produces files → user commits → checkpoint created.

**What it exercises:**
- `parse-hook` for all event types (session start, turn start, turn end, session end)
- `get-session-id` — session ID extraction from hook input
- `get-session-dir` / `resolve-session-file` — finding session/transcript files
- `read-session` / `write-session` — session data management
- `read-transcript` / `chunk-transcript` / `reassemble-transcript` — transcript handling

**Cycle:**

1. Run: `make test:e2e:run TEST=TestSingleSessionManualCommit`
2. **Watch it FAIL** — read the failure output carefully
3. Read the failure — which subcommand returns wrong data or errors?
4. Implement the MINIMUM code to fix the failure
5. Re-run until PASS
6. `make build`
7. Commit

### Step 5: E2E Tier 3 — `TestCheckpointDeepValidation`

Validates transcript quality: JSONL validity, content hash correctness, prompt extraction accuracy.

**What it exercises:**
- `get-transcript-position` — transcript file size/position
- `extract-modified-files` — parsing transcript for file operations
- `extract-prompts` — parsing transcript for user messages
- `extract-summary` — parsing transcript for AI summaries

**Cycle:**

1. Run: `make test:e2e:run TEST=TestCheckpointDeepValidation`
2. **Watch it FAIL** — this test often exposes subtle transcript formatting bugs
3. Implement the MINIMUM fix
4. Re-run until PASS
5. `make build`
6. Commit

### Step 6: E2E Tier 4 — `TestMultipleTurnsManualCommit`

Multi-turn session management. Two sequential prompts, one commit.

**What it exercises:**
- Session persistence across multiple prompts
- Transcript accumulation across turns
- Checkpoint capturing both turns

**Cycle:**

1. Run: `make test:e2e:run TEST=TestMultipleTurnsManualCommit`
2. **Watch it FAIL**
3. Implement the MINIMUM fix
4. Re-run until PASS
5. `make build`
6. Commit

### Step 7: E2E Tier 5 — `TestSessionMetadata`

Agent identification in checkpoint metadata.

**What it exercises:**
- Session metadata has correct agent name
- Session ID is properly stored
- Agent type field is populated

**Cycle:**

1. Run: `make test:e2e:run TEST=TestSessionMetadata`
2. **Watch it FAIL**
3. Implement the MINIMUM fix
4. Re-run until PASS
5. `make build`
6. Commit

### Step 8: E2E Tier 6 — `TestInteractiveSession`

Tmux-based interactive mode. **Skip if the agent doesn't support interactive mode** (check AGENT.md's E2E prerequisites).

**What it exercises:**
- Interactive session launch
- Multi-step prompting within a session
- Session end on exit

**Cycle:**

1. Check AGENT.md — if interactive mode is not supported, skip this tier
2. Run: `make test:e2e:run TEST=TestInteractiveSession`
3. **Watch it FAIL**
4. Implement the MINIMUM fix
5. Re-run until PASS
6. `make build`
7. Commit

### Step 9: E2E Tier 7 — `TestRewind`

Rewind functionality after a checkpoint.

**What it exercises:**
- Rewind command works on checkpoints created by this agent
- State is properly restored after rewind

**Cycle:**

1. Run: `make test:e2e:run TEST=TestRewind`
2. **Watch it FAIL**
3. Implement the MINIMUM fix
4. Re-run until PASS
5. `make build`
6. Commit

### Step 10: Full E2E Suite Pass

Run the complete E2E suite to catch any regressions:

```bash
make test:e2e
```

This runs every test, not just the ones targeted in Steps 3-9.

**Important:** If some tests fail when running the full suite but pass individually, it may be a timing issue. Re-run each failing test individually before investigating:

```bash
make test:e2e:run TEST=TestFailingTestName
```

Fix any real failures before proceeding. The same cycle applies: read the failure, implement the minimum fix, re-run.

All E2E tests must pass before writing unit tests.

### Step 11: Write Unit Tests

Now that all E2E tiers pass, write unit tests to lock in behavior. Use real data from E2E runs (captured JSON payloads, transcript snippets, config file contents) as golden fixtures.

**Test files to create:**

1. **`cmd/hooks_test.go`** (or language equivalent) — Test `install-hooks` (creates config, idempotent), `uninstall-hooks` (removes hooks), `are-hooks-installed` (detects presence). Use a temp directory to avoid touching real config.

2. **`cmd/lifecycle_test.go`** — Test `parse-hook` for all event types. Use actual JSON payloads from E2E runs or AGENT.md examples. Test every event type mapping, null returns for unknown hook names, empty input, and malformed JSON.

3. **`cmd/session_test.go`** — Test session subcommands (`get-session-id`, `read-session`, `write-session`) with actual JSON payloads.

4. **`cmd/transcript_test.go`** — Test `read-transcript`, `chunk-transcript`, `reassemble-transcript` with sample data. Test transcript analyzer methods if implemented. Use transcript snippets from E2E runs as golden test data.

5. **`cmd/info_test.go`** — Test `info` returns valid JSON with correct fields and `detect` returns expected results.

**Where to find golden test data:**

- E2E artifact directories contain captured transcripts, hook payloads, and config files
- `AGENT.md` has example JSON payloads in the "Hook input" sections
- The agent's actual config file format from E2E test repos

Run: format + lint + test

**Commit:** Create a git commit for the unit tests.

### Step 12: Final Validation

Run the complete validation:

```bash
make build     # Build
make test      # Unit tests
make test:e2e  # E2E tests
```

Summarize:
- All E2E tiers passing (list which tests pass)
- Unit test coverage (number of test functions, what they cover)
- Any gaps or TODOs remaining
- Commands to build and install the binary

## Standing Instructions

- **Check AGENT.md first** for agent-specific information. If AGENT.md doesn't cover what you need, search external docs — but always update AGENT.md with anything new you discover.
- **Preserve unknown config keys** when modifying agent configuration files (read-modify-write pattern).
- **Validate JSON output** after each implementation — malformed JSON will cause the CLI to skip the agent.
- **Handle missing files gracefully** — return appropriate error messages to stderr rather than panicking.

## E2E Debugging Protocol

At every E2E failure, follow this protocol:

1. **Read the test output** — the assertion message often tells you exactly what's wrong
2. **Check the agent binary output** — run the failing subcommand manually with the same args/stdin
3. **Check Entire CLI logs** — look in the test repo's `.entire/logs/` directory
4. **Implement the minimum fix** — don't over-engineer; fix only what the test demands
5. **Re-run the failing test** — not the whole suite, just the one test

## Commit Strategy

After completing each tier:
1. Build and verify the binary
2. Run format and lint
3. Create a git commit describing which tier was completed
