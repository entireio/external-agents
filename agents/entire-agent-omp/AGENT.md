# Oh My Pi (omp) — External Agent Research

## Verdict: COMPATIBLE

Oh My Pi has a rich TypeScript extension system with lifecycle hooks, JSONL session storage with full transcript content (including tool calls, token usage, and actual assistant responses), and a non-interactive print mode. All necessary protocol subcommands can be implemented.

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `omp` (verified) |
| Help available | PASS | `omp --help` (verified) |
| Hook keywords | PASS | `extension` / `hook` found in help (verified) |
| Session keywords | PASS | `session`, `resume`, `continue` found (verified) |
| Config directory | PASS | `~/.omp/agent` (verified) |
| Documentation | PASS | https://github.com/can1357/oh-my-pi |

## Binary
- Name: `omp`
- Runtime: Node.js
- Package: `@oh-my-pi/pi-coding-agent` on npm
- Install: `npm install -g @oh-my-pi/pi-coding-agent`

## Key Differences from Pi

| Aspect | pi | omp |
|--------|----|----|
| Binary name | `pi` | `omp` |
| npm package | `@mariozechner/pi-coding-agent` | `@oh-my-pi/pi-coding-agent` |
| Config root | `~/.pi/agent` | `~/.omp/agent` |
| Extension dir (project) | `.pi/extensions/` | `.omp/extensions/` |
| Extension dir (global) | `~/.pi/agent/extensions/` | `~/.omp/agent/extensions/` |
| Protected dir | `.pi` | `.omp` |
| Session IDs | UUIDs | Snowflake hex IDs |
| Non-interactive flags | `--no-skills --no-prompt-templates --no-themes` | `--no-skills --no-rules` |
| Resume command | `pi --continue` | `omp --continue` |
| Hook flag | `--extension` | `--hook` (aliases `--extension`) |
| Additional JSONL types | — | `ttsr_injection`, `session_init`, `mode_change` |
| Import type in extension | `ExtensionAPI` from `@mariozechner/...` | `ExtensionAPI` from `@oh-my-pi/...` |

## Hook Mechanism
- Config format: TypeScript extension files (loaded via jiti, no compilation needed)
- Extension locations:
  - Global: `~/.omp/agent/extensions/*.ts` or `~/.omp/agent/extensions/*/index.ts`
  - Project-local: `.omp/extensions/*.ts` or `.omp/extensions/*/index.ts`
- Extension registration: default export function receiving `ExtensionAPI`, register handlers via `pi.on(event, handler)`
- Shell execution in extensions: `child_process` from Node.js
- Hook flag: `--hook` (aliases `--extension`)

### Hook Names and Protocol Mapping
| Native Event Name | When It Fires | Protocol Event Type | Verified |
|------------------|---------------|---------------------|----------|
| `session_start` | Initial session load | 1 = SessionStart | Yes |
| `before_agent_start` | After user submits prompt, before agent loop | 2 = TurnStart | Yes |
| `agent_end` | Agent loop ends (all turns complete) | 3 = TurnEnd | Yes |
| `session_shutdown` | Process exit (Ctrl+C, Ctrl+D, or -p mode exit) | (cleanup only) | Yes |

### Hook Input Format (Extension → Binary)
The TypeScript extension constructs JSON and passes it to `entire agent hook omp <event>` on stdin:

**session_start**:
```json
{
  "type": "session_start",
  "cwd": "/path/to/repo",
  "session_file": "/Users/.../.omp/agent/sessions/<encoded-path>/<timestamp>_<hex-id>.jsonl"
}
```

**before_agent_start**:
```json
{
  "type": "before_agent_start",
  "cwd": "/path/to/repo",
  "session_file": "/Users/.../.omp/agent/sessions/<encoded-path>/<timestamp>_<hex-id>.jsonl",
  "prompt": "user prompt text"
}
```

**agent_end**:
```json
{
  "type": "agent_end",
  "cwd": "/path/to/repo",
  "session_file": "/Users/.../.omp/agent/sessions/<encoded-path>/<timestamp>_<hex-id>.jsonl"
}
```

**session_shutdown**:
```json
{
  "type": "session_shutdown"
}
```

## Session Management
- Session directory: `~/.omp/agent/sessions/<encoded-path>/`
- Session file pattern: `<ISO-timestamp>_<snowflake-hex-id>.jsonl`
  - Example: `2026-03-27T21-38-13-384Z_0195e4b3a1c07f2d8e9a.jsonl`
- Session ID source: Snowflake hex ID extracted from session filename (after last `_`, before `.jsonl`)
- Session file format: JSONL (newline-delimited JSON, one entry per line)
- Resume mechanism: `omp --continue`

## Transcript
- Location: `~/.omp/agent/sessions/<encoded-path>/<timestamp>_<hex-id>.jsonl`
- Format: JSONL with tree structure (entries have `id` and `parentId`) — identical to pi v3
- Version: 3

### Entry Types
| Entry Type | Fields | Purpose |
|-----------|--------|---------|
| `session` | `version`, `id`, `timestamp`, `cwd` | Session header (first line) |
| `model_change` | `provider`, `modelId` | Model selection |
| `message` | `message` object with `role`, `content`, etc. | All messages |
| `ttsr_injection` | (omp-specific) | Internal state injection — ignored by transcript parser |
| `session_init` | (omp-specific) | Session initialization — ignored by transcript parser |
| `mode_change` | (omp-specific) | Mode change record — ignored by transcript parser |

