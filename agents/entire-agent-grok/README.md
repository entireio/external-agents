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

> **Trust is required, and failure is silent.** Until the folder is trusted Grok skips
> `.grok/hooks/entire.json` without any warning, so Entire captures nothing and no
> checkpoints appear. If a session produced no checkpoints, check trust first.

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

## Session Restore And Resume

Entire can restore a Grok session onto another machine, or back onto this one after the
local session directory is gone. `entire session resume <branch>` writes
`chat_history.jsonl` and a `summary.json` back into
`~/.grok/sessions/<encoded-repo-cwd>/<session-id>/`, and the session can then be continued
with:

```bash
grok --resume <session-id>
```

Two limitations are worth knowing before relying on it.

**Resume is not full fidelity.** Grok records its reasoning state in an
`encrypted_content` blob on every `reasoning` line. Entire strips that field before storing
the transcript, so a resumed session replays the conversation, tool calls and results, but
without Grok's prior reasoning context. Conversation recall is unaffected in testing; the
effect on long or complex threads has not been characterised.

The field is stripped for two reasons. It is large: a reasoning-heavy session produced
values up to 12 KB each, around 15% of the transcript, and the transcript is re-stored on
every checkpoint. And Entire's redactor treats it as a secret because it is high-entropy
base64, corrupting it on restore and leaving a session Grok refuses to replay at all
("This session's conversation history is incompatible with the current model"). Dropping
the field avoids both problems, and Grok replays correctly without it. This mirrors what
Entire already does for the built-in Codex agent.

**Sessions captured before this behaviour shipped cannot be resumed.** Their stored
transcripts already contain redaction-corrupted `encrypted_content`. Redaction is
irreversible, so those sessions must be started fresh.

Prefer `grok --resume <session-id>` over `grok --continue`. `--continue` picks whichever
session Grok saw most recently in that directory, which may be a different one, and it
resumes it without complaining.

## Development

```bash
mise run build
mise run test
external-agents-tests verify ./entire-agent-grok

cd ../../
GROK_E2E=1 E2E_AGENT=grok mise run test:e2e:lifecycle
```