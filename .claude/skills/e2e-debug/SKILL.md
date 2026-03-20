# Debug External Agent E2E Failures

Diagnose external agent bugs using captured artifacts from the E2E test suite. Artifacts are written to `e2e/artifacts/` locally or downloaded from CI via GitHub Actions.

## Inputs

The user provides either:
- **A test run directory:** `e2e/artifacts/{timestamp}/` -- triage all failures
- **A specific test directory:** `e2e/artifacts/{timestamp}/{TestName}-{agent}/` -- debug one test

## Test Categories

This repo has two kinds of tests:

| Category | File | What it tests |
|----------|------|---------------|
| **Protocol tests** | `e2e/kiro_test.go` | Individual agent subcommands (`info`, `detect`, `get-session-id`, `parse-hook-event`, etc.) via `AgentRunner` |
| **Lifecycle tests** | `e2e/kiro_lifecycle_test.go` | Full workflows: prompt execution → git commit → checkpoint creation, using `entire` CLI + agent CLI |

Protocol tests run against the agent binary directly. Lifecycle tests require both the `entire` CLI and the agent's underlying CLI (e.g., `kiro-cli-chat`).

## Artifact Layout

```
e2e/artifacts/{timestamp}/
├── entire-version.txt          # CLI version under test
└── {TestName}-{agent}/         # e.g., TestLifecycle_SinglePromptManualCommit-kiro
    ├── PASS or FAIL            # Status marker
    ├── console.log             # Full operation transcript
    ├── pane.txt                # Tmux pane capture (interactive session tests only)
    ├── git-log.txt             # git log --decorate --graph --all
    ├── git-tree.txt            # ls-tree HEAD + entire/checkpoints/v1 branch
    ├── entire-logs/entire.log  # CLI structured JSON logs from .entire/logs/
    ├── checkpoint-metadata/    # Checkpoint + session metadata
    │   └── {first-2-chars}/{remaining-10-chars}/
    │       ├── metadata.json   # Checkpoint-level metadata
    │       └── 0/
    │           └── metadata.json   # Session 0 metadata
    └── repo -> /tmp/...        # Symlink to preserved repo (E2E_KEEP_REPOS=1 only)
```

## Preserved Repo

When the test run was executed with `E2E_KEEP_REPOS=1`, each test's artifact directory contains a `repo` symlink pointing to the preserved temporary git repository. This is the actual repo the test operated on -- you can inspect it directly.

**Navigate via the symlink** (e.g., `{artifact-dir}/repo/`) rather than resolving the `/tmp/...` path. The symlink lives inside the artifact directory so permissions and paths stay consistent.

The preserved repo contains:
- Full git history with all branches (main, `entire/checkpoints/v1`)
- The `.entire/` directory with CLI state, config, and raw logs
- All files the agent created or modified, in their final state

This is the most powerful debugging tool -- you can run `git log`, `git diff`, `git show`, inspect `.entire/` internals, and see exactly what the CLI left behind.

## Debugging Workflow

### 1. Triage (if given a run directory)

List the test subdirectories and check for `FAIL` markers. Read the Go test output or `console.log` files to identify failures and their error messages.

### 2. Read console.log (most important)

Full transcript of every operation:
- `> kiro-cli-chat chat --no-interactive ...` -- agent prompts with stdout/stderr
- `> git add/commit/...` -- git commands
- `> send: ...` -- interactive session inputs (tmux-based tests)

This tells you what happened chronologically.

### 3. Read test source code

Find the failing test in `e2e/kiro_lifecycle_test.go` (lifecycle tests) or `e2e/kiro_test.go` (protocol tests). Understand what the test expected vs what console.log shows actually happened.

Key test infrastructure to understand:
- `testutil.ForEachAgent()` -- runs the test for each registered agent with timeout
- `testutil.SetupRepo()` -- creates temp git repo, runs `entire enable`, sets up artifact capture
- `s.RunPrompt()` -- sends a prompt to the agent and captures output
- `testutil.WaitForCheckpoint()` -- polls until checkpoint branch advances
- `testutil.CaptureArtifacts()` -- called during t.Cleanup(), writes all artifacts

### 4. Diagnose the issue

Cross-reference console.log (what happened) with the test (what should have happened). Determine whether the issue is in the agent, the CLI, or the test itself:

| Symptom | Investigation |
|---------|---------------|
| Agent subcommand returns wrong output | Protocol test: check `AgentRunner` invocation, compare expected vs actual JSON |
| Agent prompt fails / produces wrong files | Check `console.log` for agent stderr, verify agent CLI is available |
| Checkpoint not created / timeout | Check `entire-logs/entire.log` for hook invocations, phase transitions, errors |
| Wrong checkpoint content | Check `git-tree.txt` for checkpoint branch files, `checkpoint-metadata/` for session info |
| Hooks didn't fire | Check `entire-logs/entire.log` for missing hook entries (session-start, user-prompt-submit, stop, post-commit) |
| Detection fails | Check agent `detect` subcommand output, verify expected files exist in repo |
| Session ID wrong | Check `get-session-id` output, verify `.entire/` session state |
| Attribution issues | Check `checkpoint-metadata/` for `files_touched`, session metadata for attribution data |
| Strategy mismatch | Check `entire-logs/entire.log` for `strategy` field, verify auto-commit vs manual-commit behavior |
| Interactive session hangs | Check `pane.txt` for tmux capture, look for agent waiting on input |

### 5. Deep dive files

- **console.log**: Chronological transcript of all operations. Most important file for understanding what happened.
- **pane.txt**: Tmux pane capture for interactive session tests. Shows the terminal state at artifact capture time.
- **entire-logs/entire.log**: Structured JSON logs -- hook lifecycle, session phases (`active` -> `idle` -> `ended`), warnings, errors. Key fields: `component`, `hook`, `strategy`, `session_id`.
- **git-log.txt**: Commit graph showing main branch, `entire/checkpoints/v1`, checkpoint initialization.
- **git-tree.txt**: Files at HEAD vs checkpoint branch (separated by `--- entire/checkpoints/v1 ---`).
- **checkpoint-metadata/**: `metadata.json` has `checkpoint_id`, `strategy`, `files_touched`, `token_usage`, and `sessions` array. Session subdirs have per-session details including `agent`, `transcript_path`, and `initial_attribution`.

### 6. Report findings

Identify whether the issue is in:
- **Agent binary** (protocol compliance, subcommand output, detection logic)
- **Agent CLI** (kiro-cli-chat behavior, prompt execution, file creation)
- **Entire CLI hooks** (prepare-commit-msg, commit-msg, post-commit)
- **Session management** (phase transitions, session tracking, session IDs)
- **Checkpoint creation** (branch management, metadata writing, content hash)
- **Attribution** (file tracking, prompt correlation, transcript capture)
- **Test harness** (testutil assertions, timing, environment setup)
