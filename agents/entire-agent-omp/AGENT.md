# Oh My Pi Integration Contract

## Compatibility Target

| Interface | Target |
|---|---|
| `omp` CLI and extension API | 17.1.1 |
| `omp` session format | JSONL version 3, with legacy header-first and current title-slot-first physical layouts |
| Entire external-agent protocol | 1 |
| Entire lifecycle validation | 0.8.42 on Linux |

Compatibility claims below are limited to these versions and the tagged `omp` sources listed under Evidence.

## Native Interface

- Product: Oh My Pi; official short name and binary: `omp`.
- Print mode: `omp -p <prompt>`.
- Unattended approval: `--approval-mode=yolo`.
- Resume: `omp --resume <session-id>`; continue the latest terminal session with `omp --continue`.
- Project extensions: `<cwd>/.omp/extensions`; a directory entry resolves through `index.ts` before `index.js`.
- Extension factory: a default-exported function receiving `ExtensionAPI` from `@oh-my-pi/pi-coding-agent`.

### Lifecycle Mapping

| `omp` extension event | Native guarantee used | Entire event |
|---|---|---|
| `session_start` | Initial session load; session manager and project cwd are available | type 1, SessionStart |
| `session_switch` | `/new`, resume, fork, or handoff completed | type 1, SessionStart after draining the previous turn |
| `session_branch` | Branch navigation completed | type 1, SessionStart after draining the previous turn |
| `before_agent_start` | Pre-send prompt notification; may be superseded before execution | no protocol event; caches the pending prompt |
| `agent_start` | Agent loop actually begins; repeats from an automatic continuation are skipped | type 2, TurnStart with the pending prompt |
| `agent_end` | Emitted after `omp` decides whether the settled turn will continue; `willContinue: true` is skipped | type 3, TurnEnd, synchronously forwarded under hook name `agent_end` |
| `session_shutdown` | Session cleanup | no protocol event; clears adapter-local identity cache |

`omp` 17.1.1 can supersede a `before_agent_start` notification before the agent loop begins, so the adapter waits for `agent_start` before opening the Entire turn. Intermediate settles carry `willContinue: true`; they and repeated start events from automatic continuations stay within the existing turn. The final settle is forwarded synchronously so print-mode shutdown cannot discard the Entire TurnEnd event. Session switches and branches use the same reset path as initial loading, with any previous TurnEnd completed before the new SessionStart.

The generated extension reads session identity through `ctx.sessionManager.getSessionId()` and the native transcript path through `ctx.sessionManager.getSessionFile()`. Hook subprocess failures are contained and never abort `omp`.

## Session Storage

Default root:

```text
$HOME/${PI_CONFIG_DIR:-.omp}/agent/sessions/<encoded-cwd>/
```

Overrides:

- `PI_CONFIG_DIR=<config-dir>` changes the config directory relative to home.
- `PI_CODING_AGENT_DIR=<agent-dir>` selects the absolute-resolved `<agent-dir>/sessions` and normally takes precedence over XDG.
- `OMP_PROFILE=<name>` selects `$HOME/${PI_CONFIG_DIR:-.omp}/profiles/<name>/agent/sessions`; `PI_PROFILE` is the fallback when `OMP_PROFILE` is unset. An empty value or `default` selects the default profile.
- On Linux and macOS, an existing `$XDG_DATA_HOME/omp` selects its `sessions` directory for the default profile. A named profile instead requires and selects `$XDG_DATA_HOME/omp/profiles/<name>/sessions`. Windows does not activate XDG roots.

`omp` canonicalizes cwd before deriving `<encoded-cwd>`:

- inside home: `-<relative-path>`;
- inside the OS temporary root: `-tmp-<relative-path>`;
- elsewhere: `--<absolute-path-without-leading-separator>--`.

For every form, `/`, `\\`, and `:` inside the encoded portion are replaced by `-`; the encoding is one directory name, not a nested path. Home itself is `-`, and the temporary root itself is `-tmp`.

Native filenames are `<timestamp>_<session-id>.jsonl`. Session ID lookup therefore scans direct `.jsonl` files and compares the parsed header ID rather than assuming the ID is the filename.

### Physical JSONL Layout

`omp` 17.1.1 may write:

1. a `type: "title"` slot exactly 256 UTF-8 bytes including its newline;
2. a `type: "session"` header containing `version`, `id`, `timestamp`, and `cwd`;
3. append-only entries carrying `id`, `parentId`, `timestamp`, and an entry-specific payload.

Legacy files can begin directly with the session header. The adapter accepts both layouts. Conversation state is a tree: the active transcript starts at the last valid entry, follows `parentId` to the root, then reverses that chain. File order alone does not define the active branch.

