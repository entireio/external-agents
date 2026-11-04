# Checkpoint — Track 3 Noon Curveball

**Project:** CodeTriage  
**Track:** BTW Buildathon Track 3 — custom Entire external agent  
**Scope:** `agents/entire-agent-codetriage/`  
**Prerequisite:** [Checkpoint 1](checkpoint_1.md) (pre-commit ESI gatekeeper)

## Invalidated assumption

Checkpoint 1 assumed every lifecycle payload arriving at `parse-hook` / `identify_modified_files` would be a **single Entire JSON object** (`hook_type`, `session_id`, `modified_files`, `tool_input`, …).

That is no longer true. Entire may now hand the agent a **heterogeneous AcmeCode JSONL transcript** (one JSON object per line) mixed with, or instead of, the original object. The commit gate must extract changed files from AcmeCode `file_changed` events without forking ESI, telemetry, or checkpoint writers.

## Entire Graph impact analysis (run before any code)

Commands (repo root):

```text
entire graph capabilities --json
entire graph search --repo . --profile full --query "identify_modified_files parse-hook commit transcript JSONL parser lifecycle summary checkpoint write session"
entire graph impact --repo . --symbol identify_modified_files --file agents/entire-agent-codetriage/src/entire_agent_codetriage/hooks.py
entire graph impact --repo . --symbol parse_hook --file agents/entire-agent-codetriage/src/entire_agent_codetriage/hooks.py
entire graph impact --repo . --symbol write_session --file agents/entire-agent-codetriage/src/entire_agent_codetriage/sessions.py
entire graph impact --repo . --symbol evaluate_commit --file agents/entire-agent-codetriage/src/entire_agent_codetriage/hooks.py
entire graph impact --repo . --symbol chunk_transcript --file agents/entire-agent-codetriage/src/entire_agent_codetriage/sessions.py
entire graph impact --repo . --symbol _write_git_commit_hook --file agents/entire-agent-codetriage/src/entire_agent_codetriage/hooks.py
```

### Blast radius of a transcript-format change

| Symbol | Role | Direct / transitive callers | Safe to change? |
| --- | --- | --- | --- |
| `identify_modified_files` | Parser: seed files for ESI | `evaluate_commit` → `parse_hook` / tests | **Yes — this is the adapter seam** |
| `parse_hook` | Lifecycle handler (`start` / `stop` / `commit`) | `_cmd_parse_hook`, hook tests | **Yes — decode stdin here** |
| `evaluate_commit` | ESI entry | `parse_hook`, commit tests | Touch only via file list, not BFS |
| `write_session` | Checkpoint / session persist | `_cmd_write_session` | **Do not change schema** |
| `chunk_transcript` / `reassemble_transcript` | Opaque byte slicing | CLI transcript helpers | **Do not change** (bytes in / bytes out) |
| `_write_git_commit_hook` | Git install path | `install_hooks` → CLI / tests | **Yes — audit fix** (`pre-commit`) |

**Not on the change list (preserve Checkpoint 1 behavior):**

- `compute_blast_radius` / `classify_esi` — Level 1 iff `depth >= 3` or `impacted_files >= 10`
- `log_esi_run` — MLflow fields unchanged
- `write_session` stored JSON (`session_id`, `native_data`, `modified_files`, `new_files`, `deleted_files`)
- Protocol `info` / `detect` / session helpers

Parser → lifecycle → summary → checkpoint data flow after the adapter:

```text
stdin (Entire JSON | AcmeCode JSONL)
        │
        ▼
adapt_raw / adapt_payload     ← new unified adapter (transcript.py)
        │  canonical dict: session_id, modified_files, tool_input, …
        ▼
parse_hook  ──start/stop──►  Event type 1 / 5 (summary)
        │
        └──commit──► identify_modified_files
                          │
                          ▼
                    evaluate_commit → compute_blast_radius (unchanged ESI)
                          │
                          ├─ Event metadata + optional response_message
                          └─ log_esi_run
```

`write_session` remains the checkpoint writer. Incomplete JSONL is never passed through `json.loads` as one document, so a truncated transcript cannot wipe or rewrite a session file.

## Design changes

### 1. Unified adapter (`transcript.py`)

One decode path, two surface formats:

- **Original Entire JSON:** `json.loads` of the whole blob still wins when the input is one object (or a JSON array of objects). Existing fields are copied through. `modified_files` / `tool_input` / `raw_data` keep working.
- **AcmeCode JSONL:** if the blob is not one JSON value, each line is parsed independently. `file_changed` events (also `type: file_changed`) contribute paths from `path`, `file_path`, `file`, `data.path`, etc. Those paths are lifted into `modified_files`.

`identify_modified_files` and `parse_hook --hook commit` both call this adapter, then the **existing** ESI / Event / telemetry code. There is no second blast-radius implementation.

### 2. Resilience

Unknown objects (`telemetry`, `mystery`, strings, numbers, arrays) are skipped. `json.loads` failures on a line do not abort the rest of the transcript and never raise into the CLI.

### 3. Incomplete / truncated JSONL

A truncated last line is dropped; every prior valid object is kept. `parse_hook` still emits a protocol Event (partial `session_id` / `modified_files`) instead of returning `null` and discarding the turn.

### 4. Git hook audit fix

`install-hooks` now writes **`.git/hooks/pre-commit`** (Git’s real hook name). The previous `pre-commit-codetriage` file was never invoked by Git. Legacy leftovers are removed on install. Uninstall deletes `.git/hooks/pre-commit` only when the file is the CodeTriage script.

Re-running `install-hooks` when `.codetriage/hooks.json` already exists still refreshes the Git hook so existing installs pick up the filename fix (protocol still reports `hooks_installed: 0` for the idempotent path).

## Why the new result is safe

1. **ESI math is byte-identical.** `blast_radius.py` was not edited. The adapter only changes *which seed paths* are fed into the same BFS.
2. **Original fixtures still pass.** A single Entire JSON object is decoded as one record; Checkpoint 1 commit-rejection tests (`modified_files: ["core.py"]` + 3-hop reverse chain → ESI Level 1) remain green.
3. **Checkpoints are not rewritten by parse failures.** `write_session` / `chunk_transcript` are unchanged. Truncated JSONL cannot `json.loads`-fail the whole stdin into a session clobber; bad lines are skipped.
4. **Unknown events cannot crash the binary.** The protocol CLI already swallows unexpected exceptions; the adapter additionally refuses to raise on per-line JSON errors.
5. **Git actually runs the gate.** The hook path matches Git’s contract, so `pre-commit` can fire. Empty stdin still maps to `null` (Checkpoint 1 `parse_hook("", …)` behavior).

## Tests

`cd agents/entire-agent-codetriage && python -m pytest -q` — **26 passed**.

Explicit curveball coverage in `tests/test_transcript.py`:

| Test | Requirement |
| --- | --- |
| `test_original_entire_lifecycle_format` | Original Entire JSON |
| `test_acmecode_jsonl_file_changed_events` | AcmeCode JSONL `file_changed` |
| `test_unknown_json_events_are_skipped` | Unknown events ignored |
| `test_truncated_jsonl_returns_partial_result` | Truncated JSONL → partial Event |
| `test_jsonl_file_changed_still_triggers_esi_level_1` | ESI Level 1 unchanged on JSONL seeds |

Also: `test_install_writes_git_pre_commit_hook`, `test_parse_hook_accepts_acmecode_jsonl`.
