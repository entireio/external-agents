# Amp — External Agent Research

## Verdict: COMPATIBLE

Amp has a TypeScript plugin system with lifecycle events for `agent.start` and `agent.end`. Project plugins require launching Amp with `PLUGINS=all` in classic Amp clients. Amp threads are server-side, so the integration uses plugin hooks to discover the active thread ID and then retrieves the authoritative transcript with `amp threads export <thread-id>` directly from the `session.start` and `agent.end` hooks.

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `amp` found locally |
| Help available | PASS | `amp --help` |
| Version info | PASS | `amp --version` |
| Hook keywords | PASS | Plugin docs provide lifecycle events |
| Session keywords | PASS | `threads continue`, `threads export` |
| Config directory | PASS | `~/.config/amp` documented |
| Documentation | PASS | https://ampcode.com/manual/plugins.md and https://ampcode.com/manual/plugin-api.md |

## Binary
- Name: `amp`
- Install: Amp CLI from AmpCode distribution.
- Plugin loading: run `PLUGINS=all amp ...`.

## Hook Mechanism
- Config file: `.amp/plugins/entire.ts`
- Config format: TypeScript plugin running on Amp's Bun-provided runtime.
- Hook registration: plugin default export receives `PluginAPI`, registers handlers with `amp.on(event, handler)`.
- Hook names and protocol mapping:

| Native Hook Name | When It Fires | Protocol Event Type |
|------------------|---------------|---------------------|
| synthetic `session.start` | before first `agent.start` per thread | 1 = SessionStart |
| `agent.start` | user submits a prompt | 2 = TurnStart |
| `agent.end` | agent finishes a turn | 3 = TurnEnd |

`session.start` from Amp itself does not always include `thread.id`, so the plugin does not rely on it for session identity.

## Session Management
- Session ID source: Amp `thread.id`.
- Session directory: `.entire/tmp/amp/`.
- Session file format: exported Amp thread JSON, parsed as `AmpThread` from `internal/amp/types.go`. The binary writes this file on `session.start` and refreshes it on `agent.end`.
- Resume mechanism: `PLUGINS=all amp threads continue <thread-id>`.

## Transcript
- Authoritative storage: `session_ref` always contains the exported Amp thread JSON byte-for-byte (the output of `amp threads export <thread-id>`). This JSON is the session's native data and the input for all transcript operations.
- Retrieval: the binary runs `amp threads export <thread-id>` during the `session.start` and `agent.end` plugin hooks and writes the result to `session_ref`. `prepare-transcript --session-ref <path>` is available as a refresh — it parses the existing prepared transcript to recover `thread.id` and re-exports.
- Authoritative parser: `internal/amp/types.go` models the exported transcript as `AmpThread`, `ThreadMessage`, `ThreadContentBlock`, `ThreadToolRun`, and `ThreadUsage`.
- User prompt extraction: `AmpThread.Messages[].Content[]` text blocks on `role: "user"` messages.
- Assistant summary extraction: last non-empty text block on `role: "assistant"` messages.
- Modified file extraction: `ThreadToolRun.TrackFiles`, tool input path fields (`path`, `filePath`, `filepath`, `file`, `absolutePath`, `paths`, `files`), and common tool result `absolutePath` fields.
- Token usage extraction: `ThreadMessage.Usage` on exported messages.
- Unprepared behavior: transcript analyzer, compact transcript, token calculation, and read-session operations require exported `AmpThread` JSON and return an error if called on a `session_ref` that has not yet been populated by an export.

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|----------------|----------------------|-------------|
| `info` | static metadata | Return name `amp`, capabilities | Required |
| `detect` | `amp` binary | Check `command -v amp` | Required |
| `get-session-id` | Amp thread ID | Read from hook input | Required |
| `get-session-dir` | `.entire/tmp` | Standard Entire temp dir | Required |
| `resolve-session-file` | `.entire/tmp/<id>.json` | Standard path resolution | Required |
| `read-session` | exported Amp JSON | Validate and parse `AmpThread`; use `AmpThread.ID`, raw JSON `native_data`, and extracted modified files | Required |
| `write-session` | exported Amp JSON | Write native data to session ref | Required |
| `read-transcript` | exported Amp JSON | Return raw bytes after validating `AmpThread` JSON | Required |
| `chunk-transcript` | raw bytes | Base64 chunking via protocol JSON encoding | Required |
| `reassemble-transcript` | chunks | Reassemble raw bytes | Required |
| `format-resume-command` | Amp thread continue | `PLUGINS=all amp threads continue <id>` | Required |
| `parse-hook` | plugin event JSON | Map hook payloads to protocol events; export Amp thread on `session.start` and `agent.end` | Hooks |
| `install-hooks` | project plugin | Write `.amp/plugins/entire.ts` | Hooks |
| `uninstall-hooks` | project plugin | Remove `.amp/plugins/entire.ts` | Hooks |
| `are-hooks-installed` | project plugin | Check plugin marker | Hooks |
| `get-transcript-position` | exported Amp JSON | Validate `AmpThread` and return byte length | Transcript analyzer |
| `extract-modified-files` | exported Amp JSON | Parse `AmpThread` tool inputs/results and `trackFiles` | Transcript analyzer |
| `extract-prompts` | exported Amp JSON | Parse user text blocks from `AmpThread.Messages` | Transcript analyzer |
| `extract-summary` | exported Amp JSON | Parse last assistant text block from `AmpThread.Messages` | Transcript analyzer |
| `prepare-transcript` | `amp threads export` | Re-export the Amp thread using the thread ID from the existing prepared transcript at `session_ref` | Transcript preparer |
| `compact-transcript` | exported Amp JSON | Emit Entire compact JSONL from `AmpThread` | Compact transcript |
| `calculate-tokens` | exported Amp JSON | Sum `ThreadMessage.Usage` token fields | Token calculator |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | Amp plugins expose lifecycle events |
| transcript_analyzer | true | Exported Amp JSON contains prompts, tool calls/results, and assistant messages |
| compact_transcript | true | Exported Amp JSON maps to Entire compact transcript format |
| transcript_preparer | true | Exports Amp thread JSON into the session ref |
| token_calculator | true | Exported Amp JSON contains `ThreadMessage.usage` |
| text_generator | false | Amp CLI is used for agent execution, not summary generation |
| hook_response_writer | false | Not needed for lifecycle hooks |
| subagent_aware_extractor | false | No documented subagent transcript tree |

## Gaps & Limitations
- Amp must be launched with `PLUGINS=all` for hooks to fire.
- All transcript operations require the prepared `amp threads export` JSON at `session_ref`. The binary triggers this export from the `session.start` and `agent.end` plugin hooks; operations called before either of those hooks fires will return an error.
- `session.start` cannot be trusted for thread identity because `thread.id` is optional there.

## E2E Test Prerequisites
- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN`.
- Agent CLI binary: `amp`.
- Non-interactive prompt command: `PLUGINS=all amp --dangerously-allow-all --no-notifications --no-ide -x '<prompt>'`.
- Interactive mode: `PLUGINS=all amp`.
- Expected prompt pattern: `>`.
- Timeout multiplier: 1.5.
