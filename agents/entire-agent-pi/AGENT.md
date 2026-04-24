# Pi — External Agent Research

## Verdict: COMPATIBLE

Pi has a rich TypeScript extension system with lifecycle hooks, JSONL session storage with full transcript content (including tool calls, token usage, and actual assistant responses), and a non-interactive print mode. All necessary protocol subcommands can be implemented.

## Static Checks
| Check | Result | Notes |
|-------|--------|-------|
| Binary present | PASS | `/opt/homebrew/bin/pi` (verified) |
| Help available | PASS | `pi --help` (verified) |
| Version info | PASS | v0.63.1 (verified) |
| Hook keywords | PASS | `extension` found in help (verified) |
| Session keywords | PASS | `session`, `resume`, `continue` found (verified) |
| Config directory | PASS | `~/.pi/agent` (verified) |
| Documentation | PASS | https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/extensions.md |

## Binary
- Name: `pi`
- Version: 0.63.1
- Runtime: Node.js (`/usr/bin/env node` script)
- Package: `@mariozechner/pi-coding-agent` on npm
- Install: `npm install -g @mariozechner/pi-coding-agent` or Homebrew

## Hook Mechanism
- Config format: TypeScript extension files (loaded via jiti, no compilation needed)
- Extension locations:
  - Global: `~/.pi/agent/extensions/*.ts` or `~/.pi/agent/extensions/*/index.ts`
  - Project-local: `.pi/extensions/*.ts` or `.pi/extensions/*/index.ts`
- Extension registration: default export function receiving `ExtensionAPI`, register handlers via `pi.on(event, handler)`
- Shell execution in extensions: `pi.exec(command, args, options?)` or `child_process` from Node.js
- Hot-reload: Extensions in auto-discovered locations can be hot-reloaded with `/reload`

### Hook Names and Protocol Mapping
| Native Event Name | When It Fires | Protocol Event Type | Verified |
|------------------|---------------|---------------------|----------|
| `session_start` | Initial session load | 1 = SessionStart | Yes |
| `before_agent_start` | After user submits prompt, before agent loop | 2 = TurnStart | Yes |
| `turn_end` | Each LLM turn completes (tool use or final) | (internal, not a lifecycle event) | Yes |
| `agent_end` | Agent loop ends (all turns complete) | 3 = TurnEnd | Yes |
| `session_shutdown` | Process exit (Ctrl+C, Ctrl+D, or -p mode exit) | (cleanup only) | Yes |

### Hook Input Format (Extension → Binary)
The TypeScript extension constructs JSON and passes it to `entire agent hook pi <event>` on stdin:

**session_start** (verified):
```json
{
  "type": "session_start",
  "cwd": "/path/to/repo",
  "session_file": "/Users/.../.pi/agent/sessions/<encoded-path>/<timestamp>_<uuid>.jsonl"
}
```

**before_agent_start** (verified):
```json
{
  "type": "before_agent_start",
  "cwd": "/path/to/repo",
  "session_file": "/Users/.../.pi/agent/sessions/<encoded-path>/<timestamp>_<uuid>.jsonl",
  "prompt": "user prompt text"
}
```

**agent_end** (verified):
```json
{
  "type": "agent_end",
  "cwd": "/path/to/repo",
  "session_file": "/Users/.../.pi/agent/sessions/<encoded-path>/<timestamp>_<uuid>.jsonl",
  "message_count": 4
}
```

**session_shutdown** (verified):
```json
{
  "type": "session_shutdown"
}
```

## Session Management
- Session directory: `~/.pi/agent/sessions/<encoded-path>/`
  - `PI_CODING_AGENT_DIR` env var overrides `~/.pi/agent` base
- Path encoding: absolute path with `/` → `-`, wrapped in `--` prefix/suffix
  - Example: `/Users/nodo/work/repo` → `--Users-nodo-work-repo--` (verified)
- Session file pattern: `<ISO-timestamp>_<uuid>.jsonl`
  - Example: `2026-03-27T21-38-13-384Z_34567c89-98b3-4cc3-a76d-1a4a67193648.jsonl` (verified)
- Session ID source: UUID from the session file header entry (first line, `id` field) (verified)
- Session file format: JSONL (newline-delimited JSON, one entry per line)
- Resume mechanism: `pi --continue` (most recent) or `pi --session <path>` (specific file)

## Transcript
- Location: `~/.pi/agent/sessions/<encoded-path>/<timestamp>_<uuid>.jsonl` (verified)
- Format: JSONL with tree structure (entries have `id` and `parentId`)
- Version: 3 (verified from session header)

### Entry Types (verified)
| Entry Type | Fields | Purpose |
|-----------|--------|---------|
| `session` | `version`, `id`, `timestamp`, `cwd` | Session header (first line) |
| `model_change` | `provider`, `modelId` | Model selection |
| `thinking_level_change` | `thinkingLevel` | Thinking level setting |
| `message` | `message` object with `role`, `content`, etc. | All messages |

### Message Roles (verified)
| Role | Content Types | Key Fields |
|------|--------------|------------|
| `user` | `text` | `content[].text` |
| `assistant` | `text`, `toolCall`, `thinking` | `content[]`, `usage`, `stopReason`, `model`, `provider` |
| `toolResult` | `text` | `toolCallId`, `toolName`, `content[]`, `isError` |

### Tool Call Format (verified)
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
- `bash`: may modify files but path extraction is unreliable

### Token Usage Format (verified)
```json
{
  "input": 2572,
  "output": 73,
  "cacheRead": 0,
  "cacheWrite": 0,
  "totalTokens": 2645,
  "cost": {"input": 0.01286, "output": 0.001825, "cacheRead": 0, "cacheWrite": 0, "total": 0.014685}
}
```

