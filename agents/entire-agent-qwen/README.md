# entire-agent-qwen

External agent binary that teaches the Entire CLI how to work with Qwen Code.

## Capabilities

| Capability | Status |
|------------|--------|
| hooks | Yes, command hooks in `.qwen/settings.json` for Qwen Code lifecycle, tool, compact, notification, permission, and subagent events |
| transcript_analyzer | Yes, via Entire sidecar JSONL |
| compact_transcript | Yes, emits Entire compact transcript JSONL |
| transcript_preparer | No |
| token_calculator | No |

## Installation

```bash
cd agents/entire-agent-qwen
mise run build
cp entire-agent-qwen /usr/local/bin/
```

Qwen Code itself must also be installed as `qwen` for real sessions:

```bash
npm install -g @qwen-code/qwen-code
```

## Enable In A Repo

```bash
cd /path/to/repo
entire enable --agent qwen --telemetry=false
```

This writes command hooks to `.qwen/settings.json`. Hooks call:

```bash
entire hooks qwen <hook-name>
```

The hook commands are wrapped so a missing `entire` binary never breaks a Qwen session. Existing user hook fields such as `env`, `statusMessage`, HTTP hook `url`, `headers`, `allowedEnvVars`, and unknown future Qwen fields are preserved when Entire installs or uninstalls its entries.

## Usage

```bash
qwen -p "Create hello.txt with hello world" --yolo
git add .
git commit -m "qwen checkpoint test"
entire checkpoint list
```

Entire stores Qwen sidecar transcripts in a repo-scoped OS temp directory such as `/tmp/entire-qwen/<repo-hash>/<session_id>.jsonl`, preserving Qwen's native `transcript_path` as metadata. A small `.entire/tmp/<session_id>.json` marker is also written so Entire's shared session persistence tooling can discover the session without making the transcript path collide with other external agents.

The adapter records Qwen's supported hook payload shapes, including `tool_input`, `inputs`, `input`, `tool_response`, `response`, and subagent/permission metadata. Local models must still make Qwen Code execute real tools; if a model only prints XML-style tool tags as text, Qwen Code will not fire `PostToolUse` and there is no file change for Entire to checkpoint.

## Development

```bash
mise run build
mise run test
external-agents-tests verify ./entire-agent-qwen

cd ../../
QWEN_E2E=1 E2E_AGENT=qwen mise run test:e2e:lifecycle
```
