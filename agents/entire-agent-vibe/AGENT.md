# Mistral Vibe External Agent Research

## Verdict: COMPATIBLE

Mistral Vibe Code (VS Code extension / Vibe CLI) persists a per-session recovery journal on disk, and its native project hooks (`.vibe/hooks.toml`) map onto Entire lifecycle events.

## Binary

- Entire external-agent binary: `entire-agent-vibe`
- Runtime: Python 3.8+, standard library only (no packages)
- Entire agent name: `vibe`

## Hook Mechanism

Vibe declares hooks in `.vibe/hooks.toml`. `install-hooks` appends a managed block:

```toml
[[hooks]]
name = "entire-turn-end"
type = "post_agent"
command = "entire hooks vibe post_agent"
```

The hook payload (session_id, parent_session_id, transcript_path, cwd) is piped to `parse-hook`, which maps `post_agent` to Entire's TurnEnd event (type 3) so the CLI stores a checkpoint.

## Session Storage

- Sessions: `~/.vibe/logs/session/unified/<session-uuid>/journal/*.jsonl`
- Index: `~/.vibe/logs/session/unified.session-index.json`
- Records: `projection_delta` ops (`append_entry` / `replace_entry` / `remove_entry`) carry the canonical conversation; `core_input`, `action_intent`, `action_result`, `callback_*` and `receipt_*` are machinery.

The journal is a rolling window: Vibe compacts and deletes older chunks.

## Protocol Mapping

The single script implements the full protocol command set (`info`, `detect`, `get-session-id`, `get-session-dir`, `resolve-session-file`, `read-session`, `write-session`, `read-transcript`, `prepare-transcript`, `chunk-transcript`, `reassemble-transcript`, `format-resume-command`, `get-transcript-position`, `extract-prompts`, `extract-modified-files`, `extract-summary`, `compact-transcript`, `calculate-tokens`, `parse-hook`, `install-hooks`, `uninstall-hooks`, `are-hooks-installed`).

## Timing Model

The `post_agent` hook fires BEFORE the turn's final assistant message is flushed to the journal. `prepare-transcript` therefore waits for journal quiescence (size stable for 0.5 s, 4 s max). `compact-transcript` merges the prepared Markdown (full history) with the live journal (complete recent tail), because the CLI's temp transcript can miss the latest turn's final message while the journal can lack early turns.

## E2E Strategy

Verified end-to-end against Mistral Vibe Code 1.27.0 and Entire CLI 0.12.2: multi-turn sessions produce complete transcripts (each turn's final response included), per-turn token deltas, and git-confirmed file changes on the dashboard. CI runs the unit suite against synthetic journals with a temp `HOME`; no Vibe installation and no LLM credentials are needed.
