# entire-agent-omp

External agent binary that adds Oh My Pi (`omp`) support to Entire CLI.

## Requirements

- `entire` on `PATH` with external-agent discovery enabled.
- `omp` on `PATH`. This adapter is validated against `omp` 17.1.1.

## Installation

Using mise:

```bash
cd agents/entire-agent-omp
mise run build
cp entire-agent-omp ~/.local/bin/
```

Using Go directly:

```bash
cd agents/entire-agent-omp
go build -o entire-agent-omp ./cmd/entire-agent-omp
cp entire-agent-omp ~/.local/bin/
```

## Enable in a Repository

Set `external_agents` in `.entire/settings.json`:

```json
{
  "external_agents": true
}
```

Then enable Oh My Pi:

```bash
entire enable --agent omp --telemetry=false
```

Entire installs the project extension at `.omp/extensions/entire/index.ts`. `omp` loads this extension automatically when started from that repository.

## Usage

```bash
omp -p "Create hello.txt with hello world" --approval-mode=yolo
git add hello.txt
git commit -m "add hello.txt"
entire checkpoint list
```

Interactive `omp` sessions use the same extension. Entire can resume a recorded session through `omp --resume <session-id>`; an empty session ID maps to `omp --continue`.

## Behavior

| Capability | Behavior |
|---|---|
| `hooks` | Maps `omp` initial load, session switch/branch, the first actual `agent_start`, and the final `agent_end` to Entire session start, turn start, and turn end events. The preceding prompt is captured from `before_agent_start`; automatic continuations remain within the open Entire turn. |
| `transcript_analyzer` | Reads `omp` JSONL, follows the active parent chain, extracts user prompts, the latest assistant text, and paths from `write`, `edit`, and `apply_patch` tool calls. |
| `compact_transcript` | Emits Entire v1 compact JSONL with user text, assistant text, tool calls/results, and available per-message token counts. |
| `uses_terminal` | Supports print and interactive `omp` sessions. |

At turn end, the adapter copies the native transcript to `<omp project session directory>/.entire/<session-id>.jsonl`. The copy is private and stable while Entire records the turn. The source must be a regular `.jsonl` file directly inside the project session directory managed by `omp`.

The installed extension also exports `GIT_TERMINAL_PROMPT=0` for `omp` bash tool calls unless the command already sets it, preventing unattended Git commands from waiting for terminal credentials.

## Limitations

- `omp --no-extensions` disables Entire lifecycle capture.
- Explicit `omp --session-dir` paths are not accepted for turn-end snapshots. Use the default `omp` directory, `PI_CONFIG_DIR`, profile/`PI_CODING_AGENT_DIR`, or active XDG-managed session directory.
- Modified-file analysis does not infer paths changed indirectly by arbitrary shell commands.
- `omp` event and session formats are versioned interfaces. Versions other than 17.1.1 require compatibility verification.