Message entries use roles `user`, `assistant`, and `toolResult`. Assistant content can contain `text`, `thinking`, and `toolCall` blocks. Tool calls carry `id`, `name`, and `arguments`; tool results refer back through `toolCallId`.

At TurnEnd, the adapter copies the source to:

```text
<managed omp project session directory>/.entire/<session-id>.jsonl
```

The adapter rejects a source unless it resolves to a regular direct-child `.jsonl` file under the computed `omp` project session directory, and rejects a destination that is a symlink or not a directory. These same-user path checks are not a race-resistant filesystem boundary. Directory and file modes are restricted to `0700` and `0600`. A failed validation or copy emits TurnEnd without `session_ref` or `model`; it never exposes an unstable source path.

## Protocol Mapping

| Subcommand | `omp` mapping |
|---|---|
| `info` | Name `omp`; protected directory `.omp`; protected file `.omp/extensions/entire/index.ts` |
| `detect` | `omp` lookup on `PATH` |
| `get-session-id` | Hook `session_id` |
| `get-session-dir` | `omp`-managed project session directory |
| `resolve-session-file` | Header-ID scan, then safe `<session-id>.jsonl` fallback |
| `read-session` | Header metadata plus byte-for-byte native JSONL; opaque protocol data remains readable |
| `write-session` | Atomic private write of `native_data` to `session_ref` |
| `read-transcript` | Raw `session_ref` bytes |
| `chunk-transcript` | Size-bounded raw byte chunks |
| `reassemble-transcript` | Exact byte concatenation |
| `format-resume-command` | `omp --resume <quoted-id>` or `omp --continue` |
| `parse-hook` | Lifecycle table above |
| `install-hooks` | Atomic install of `.omp/extensions/entire/index.ts`; foreign content is preserved unless forced |
| `uninstall-hooks` | Remove only a marker-owned extension |
| `are-hooks-installed` | Marker check on the owned extension |
| `get-transcript-position` | Physical JSONL line count |
| `extract-modified-files` | Active-branch `write`, `edit`, and `apply_patch` tool paths after the physical-line offset |
| `extract-prompts` | Active-branch user text after the physical-line offset |
| `extract-summary` | Last non-empty active-branch assistant text |
| `compact-transcript` | Base64-wrapped Entire v1 compact JSONL with user text, assistant text, tool calls/results, and available input/output usage |

## Declared Capabilities

| Capability | Value | Basis |
|---|---:|---|
| `hooks` | true | Project extension lifecycle events |
| `transcript_analyzer` | true | Native JSONL contains prompts, assistant text, tool calls, and paths |
| `compact_transcript` | true | Native messages map to Entire compact JSONL |
| `uses_terminal` | true | `omp` supports print and interactive terminal modes |
| `transcript_preparer` | false | Native transcript already exists on disk |
| `token_calculator` | false | No separate aggregate-token protocol implementation |
| `text_generator` | false | `omp` execution remains outside the adapter protocol |
| `hook_response_writer` | false | Lifecycle forwarding needs no `omp` response channel |
| `subagent_aware_extractor` | false | No declared external-agent subagent transcript contract |

## Limits

- `--no-extensions` prevents lifecycle forwarding.
- Explicit `--session-dir` paths fall outside the managed-directory proof and produce TurnEnd without a transcript reference.
- Modified-file extraction covers explicit mutating tool calls. It does not infer arbitrary shell side effects.
- The adapter ignores thinking/image/custom blocks in compact output unless they contain supported user or assistant text.
- `omp` schema or lifecycle changes after 17.1.1 require renewed compatibility verification.

## Evidence

- `omp` 17.1.1 extension API: <https://github.com/can1357/oh-my-pi/blob/v17.1.1/docs/extensions.md>
- `omp` 17.1.1 extension discovery: <https://github.com/can1357/oh-my-pi/blob/v17.1.1/docs/extension-loading.md>
- `omp` 17.1.1 session model: <https://github.com/can1357/oh-my-pi/blob/v17.1.1/docs/session.md>
- `omp` 17.1.1 lifecycle routing: <https://github.com/can1357/oh-my-pi/blob/v17.1.1/packages/coding-agent/src/session/agent-session.ts>
- `omp` 17.1.1 lifecycle event contracts: <https://github.com/can1357/oh-my-pi/blob/v17.1.1/packages/coding-agent/src/extensibility/shared-events.ts>
- `omp` 17.1.1 directory roots: <https://github.com/can1357/oh-my-pi/blob/v17.1.1/packages/utils/src/dirs.ts>
- `omp` 17.1.1 cwd encoding: <https://github.com/can1357/oh-my-pi/blob/v17.1.1/packages/coding-agent/src/session/session-paths.ts>
- `omp` 17.1.1 fixed title slot: <https://github.com/can1357/oh-my-pi/blob/v17.1.1/packages/coding-agent/src/session/session-title-slot.ts>
