# Amp — External Agent Research

## Verdict: COMPATIBLE

Amp has a TypeScript plugin system with lifecycle events for `agent.start` and `agent.end`. Project plugins require launching Amp with `PLUGINS=all`. The public plugin API does not expose a stable native transcript file path, so the integration captures event/message payloads into `.entire/tmp/amp/*.jsonl` and implements transcript analysis from that cache.

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
- Session file format: JSONL cache written by the external agent binary from hook payloads.
- Resume mechanism: `PLUGINS=all amp threads continue <thread-id>`.

## Transcript
- Bootstrap location: `.entire/tmp/amp/<safe-thread-id>.jsonl` before preparation.
- Authoritative location: the same `session_ref` after `prepare-transcript` overwrites it with `amp threads export <thread-id>` JSON.
- Authoritative format: `AmpThread` JSON modeled in `internal/amp/types.go`.
- User prompt field: `AmpThread.messages[].content[]` text blocks on `role: "user"` messages.
- Modified files field: `ThreadToolRun.trackFiles`, tool input path fields, and common tool result path fields.
- Token usage field: `ThreadMessage.usage` on exported assistant messages.

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|----------------|----------------------|-------------|
| `info` | static metadata | Return name `amp`, capabilities | Required |
| `detect` | `amp` binary | Check `command -v amp` | Required |
| `get-session-id` | Amp thread ID | Read from hook input | Required |
| `get-session-dir` | `.entire/tmp` | Standard Entire temp dir | Required |
| `resolve-session-file` | `.entire/tmp/<id>.json` | Standard path resolution | Required |
| `read-session` | exported Amp JSON | Build AgentSession from authoritative `AmpThread` JSON | Required |
| `write-session` | exported Amp JSON | Write native data to session ref | Required |
| `read-transcript` | exported Amp JSON | Return raw bytes after validating `AmpThread` JSON | Required |
| `chunk-transcript` | raw bytes | Base64 chunking via protocol JSON encoding | Required |
| `reassemble-transcript` | chunks | Reassemble raw bytes | Required |
| `format-resume-command` | Amp thread continue | `PLUGINS=all amp threads continue <id>` | Required |
| `parse-hook` | plugin event JSON | Map hook payloads to protocol events | Hooks |
| `install-hooks` | project plugin | Write `.amp/plugins/entire.ts` | Hooks |
| `uninstall-hooks` | project plugin | Remove `.amp/plugins/entire.ts` | Hooks |
| `are-hooks-installed` | project plugin | Check plugin marker | Hooks |
| `get-transcript-position` | exported Amp JSON | Return byte length | Transcript analyzer |
| `extract-modified-files` | exported Amp JSON | Parse `AmpThread` tool inputs/results | Transcript analyzer |
| `extract-prompts` | exported Amp JSON | Parse user text blocks from `AmpThread.messages` | Transcript analyzer |
| `extract-summary` | exported Amp JSON | Parse last assistant text block from `AmpThread.messages` | Transcript analyzer |
| `prepare-transcript` | `amp threads export` | Export server-side thread JSON into `session_ref` | Transcript preparer |
| `compact-transcript` | exported Amp JSON | Emit Entire compact JSONL from `AmpThread` | Compact transcript |

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
- Transcript data starts as plugin-captured JSONL only for bootstrap thread ID discovery and is later prepared into authoritative `amp threads export` JSON.
- `session.start` cannot be trusted for thread identity because `thread.id` is optional there.

## E2E Test Prerequisites
- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN`.
- Agent CLI binary: `amp`.
- Non-interactive prompt command: `PLUGINS=all amp --dangerously-allow-all --no-notifications --no-ide -x '<prompt>'`.
- Interactive mode: `PLUGINS=all amp`.
- Expected prompt pattern: `>`.
- Timeout multiplier: 1.5.
