# E2E Tests

End-to-end tests for external agents, exercising the full lifecycle: agent prompts, git hooks, checkpoints, and rewind.

## Structure

```
e2e/
├── agents/           # Agent interface + implementations (kiro, etc.)
│   ├── agent.go      # Agent/Session interfaces, registry, concurrency gating
│   ├── tmux.go       # TmuxSession for interactive PTY sessions
│   └── kiro.go       # Kiro agent (kiro-cli-chat)
├── entire/           # `entire` CLI wrapper
│   └── entire.go     # Enable, Disable, RewindList, Rewind
├── testutil/         # Shared test infrastructure
│   ├── metadata.go   # Checkpoint/session metadata types
│   ├── artifacts.go  # Artifact capture (git-log, pane, logs)
│   ├── repo.go       # RepoState, SetupRepo, ForEachAgent, Git helpers
│   └── assertions.go # Test assertions (testify-based)
├── bootstrap/        # Pre-test agent bootstrap (CI auth setup)
│   └── main.go       # go run ./e2e/bootstrap
├── setup_test.go     # TestMain: build agents, artifact dir, preflight
├── kiro_lifecycle_test.go  # Lifecycle tests (ForEachAgent pattern)
├── kiro_test.go      # Protocol-level tests (stdin/stdout subcommands)
├── harness.go        # AgentRunner for protocol tests
├── testenv.go        # TestEnv for protocol tests
└── fixtures.go       # HookInput, KiroTranscript builders
```

## Running Tests

### All E2E tests (protocol + lifecycle)

```bash
make test-e2e
```

### Lifecycle tests only

```bash
make test-e2e-lifecycle
```

### Single test

```bash
cd e2e && go test -tags=e2e -v -count=1 -run TestLifecycle_SinglePromptManualCommit ./...
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `E2E_AGENT` | Filter to a single agent (e.g. `kiro`). Default: all registered agents. |
| `E2E_ENTIRE_BIN` | Path to `entire` binary. Falls back to `$PATH` lookup. |
| `E2E_ARTIFACT_DIR` | Override artifact output directory. |
| `E2E_KEEP_REPOS` | Set to any value to preserve temp repos after tests. |
| `E2E_REQUIRE_LIFECYCLE` | Set to `1` to fail (not skip) when lifecycle deps are missing. |
| `E2E_CONCURRENT_TEST_LIMIT` | Override per-agent concurrency limit (default: 2 for kiro). |

## Adding a New Agent

1. Create `e2e/agents/<name>.go` implementing the `Agent` interface.
2. In `init()`, conditionally register based on `E2E_AGENT` env var.
3. Call `RegisterGate("<name>", N)` to set concurrency limit.
4. If it's an external agent, implement `ExternalAgent` interface.

## Debugging Failures

After a test run, check `e2e/artifacts/<timestamp>/` for:

- `git-log.txt` — full git history including checkpoint branch
- `git-tree.txt` — file tree at HEAD and checkpoint branch tip
- `console.log` — all agent prompts, outputs, and git commands
- `pane.txt` — final tmux pane content (interactive tests)
- `PASS` / `FAIL` — test outcome marker
- `checkpoint-metadata/` — checkpoint and session metadata JSON
- `entire-logs/` — entire CLI debug logs

Set `E2E_KEEP_REPOS=1` to preserve the temp git repo (symlinked from artifact dir).
