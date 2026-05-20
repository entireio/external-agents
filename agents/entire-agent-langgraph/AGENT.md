# LangGraph External Agent Research

## Verdict: COMPATIBLE

LangGraph and LangChain expose callback hooks that map cleanly to Entire lifecycle events through the published `entire-adapter` package.

## Binary

- Entire external-agent binary: `entire-agent-langgraph`
- Runtime package: `entire-adapter[langgraph]==0.2.1`
- Entire agent name: `langgraph`

## Hook Mechanism

LangGraph applications pass `EntireCallbackHandler` in the runnable config:

```python
config={"callbacks": [EntireCallbackHandler(agent_label="repo-editor")]}
```

The callback directly invokes:

```text
entire hooks langgraph session-start
entire hooks langgraph turn-start
entire hooks langgraph turn-end
entire hooks langgraph session-end
```

## Protocol Mapping

The PyPI package implements Entire's external-agent protocol commands, including `info`, `detect`, `parse-hook`, `read-session`, transcript extraction, and compact transcript generation.

## E2E Strategy

The shared lifecycle suite uses a deterministic local LangGraph fixture, not a live LLM. The fixture writes requested files through a LangChain tool so `on_tool_end` triggers a real Entire `turn-end` hook and shadow checkpoint.
