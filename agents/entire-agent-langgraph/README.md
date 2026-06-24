# entire-agent-langgraph

External agent wrapper that lets Entire CLI work with LangGraph and LangChain agents through the published Python package `entire-adapter`.

## Installation

Most users should install from PyPI:

```bash
pip install "entire-adapter[langgraph]"
entire enable --agent langgraph --telemetry=false
```

That installs `entire-agent-langgraph` on `PATH`, which Entire discovers when external agents are enabled.

## Source Build

From this directory:

```bash
mise run build
./entire-agent-langgraph info
```

The source build creates a local `.venv`, installs `entire-adapter[langgraph]==0.2.1`, and writes `./entire-agent-langgraph`.

## How It Works

LangGraph applications add `EntireCallbackHandler` to their callback config:

```python
from entire_adapter import EntireCallbackHandler

handler = EntireCallbackHandler(agent_label="repo-editor")
graph.invoke({"prompt": "Edit the repo"}, config={"callbacks": [handler]})
print(handler.session_id)
```

The callback maps top-level graph starts, tool calls, tool results, and graph completion to Entire lifecycle hooks:

```text
on_chain_start -> session-start + turn-start
on_tool_start  -> transcript tool context
on_tool_end    -> turn-end checkpoint hook
on_chain_end   -> session-end
```

The adapter writes transcript JSONL outside the worktree when possible and passes that path to Entire as `session_ref`.

`install-hooks` writes a small marker under `.entire/` because LangGraph hooks are emitted by Python callbacks in the user process, not by framework-managed project hook files.

## Capabilities

| Capability | Status |
| ---------- | ------ |
| hooks | Yes, emitted by Python callbacks through `entire hooks langgraph <hook>` |
| transcript_analyzer | Yes, parses adapter JSONL transcripts |
| compact_transcript | Yes, emits compact JSONL |
| transcript_preparer | No |
| token_calculator | No |

## Development

```bash
mise run test
mise run clean
```

Lifecycle coverage lives in the repo-root `e2e/` harness and runs a deterministic local LangGraph workflow without live LLM credentials.
