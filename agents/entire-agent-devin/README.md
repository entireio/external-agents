# entire-agent-devin

Entire external agent for [Devin CLI](https://docs.devin.ai/cli) ("Devin for
Terminal", Cognition). Lets Entire capture checkpoints, transcripts, prompts,
and token usage from Devin CLI sessions.

## Capabilities

- **Hooks** — installs Claude Code-format lifecycle hooks into
  `.devin/hooks.v1.json` (`SessionStart`, `UserPromptSubmit`, `Stop`,
  `SessionEnd`)
- **Transcript analysis** — parses Devin's canonical ATIF transcript
  (`~/.local/share/devin/cli/transcripts/<session_id>.json`): step positions,
  modified files from `write`/`edit`/`notebook_edit` tool calls, user prompts
- **Transcript preparer** — polls for the canonical ATIF file; if Devin has
  not flushed it yet, reads `~/.local/share/devin/cli/sessions.db`
  (`message_nodes`) and materializes a live ATIF transcript, falling back to
  a minimal stub only when both sources are unavailable
- **Token calculation** — per-step `metrics` (prompt/completion/cached
  tokens; fresh input = prompt − cached)
- **Compact transcripts** — converts ATIF steps (messages, tool calls, and
  observation results) into Entire Transcript Format for `transcript.jsonl`

See [AGENT.md](AGENT.md) for the live-verified behavior notes.

## Setup

```bash
cd agents/entire-agent-devin
mise run build
cp entire-agent-devin /usr/local/bin/
```

Enable external agent discovery and Devin in your repo:

```bash
cd /path/to/your/repo
# .entire/settings.json needs: {"external_agents": true}
entire enable --agent devin --telemetry=false
devin -p "Create hello.txt with hello world" --permission-mode accept-edits
```

Resume a captured session with `devin -r <session_id>` (session IDs are
word-pairs like `snowy-efraasia`).

## Known limitations

- Devin resumes conversations from its own SQLite store, so `entire rewind`
  restores the transcript file but not Devin's conversation memory;
  cross-machine resume requires the session to exist locally.
- Checkpoints condensed mid-session now read Devin's live SQLite session
  store before falling back to a stub. The fallback is only used when the
  local `devin` CLI is not logged in or the session is not in the local
  `sessions.db`.
- Devin loads `.claude/settings.json` hooks by default. If Entire is enabled
  for Claude Code in the same repo, Devin sessions will also fire the
  claude-code hooks; set `read_config_from.claude` to `false` in
  `.devin/config.json` to avoid this until the CLI-side guard lands.