## Data Storage Verification
- Session files contain actual assistant content: **YES** (verified — full response text, tool calls with arguments, thinking blocks)
- Secondary storage location: **none needed** — all data is in the JSONL session file
- Hook data flow verified: **YES** — extension receives event data, ctx provides session file path and cwd
- Verification method: Ran `pi -p` with a known prompt, inspected JSONL session file for actual tool call arguments and response text

## Protocol Mapping
| Subcommand | Native Concept | Implementation Notes | Feasibility |
|-----------|---------------|---------------------|-------------|
| `info` | static metadata | Return name "pi", type "Pi", capabilities | Required |
| `detect` | `pi` binary | Check `command -v pi` or `.pi/` in repo | Required |
| `get-session-id` | session header UUID | Extract from hook input `session_file` path (UUID after `_`) or read first line of JSONL | Required |
| `get-session-dir` | `.entire/tmp` | Use default Entire session dir | Required |
| `resolve-session-file` | `.entire/tmp/<id>.json` | Standard path resolution | Required |
| `read-session` | JSONL transcript | Read Pi's JSONL, build AgentSession with native_data | Required |
| `write-session` | cached transcript | Write normalized session data to session ref | Required |
| `read-transcript` | JSONL file bytes | Read raw bytes from Pi session or cached `.entire/tmp/<id>.json` | Required |
| `chunk-transcript` | raw bytes | Base64 chunk by max size | Required |
| `reassemble-transcript` | base64 chunks | Reassemble chunks | Required |
| `compact-transcript` | compact JSONL | Emit normalized Entire compact transcript JSONL for checkpoints v2 | Compact transcript |
| `format-resume-command` | `pi --continue` | Return `pi --continue` or `pi --session <path>` | Required |
| `parse-hook` | extension event JSON | Map extension JSON to EventJSON (type 1/2/3) | Hooks |
| `install-hooks` | `.pi/extensions/entire/` | Write TypeScript extension that calls `entire agent hook pi <event>` | Hooks |
| `uninstall-hooks` | remove extension dir | Delete `.pi/extensions/entire/` | Hooks |
| `are-hooks-installed` | check extension exists | Check for `.pi/extensions/entire/index.ts` | Hooks |
| `get-transcript-position` | file size | Return byte count of transcript file | Transcript analyzer |
| `extract-modified-files` | tool call parsing | Extract `path` from `write` and `edit` tool calls in JSONL | Transcript analyzer |
| `extract-prompts` | user message parsing | Extract text from `role: "user"` messages | Transcript analyzer |
| `extract-summary` | last assistant text | Extract last `role: "assistant"` text content | Transcript analyzer |

## Selected Capabilities
| Capability | Declared | Justification |
|-----------|----------|---------------|
| hooks | true | Pi has a TypeScript extension system with lifecycle events (verified) |
| transcript_analyzer | true | JSONL transcripts contain full structured data — tool calls, prompts, responses (verified) |
| transcript_preparer | false | JSONL files are directly readable, no pre-processing needed |
| token_calculator | true | Assistant messages contain `usage` with input/output/cache tokens (verified) |
| compact_transcript | true | JSONL transcripts can be converted to agent-agnostic compact transcript entries with inlined tool results |
| text_generator | false | Pi CLI is used for agent execution, not standalone text generation |
| hook_response_writer | false | No mechanism for writing structured responses back through hooks |
| subagent_aware_extractor | false | Pi does not expose a subagent transcript tree |

## Gaps & Limitations
- **Extension-based hooks**: Unlike agents with native JSON hook configs, Pi requires a TypeScript extension file. The extension must use `child_process.execFileSync` to call the `entire` binary, which adds Node.js as a runtime dependency for hooks.
- **Session directory discovery**: The path encoding scheme (`--` prefix/suffix, `/` → `-`) must be reimplemented in Go to locate session files. The encoding is verified but not documented by Pi — it could change in future versions.
- **No native `agentSpawn` equivalent**: Pi's `session_start` fires on every session load (including resume), not just new sessions. The binary must differentiate by checking whether the session file existed before.
- **Print mode limitations**: `pi -p` exits after one prompt. For multi-turn testing, interactive mode with `--continue` is needed.
- **No token cost breakdown by model**: The `usage.cost` field is present but the protocol's `TokenUsageResponse` uses raw token counts, not costs.

## Captured Payloads
- Verification script: `agents/entire-agent-pi/scripts/verify-pi.sh`
- Capture directory: `agents/entire-agent-pi/.probe-pi-*/captures/`
- Verification status: **VERIFIED** — script ran, all 5 lifecycle events captured
- Notable differences from docs: None — all events fire as documented with expected data

## E2E Test Prerequisites
- Entire CLI binary: `entire` from PATH or `E2E_ENTIRE_BIN` env var
- Agent CLI binary: `pi` (Node.js, installed via npm or Homebrew)
- Non-interactive prompt command: `pi -p '<prompt>' --no-skills --no-prompt-templates --no-themes`
- Interactive mode: Supported — `pi` launches interactive TUI, `pi --continue` resumes
- Expected prompt pattern: `>` (the Pi prompt indicator)
- Timeout multiplier: 1.5 (Node.js startup + LLM API calls)
- Bootstrap steps: API key must be set (e.g., `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, or other provider key)
- Transient error patterns: `"overloaded"`, `"rate limit"`, `"429"`, `"503"`, `"ECONNRESET"`, `"ETIMEDOUT"`, `"timeout"`
