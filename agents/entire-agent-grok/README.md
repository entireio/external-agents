# entire-agent-grok

External agent binary that teaches the Entire CLI how to work with Grok Build.

## Capabilities

| Capability | Status |
|------------|--------|
| hooks | Yes, command hooks in `.grok/hooks/entire.json` for Grok lifecycle, tool, compact, notification, permission, and subagent events |
| transcript_analyzer | Yes, reads Grok native `chat_history.jsonl` under `~/.grok/sessions/` |
| compact_transcript | Yes, compacts native Grok chat history for Entire storage |
| transcript_preparer | No |
| token_calculator | No |

## Installation

```bash
cd agents/entire-agent-grok
mise run build
cp entire-agent-grok ~/.local/bin/
```

Grok Build itself must also be installed as `grok` for real sessions.

## Enable In A Repo

```bash
cd /path/to/repo
# external_agents must be enabled in .entire/settings.json
entire enable --agent grok --telemetry=false
```

This writes command hooks to `.grok/hooks/entire.json`. Hooks call:

```bash
entire hooks grok <hook-name>
```

Hook commands are wrapped so a missing `entire` binary never breaks a Grok session. Existing user hook entries in `entire.json` are preserved when Entire installs or uninstalls its entries.

Project hooks require Grok folder trust. Run `/hooks-trust` (or launch with `--trust`) before hooks execute.

## Usage

```bash
grok "Create hello.txt with hello world"
git add .
git commit -m "grok checkpoint test"
entire checkpoint list
```

Entire resolves transcripts from Grok's native session store:

`~/.grok/sessions/<encoded-repo-cwd>/<session-id>/chat_history.jsonl`

A small `.entire/tmp/<session_id>.json` marker is also written so Entire's shared session persistence tooling can discover the session.

## Development

```bash
mise run build
mise run test
external-agents-tests verify ./entire-agent-grok

cd ../../
GROK_E2E=1 E2E_AGENT=grok mise run test:e2e:lifecycle
```