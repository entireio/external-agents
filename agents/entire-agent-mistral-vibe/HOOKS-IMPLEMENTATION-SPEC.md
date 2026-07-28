# Mistral Vibe — Lifecycle Hooks Implementation Spec

This document contains everything needed to add lifecycle hooks to Mistral Vibe so it integrates with the Entire CLI external agent protocol.

## Goal

Add a configurable lifecycle hook system to Mistral Vibe that:
1. Fires shell commands at key lifecycle events (session start, prompt submit, tool use, session end)
2. Passes structured JSON on stdin to hook commands
3. Is configured via `.vibe/hooks.toml` (project-level) or `~/.vibe/hooks.toml` (global)
4. Works with both interactive (TUI) and programmatic (`-p`) modes

## Vibe Source Code Location

The Vibe package is installed at:
```
~/.local/share/uv/tools/mistral-vibe/lib/python3.13/site-packages/vibe/
```

The upstream repo is `mistralai/mistral-vibe` (open source).

## Current Architecture — Where Hooks Fit

### Agent Loop (`vibe/core/agent_loop.py`)

This is the core event loop. Key methods where hooks should fire:

| Lifecycle Event | Where in Code | When It Fires |
|----------------|---------------|---------------|
| **session_start** | `AgentLoop.__init__()` or first call to `act()` | Session begins |
| **user_prompt_submit** | `AgentLoop.act(prompt)` entry point | User submits a prompt |
| **pre_tool_use** | Before `_execute_tool_call()` runs a tool | About to execute a tool |
| **post_tool_use** | After `_execute_tool_call()` returns | Tool execution complete |
| **stop** | End of `act()` generator / session teardown | Agent done responding |

### Key Classes

- **`AgentLoop`** (`vibe/core/agent_loop.py`) — Main loop, lines ~150+. Has `act(prompt)` async generator that yields events. This is where most hooks fire.
- **`SessionLogger`** (`vibe/core/session/session_logger.py`) — Manages session persistence. Has `session_id`, `session_dir`, `messages_filepath`.
- **`VibeConfig`** (`vibe/core/config/_settings.py`) — Pydantic settings loaded from TOML. Hook config goes here.
- **`MiddlewarePipeline`** (`vibe/core/middleware.py`) — Existing before-turn pipeline. Hooks could be implemented as middleware or as a separate system.

### Session Management (already exists)

Sessions are stored at `~/.vibe/logs/session/session_YYYYMMDD_HHMMSS_<id>/`:
- `meta.json` — Session metadata (session_id, start_time, end_time, git info, stats)
- `messages.jsonl` — Full conversation (one JSON line per message)

The session ID is a UUID generated in `SessionLogger.__init__()`.

### Config System

Vibe uses TOML config files:
- Global: `~/.vibe/config.toml`
- Project: `.vibe/config.toml` (in trusted folders)
- Loaded via Pydantic Settings in `VibeConfig` class

The harness file manager at `vibe/core/config/harness_files/_harness_manager.py` handles config discovery.

## Hook System Design

### Hook Configuration Format

Add to `.vibe/hooks.toml` (or as a `[hooks]` section in `config.toml`):

```toml
# .vibe/hooks.toml — project-level hook configuration

[hooks.session_start]
command = "entire hooks mistral-vibe session-start"

[hooks.user_prompt_submit]
command = "entire hooks mistral-vibe user-prompt-submit"

[hooks.pre_tool_use]
command = "entire hooks mistral-vibe pre-tool-use"

[hooks.post_tool_use]
command = "entire hooks mistral-vibe post-tool-use"

[hooks.stop]
command = "entire hooks mistral-vibe stop"
```

Each hook entry has:
- `command` (required): Shell command to execute
- Multiple commands per event should be supported (list of tables)

### Hook Payload Format (JSON on stdin)

Every hook command receives a JSON object on stdin. The format must match what the Entire CLI external agent binary expects to parse.

