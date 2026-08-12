# entire-agent-hermes

Preview external-agent integration for [Hermes Agent](https://hermes-agent.nousresearch.com/) and Entire CLI.

The adapter installs an embedded observer plugin into an explicitly selected `HERMES_HOME`. The plugin maintains a minimal repo registration and writes repository-keyed sanitized JSONL without reading Hermes `state.db`, credentials, memory, or native sessions.

## Build

```bash
mise run build
```

## Enable in a repository

`HERMES_HOME` is mandatory. The adapter intentionally refuses to install into an implicit default profile.

```bash
export HERMES_HOME=/absolute/path/to/a/hermes/profile
export PATH="/path/to/external-agents/agents/entire-agent-hermes:$PATH"

cd /path/to/repo
mkdir -p .entire
printf '{"external_agents":true}\n' > .entire/settings.json
entire enable --agent hermes --telemetry=false
```

Installation adds only `entire-observer` to the profile's plugin enablement and registers the current canonical repo. Repeating the command in another repo adds a second registration. Removing Hermes from one repo leaves the plugin and other repo registrations intact. Restart a running Hermes gateway after installation, update, or final removal because its process-wide plugin manager does not rescan plugins dynamically.

## Safety model

The observer retains only:

- the current sanitized user prompt;
- the current sanitized final assistant response;
- tool name and coarse status;
- normalized repo-relative modified-file paths.

It never stores system/developer prompts, memory, conversation history, platform or sender identifiers, environment dumps, secrets, tool arguments, or raw tool results. It inspects only allowlisted top-level path-bearing tool fields in memory and resolves them by strict canonical containment to the longest registered repository match. Process CWD alone never selects a repository, so pathless tools cannot attribute gateway activity to an unrelated checkout. Before a repository-scoped tool runs, it synchronously forwards the buffered SessionStart and TurnStart events; that lifecycle forwarding activates the Entire session without recursively running `entire enable`. One Hermes turn may project into multiple repositories, while each transcript retains only that repository's tool/file evidence.

Transcript file APIs accept production reads and writes only beneath the explicit `$HERMES_HOME/entire/transcripts` root. The only additional read surface is the adapter-owned `testdata` directory used by shared protocol compliance.

## Protocol compliance

```bash
mise run verify
```

The task builds the adapter, creates an isolated `HERMES_HOME`, and runs the installed shared compliance runner from a temporary directory outside this Go module. It also verifies that the shared mandatory tests ran, preventing a false pass caused by the runner selecting the caller's `go.mod`.

## Verification

```bash
scripts/verify-hermes.sh
scripts/verify-hermes.sh --run-cmd 'hermes --yolo --ignore-rules -z "Create hello.txt"'
```

The script refuses paths below the real `~/.hermes` tree. A live command still needs credentials supplied independently to the disposable profile; no profile files are copied.

## Lifecycle E2E

Live E2E registration is deliberately opt-in and requires an explicit disposable home:

```bash
tmp_home="$(mktemp -d)"
HERMES_E2E=1 HERMES_HOME="$tmp_home" E2E_AGENT=hermes mise run test:e2e:lifecycle
```

An unconfigured temp profile has no model credentials, so compilation can be verified everywhere while live model execution may remain blocked.
