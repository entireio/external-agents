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
- **Transcript preparer** — Devin writes its transcript when a session run
  ends, so the preparer polls for the post-Stop flush and materializes a
  minimal ATIF stub for mid-session checkpoints (the complete transcript is
  captured by the first condensation after the session ends)
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
- Checkpoints condensed mid-session carry a stub or previous-run transcript;
  the first condensation after the session run ends captures the complete one.
- Devin loads `.claude/settings.json` hooks by default. If Entire is enabled
  for Claude Code in the same repo, Devin sessions will also fire the
  claude-code hooks; set `read_config_from.claude` to `false` in
  `.devin/config.json` to avoid this until the CLI-side guard lands.