#### session_start
```json
{
  "hook_event_name": "session_start",
  "cwd": "/path/to/repo",
  "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

#### user_prompt_submit
```json
{
  "hook_event_name": "user_prompt_submit",
  "cwd": "/path/to/repo",
  "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "prompt": "Fix the login bug"
}
```

#### pre_tool_use
```json
{
  "hook_event_name": "pre_tool_use",
  "cwd": "/path/to/repo",
  "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "tool_name": "write_file",
  "tool_input": {"file_path": "/src/main.py", "content": "..."}
}
```

#### post_tool_use
```json
{
  "hook_event_name": "post_tool_use",
  "cwd": "/path/to/repo",
  "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "tool_name": "write_file",
  "tool_input": {"file_path": "/src/main.py", "content": "..."},
  "tool_response": {"result": "File written successfully"}
}
```

#### stop
```json
{
  "hook_event_name": "stop",
  "cwd": "/path/to/repo",
  "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

### Hook Execution Requirements

1. **Non-blocking**: Hook commands must be fired asynchronously (subprocess, fire-and-forget). They must NOT block the agent loop or slow down the user experience.
2. **stdin pipe**: Pass the JSON payload via stdin to the subprocess.
3. **No stdout capture needed**: Hook commands write their own outputs; Vibe doesn't need to read them.
4. **stderr logging**: Log any stderr from hook commands to Vibe's log file (`~/.vibe/logs/vibe.log`) for debugging.
5. **Timeout**: Kill hook subprocesses that haven't exited after 30 seconds.
6. **Error isolation**: A failing hook must never crash Vibe. Catch all exceptions, log them, continue.
7. **Environment variables**: Set `ENTIRE_REPO_ROOT` to the git repo root (if in a git repo) for hook commands.

## Implementation Plan

### Step 1: Add Hook Config Model

In `vibe/core/config/_settings.py`, add:

```python
class HookEntry(BaseModel):
    command: str

class HooksConfig(BaseModel):
    session_start: list[HookEntry] = Field(default_factory=list)
    user_prompt_submit: list[HookEntry] = Field(default_factory=list)
    pre_tool_use: list[HookEntry] = Field(default_factory=list)
    post_tool_use: list[HookEntry] = Field(default_factory=list)
    stop: list[HookEntry] = Field(default_factory=list)
```

Add `hooks: HooksConfig = Field(default_factory=HooksConfig)` to `VibeConfig`.

### Step 2: Create Hook Manager

Create `vibe/core/hooks.py`:

```python
"""Lifecycle hook manager for Vibe.

Fires shell commands at key lifecycle events, passing structured JSON on stdin.
Hook commands run asynchronously and never block the agent loop.
"""
import asyncio
import json
import logging
import os
import subprocess
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

HOOK_TIMEOUT_SECONDS = 30


class HookManager:
    def __init__(self, config: "HooksConfig", session_id: str, cwd: str) -> None:
        self._config = config
        self._session_id = session_id
        self._cwd = cwd
        self._env = self._build_env()
        self._tasks: list[asyncio.Task] = []

    def _build_env(self) -> dict[str, str]:
        env = dict(os.environ)
        # Try to find git repo root
        try:
            result = subprocess.run(
                ["git", "rev-parse", "--show-toplevel"],
                capture_output=True, text=True, timeout=5, cwd=self._cwd
            )
            if result.returncode == 0:
                env["ENTIRE_REPO_ROOT"] = result.stdout.strip()
        except Exception:
            pass
        return env

    def _base_payload(self, event_name: str) -> dict[str, Any]:
        return {
            "hook_event_name": event_name,
            "cwd": self._cwd,
            "session_id": self._session_id,
        }

    async def _fire(self, event_name: str, extra: dict[str, Any] | None = None) -> None:
        hooks = getattr(self._config, event_name, [])
        if not hooks:
            return
        payload = self._base_payload(event_name)
        if extra:
            payload.update(extra)
        payload_bytes = json.dumps(payload).encode()

        for hook in hooks:
            task = asyncio.create_task(self._run_hook(hook.command, payload_bytes))
            self._tasks.append(task)

    async def _run_hook(self, command: str, payload: bytes) -> None:
        try:
            proc = await asyncio.create_subprocess_shell(
                command,
                stdin=asyncio.subprocess.PIPE,
                stdout=asyncio.subprocess.DEVNULL,
                stderr=asyncio.subprocess.PIPE,
                env=self._env,
                cwd=self._cwd,
            )
            try:
                _, stderr = await asyncio.wait_for(
                    proc.communicate(input=payload),
                    timeout=HOOK_TIMEOUT_SECONDS,
                )
                if stderr:
                    logger.debug("Hook %r stderr: %s", command, stderr.decode(errors="replace"))
                if proc.returncode != 0:
                    logger.warning("Hook %r exited with code %d", command, proc.returncode)
            except asyncio.TimeoutError:
                proc.kill()
                logger.warning("Hook %r timed out after %ds, killed", command, HOOK_TIMEOUT_SECONDS)
        except Exception:
            logger.exception("Failed to run hook %r", command)

    # --- Public API ---

    async def fire_session_start(self) -> None:
        await self._fire("session_start")

    async def fire_user_prompt_submit(self, prompt: str) -> None:
        await self._fire("user_prompt_submit", {"prompt": prompt})

    async def fire_pre_tool_use(self, tool_name: str, tool_input: Any) -> None:
        await self._fire("pre_tool_use", {"tool_name": tool_name, "tool_input": tool_input})

    async def fire_post_tool_use(self, tool_name: str, tool_input: Any, tool_response: Any) -> None:
        await self._fire("post_tool_use", {
            "tool_name": tool_name,
            "tool_input": tool_input,
            "tool_response": tool_response,
        })

    async def fire_stop(self) -> None:
        await self._fire("stop")
        # Wait for all pending hooks to complete before session teardown
        if self._tasks:
            await asyncio.gather(*self._tasks, return_exceptions=True)
            self._tasks.clear()
```

### Step 3: Integrate into AgentLoop

In `vibe/core/agent_loop.py`, add hook firing at each lifecycle point:

1. **Constructor / session init**: Create `HookManager` with session config, session ID from `SessionLogger`, and cwd.

2. **`act()` method entry**: Fire `session_start` on first invocation, `user_prompt_submit` on each prompt.

3. **Tool execution**: Fire `pre_tool_use` before and `post_tool_use` after each tool call.

4. **Session end**: Fire `stop` when the agent loop completes or is interrupted.

Key integration points in the existing code:

```python
# In AgentLoop.__init__() — after SessionLogger is created:
from vibe.core.hooks import HookManager
self._hook_manager = HookManager(
    config=config.hooks,
    session_id=self.session_logger.session_id,
    cwd=str(Path.cwd()),
)
self._session_started = False

# In AgentLoop.act() — at the top, before processing:
if not self._session_started:
    await self._hook_manager.fire_session_start()
    self._session_started = True
await self._hook_manager.fire_user_prompt_submit(prompt)

# In tool execution (wherever _execute_tool_call or equivalent runs):
await self._hook_manager.fire_pre_tool_use(tool_name, tool_input_dict)
# ... execute tool ...
await self._hook_manager.fire_post_tool_use(tool_name, tool_input_dict, tool_result_dict)

# At session end (finally block or cleanup):
await self._hook_manager.fire_stop()
```

### Step 4: Add Hook Installation CLI Support (Optional)

For the external agent binary to call `install-hooks`, Vibe needs a way to programmatically write hook config. This can be done by writing to `.vibe/hooks.toml` directly from the Go binary — Vibe just needs to read the file on startup.

The external agent binary (`entire-agent-mistral-vibe`) handles `install-hooks` by writing the TOML file.

## Reference: How Kiro Implements Hooks

Kiro's hook system is a useful reference. Key files:

### Hook Types (from `agents/entire-agent-kiro/internal/kiro/types.go`)
```go
const (
    HookNameAgentSpawn       = "agent-spawn"        // → protocol EventType 1 (SessionStart)
    HookNameUserPromptSubmit = "user-prompt-submit"  // → protocol EventType 2 (TurnStart)
    HookNamePreToolUse       = "pre-tool-use"        // → nil (no event emitted)
    HookNamePostToolUse      = "post-tool-use"       // → nil (captures tool calls)
    HookNameStop             = "stop"                // → protocol EventType 3 (TurnEnd)
)
```

### Hook Input Format (what Kiro sends to hooks via stdin)
```go
type hookInputRaw struct {
    HookEventName string          `json:"hook_event_name"`
    CWD           string          `json:"cwd"`
    Prompt        string          `json:"prompt,omitempty"`
    ToolName      string          `json:"tool_name,omitempty"`
    ToolInput     json.RawMessage `json:"tool_input,omitempty"`
    ToolResponse  json.RawMessage `json:"tool_response,omitempty"`
}
```

### Hook Installation (from `agents/entire-agent-kiro/internal/kiro/hooks.go`)

Kiro writes hooks as JSON config files that the IDE reads:
- CLI hooks: `.kiro/agents/entire.json` — commands like `entire hooks kiro stop`
- IDE hooks: `.kiro/hooks/entire-stop.kiro.hook` — trigger type + command

For Vibe, the equivalent is writing `.vibe/hooks.toml` with commands like:
```toml
[[hooks.stop]]
command = "entire hooks mistral-vibe stop"
```

### Hook Parsing (from `agents/entire-agent-kiro/internal/kiro/hooks.go`)

The `ParseHook()` method maps native hook events to protocol event types:
- `agent-spawn` → EventType 1 (SessionStart) — generates session ID
- `user-prompt-submit` → EventType 2 (TurnStart) — includes user prompt
- `pre-tool-use` → nil (skipped)
- `post-tool-use` → nil (but captures tool calls for transcript enrichment)
- `stop` → EventType 3 (TurnEnd) — captures transcript, includes session_ref

## Entire CLI External Agent Protocol — Event Types

The protocol event types that hooks map to:

| Value | Name | Description |
|-------|------|-------------|
| 1 | SessionStart | Agent session has begun |
| 2 | TurnStart | User submitted a prompt |
| 3 | TurnEnd | Agent finished responding |
| 4 | Compaction | Context window compression |
| 5 | SessionEnd | Session terminated |
| 6 | SubagentStart | Subagent spawned |
| 7 | SubagentEnd | Subagent completed |

### Protocol Event JSON (what the external agent binary returns)
```json
{
  "type": 3,
  "session_id": "abc123",
  "session_ref": "/path/to/messages.jsonl",
  "prompt": "Fix the login bug",
  "timestamp": "2026-01-13T12:00:00Z"
}
```

## Vibe Source Files to Modify

| File | Change |
|------|--------|
| `vibe/core/config/_settings.py` | Add `HookEntry`, `HooksConfig` models; add `hooks` field to `VibeConfig` |
| `vibe/core/hooks.py` | **NEW** — `HookManager` class |
| `vibe/core/agent_loop.py` | Create `HookManager`, fire hooks at lifecycle points |
| `vibe/core/programmatic.py` | Ensure hooks fire in programmatic mode too |
| `vibe/cli/textual_ui/app.py` | Ensure hooks fire in interactive TUI mode |

## Testing the Integration

### Manual test:
```bash
# Create a test hook that dumps payloads
mkdir -p /tmp/vibe-hooks
cat > /tmp/test-hook.sh << 'SH'
#!/bin/bash
cat > "/tmp/vibe-hooks/$(date +%s)-${1:-unknown}.json"
SH
chmod +x /tmp/test-hook.sh

# Configure hooks in project
mkdir -p .vibe
cat > .vibe/hooks.toml << 'TOML'
[[hooks.session_start]]
command = "/tmp/test-hook.sh session_start"

[[hooks.user_prompt_submit]]
command = "/tmp/test-hook.sh user_prompt_submit"

[[hooks.stop]]
command = "/tmp/test-hook.sh stop"
TOML

# Run vibe
vibe -p "What is 2+2?"

# Check captured payloads
ls /tmp/vibe-hooks/
cat /tmp/vibe-hooks/*.json | python3 -m json.tool
```

### Integration test with Entire CLI:
```bash
# After hooks are implemented in Vibe and the external agent binary is built:
entire enable mistral-vibe
vibe -p "Create a file called hello.txt with hello world"
git add -A && git commit -m "test"
entire rewind list  # Should show a checkpoint
```

## Payload Field Reference

These are all the fields that should be available in hook payloads. The external agent binary (`entire-agent-mistral-vibe`) will parse these to construct protocol events.

| Field | Type | Present In | Description |
|-------|------|-----------|-------------|
| `hook_event_name` | string | All | Event type identifier |
| `cwd` | string | All | Working directory |
| `session_id` | string | All | Vibe session UUID |
| `prompt` | string | user_prompt_submit | User's prompt text |
| `tool_name` | string | pre/post_tool_use | Tool being called (e.g., `write_file`, `bash`, `grep`) |
| `tool_input` | object | pre/post_tool_use | Raw tool input arguments |
| `tool_response` | object | post_tool_use | Tool execution result |

## Session File Locations (for the external agent binary)

The external agent binary needs to know where Vibe stores sessions:

| Data | Path Pattern |
|------|-------------|
| Session log root | `~/.vibe/logs/session/` |
| Session folder | `session_YYYYMMDD_HHMMSS_<id[:8]>/` |
| Session metadata | `<session_folder>/meta.json` |
| Session transcript | `<session_folder>/messages.jsonl` |
| Config (global) | `~/.vibe/config.toml` |
| Config (project) | `.vibe/config.toml` |
| Hook config | `.vibe/hooks.toml` (proposed) |

The session_id in the folder name is the first 8 characters of the full UUID. To find a session by full ID, glob for `session_*_<id[:8]}`.
