# Lifecycle Tests

End-to-end lifecycle tests for external agents. This harness covers the behaviors that only make sense against the real Entire CLI and the real agent CLI: `entire enable`, prompt execution, hook installation, checkpoint creation, rewind, and interactive sessions.

Generic protocol compliance is no longer in this directory. Those checks run from [`entireio/external-agents-tests`](https://github.com/entireio/external-agents-tests) and are wired into this repo through GitHub Actions.

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
├── build.go          # Agent discovery + binary builds for lifecycle runs
├── setup_test.go     # TestMain: build agents, artifact dir, preflight
└── lifecycle_test.go # Shared lifecycle scenarios (ForEachAgent pattern)
```

## Running Tests

### All lifecycle tests

```bash
make test-e2e
```

### Explicit lifecycle target

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
| `E2E_CONCURRENT_TEST_LIMIT` | Override per-agent concurrency limit (default: 2 for kiro). |

## Adding a New Agent

1. Create `e2e/agents/<name>.go` implementing the `Agent` interface.
2. In `init()`, conditionally register based on `E2E_AGENT` env var.
3. Call `RegisterGate("<name>", N)` to set concurrency limit.
4. If it's an external agent, implement `ExternalAgent` so `SetupRepo` can pre-enable external agents in Entire settings.
5. Keep generic protocol validation out of this directory. Add any reusable black-box protocol coverage to `external-agents-tests` instead.

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
