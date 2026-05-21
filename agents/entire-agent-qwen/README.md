# entire-agent-qwen

External agent binary that teaches the Entire CLI how to work with Qwen Code.

## Capabilities

| Capability | Status |
|------------|--------|
| hooks | Yes, command hooks in `.qwen/settings.json` |
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

The hook commands are wrapped so a missing `entire` binary never breaks a Qwen session.

## Usage

```bash
qwen -p "Create hello.txt with hello world" --yolo
git add .
git commit -m "qwen checkpoint test"
entire checkpoint list
```

Entire stores Qwen session metadata in `.entire/tmp/qwen/<session_id>.jsonl`, preserving Qwen's native `transcript_path` as metadata.

## Development

```bash
mise run build
mise run test
external-agents-tests verify ./entire-agent-qwen

cd ../../
QWEN_E2E=1 E2E_AGENT=qwen mise run test:e2e:lifecycle
```