omp-specific entry types are not `message` type entries, so they are naturally filtered out by the existing code that checks `entry.Type == "message"`.

### Message Roles
| Role | Content Types | Key Fields |
|------|--------------|------------|
| `user` | `text` | `content[].text` |
| `assistant` | `text`, `toolCall`, `thinking` | `content[]`, `usage`, `stopReason`, `model`, `provider` |
| `toolResult` | `text` | `toolCallId`, `toolName`, `content[]`, `isError` |

### Tool Call Format
```json
{
  "type": "toolCall",
  "id": "toolu_01WcS7KmFVQoiYd9h9gavbxs",
  "name": "write",
  "arguments": {"path": "hello.txt", "content": "hello world\n"}
}
```

File-modifying tools:
- `write`: `arguments.path` (file path), `arguments.content` (file content)
- `edit`: `arguments.path` (file path), `arguments.oldText` / `arguments.newText` or `arguments.edits[]`

### Token Usage Format
```json
{
  "input": 2572,
  "output": 73,
  "cacheRead": 0,
  "cacheWrite": 0
}
```

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | static metadata | Return name "omp", type "Oh My Pi", capabilities | Required |
| `detect` | `omp` binary | Check `command -v omp` | Required |
| `get-session-id` | session header hex ID | Extract from hook input `session_file` path (hex ID after `_`) | Required |
| `get-session-dir` | `.entire/tmp` | Use default Entire session dir | Required |
| `resolve-session-file` | `.entire/tmp/<id>.json` | Standard path resolution | Required |
| `read-session` | JSONL transcript | Read omp's JSONL, build AgentSession with native_data | Required |
| `write-session` | cached transcript | Write normalized session data to session ref | Required |
| `read-transcript` | JSONL file bytes | Read raw bytes from omp session or cached `.entire/tmp/<id>.json` | Required |
| `chunk-transcript` | raw bytes | Base64 chunk by max size | Required |
| `reassemble-transcript` | base64 chunks | Reassemble chunks | Required |
| `format-resume-command` | `omp --continue` | Return `omp --continue` | Required |
| `parse-hook` | extension event JSON | Map extension JSON to EventJSON (type 1/2/3) | Hooks |
| `install-hooks` | `.omp/extensions/entire/` | Write TypeScript extension that calls `entire agent hook omp <event>` | Hooks |
| `uninstall-hooks` | remove extension dir | Delete `.omp/extensions/entire/` | Hooks |
| `are-hooks-installed` | check extension exists | Check for `.omp/extensions/entire/index.ts` | Hooks |
| `get-transcript-position` | line count | Return line count of transcript file | Transcript analyzer |
| `extract-modified-files` | tool call parsing | Extract `path` from `write` and `edit` tool calls in JSONL | Transcript analyzer |
| `extract-prompts` | user message parsing | Extract text from `role: "user"` messages | Transcript analyzer |
| `extract-summary` | last assistant text | Extract last `role: "assistant"` text content | Transcript analyzer |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | omp has a TypeScript extension system with lifecycle events (verified) |
| transcript_analyzer | true | JSONL transcripts contain full structured data — tool calls, prompts, responses (verified) |
| transcript_preparer | false | JSONL files are directly readable, no pre-processing needed |
| token_calculator | true | Assistant messages contain `usage` with input/output/cache tokens (verified) |
| text_generator | false | omp CLI is used for agent execution, not standalone text generation |
| hook_response_writer | false | No mechanism for writing structured responses back through hooks |
| subagent_aware_extractor | false | omp does not expose a subagent transcript tree |

## Gaps & Limitations
- **Extension-based hooks**: Like pi, omp requires a TypeScript extension file. The extension uses `child_process.execFile` to call the `entire` binary, adding Node.js as a runtime dependency for hooks.
- **Session directory discovery**: The path encoding scheme must be reimplemented in Go to locate session files.
- **Print mode limitations**: `omp -p` exits after one prompt. For multi-turn testing, interactive mode with `--continue` is needed.
- **omp-specific entry types**: `ttsr_injection`, `session_init`, and `mode_change` entries may have `parentId` fields. The tree walker uses these in the `parentOf` map, but since they are not `message` type entries, they do not affect `lastMessageID`. They can appear in the active branch ancestry chain (since message entries may have them as parents), which is correct behavior — their IDs flow through `parentOf` to connect the tree.

## E2E Test Prerequisites
- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN` env var
- Agent CLI binary: `omp` (Node.js, installed via npm)
- --no-skills --no-rules
- Interactive mode: Supported — `omp` launches interactive TUI, `omp --continue` resumes
- Expected prompt pattern: `\$\d` (same as pi)
- Timeout multiplier: 1.5 (Node.js startup + LLM API calls)
- Bootstrap steps: API key must be set (e.g., `ANTHROPIC_API_KEY`, or other provider key)
- Transient error patterns: `"overloaded"`, `"rate limit"`, `"429"`, `"503"`, `"ECONNRESET"`, `"ETIMEDOUT"`, `"timeout"`
