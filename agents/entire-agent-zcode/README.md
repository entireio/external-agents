# entire-agent-zcode

External agent binary integrating [ZCode](https://zcode.z.ai) — the Z.ai agentic
development environment — with the Entire CLI.

ZCode is an Electron desktop app with no headless CLI, so this integration is
read-oriented: sessions and transcripts are read from ZCode's local SQLite
store (`~/.zcode/cli/db/db.sqlite`), and lifecycle events arrive through
ZCode's documented hook system (user-level `~/.zcode/cli/config.json`).

## Capabilities

| Capability | Status |
|-----------|--------|
| `hooks` | ✅ SessionStart / UserPromptSubmit / Stop (+ compaction via `source=compact`) |
| `transcript_analyzer` | ✅ position, modified files, prompts, summary |
| `transcript_preparer` | ✅ exports sessions from SQLite to `<repo>/.entire/tmp/zcode/<id>.jsonl` |
| `token_calculator` | ✅ sums per-message input/output/cache usage |
| `text_generator` | ❌ no headless model invocation |
| `hook_response_writer` | ❌ not implemented in v1 |
| `subagent_aware_extractor` | ❌ deferred (ZCode tracks subagents via `session.parent_id`) |

## Build & test

```bash
mise run build   # → ./entire-agent-zcode
mise run test    # unit tests
```

The binary is stateless and depends only on the `sqlite3` CLI (used read-only)
being available for transcript export.

## Environment

- `ZCODE_HOME` — override ZCode's state root (default `~/.zcode`); useful for
  tests and fixture environments. The hook config is expected at
  `<ZCODE_HOME>/cli/config.json` and the session DB at
  `<ZCODE_HOME>/cli/db/db.sqlite`.

## Notes & limitations

- `install-hooks` edits the **user-level** config because ZCode ignores
  workspace-level hook configs for security. Only entries installed by this
  binary are touched; everything else in the file is preserved.
- Hook config changes are snapshotted per session by ZCode — start a new ZCode
  session after `entire enable --agent zcode`.
- `format-resume-command` returns plain `zcode`; resuming a specific session
  happens through the app's session list.
- `write-session` stores Entire's snapshot under `.entire/tmp/`; ZCode's own
  database is never written to.

See [AGENT.md](AGENT.md) for the full research notes and protocol mapping.
