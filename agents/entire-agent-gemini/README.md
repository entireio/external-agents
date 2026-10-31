# entire-agent-gemini

External agent binary that teaches the Entire CLI how to work with [Gemini CLI](https://github.com/google-gemini/gemini-cli).

## Capabilities

| Capability          | Status                                                        |
| ------------------- | ------------------------------------------------------------ |
| hooks               | Yes, command hooks in `.gemini/settings.json`                |
| transcript_analyzer | Yes, via Entire sidecar JSONL                                |
| compact_transcript  | Yes, emits Entire compact transcript JSONL                   |
| transcript_preparer | No                                                           |
| token_calculator    | No                                                           |

## Installation

```bash
cd agents/entire-agent-gemini
go build -o entire-agent-gemini ./cmd/entire-agent-gemini
cp entire-agent-gemini /usr/local/bin/
```

Or use mise:

```bash
cd agents/entire-agent-gemini
mise run build
```

## Prerequisites

- [Gemini CLI](https://github.com/google-gemini/gemini-cli) installed as `gemini` on `PATH`.
- Gemini CLI v0.26.0+ (hooks support required).
- `GEMINI_API_KEY` or Google account authentication configured for Gemini CLI.

```bash
npm install -g @anthropic-ai/gemini-cli
# or
npm install -g @google/gemini-cli
```

Set your API key:
```bash
export GEMINI_API_KEY=your-api-key-here
```

## Enable In A Repo

```bash
cd /path/to/repo
entire enable --agent gemini --telemetry=false
```

This writes command hooks to `.gemini/settings.json`. Hooks call:

```bash
entire hooks gemini <hook-name>
```

The hook commands are wrapped so a missing `entire` binary never breaks a Gemini session. Existing user hook fields are preserved when Entire installs or uninstalls its entries.

## Hook Events

| Gemini CLI Event | Protocol Event     | Type | Description                         |
| ---------------- | ------------------ | ---- | ----------------------------------- |
| SessionStart     | SessionStart       | 1    | Session begins (startup, resume)    |
| BeforeAgent      | TurnStart          | 2    | User submits prompt, before planning |
| AfterAgent       | TurnEnd            | 3    | Agent loop ends                     |
| BeforeTool       | (recorded)         | -    | Before a tool executes              |
| AfterTool        | (recorded)         | -    | After a tool executes               |
| PreCompress      | PreCompact         | 4    | Before context compression          |
| Notification     | (recorded)         | -    | System notification                 |
| SessionEnd       | SessionEnd         | 5    | Session ends                        |

## Usage

```bash
gemini "Create hello.txt with hello world"
git add .
git commit -m "gemini checkpoint test"
entire checkpoint list
```

Entire stores Gemini sidecar transcripts in a repo-scoped OS temp directory such as `/tmp/entire-gemini/<repo-hash>/<session_id>.jsonl`. A small `.entire/tmp/<session_id>.json` marker is also written so Entire's shared session persistence tooling can discover the session.

## Development

```bash
mise run build
mise run test
external-agents-tests verify ./entire-agent-gemini

cd ../../
E2E_AGENT=gemini mise run test:e2e:lifecycle
```

## Environment Variables

| Variable             | Description                                              |
| -------------------- | -------------------------------------------------------- |
| `GEMINI_API_KEY`     | API key for Gemini (set by user for Gemini CLI)          |
| `GEMINI_SESSION_ID`  | Session ID (provided by Gemini CLI to hooks)             |
| `GEMINI_CWD`         | Current working directory (provided by Gemini CLI)      |
| `GEMINI_PROJECT_DIR` | Project root directory (provided by Gemini CLI)          |
