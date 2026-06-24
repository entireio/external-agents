# CrewAI External Agent Research

## Verdict: COMPATIBLE

CrewAI exposes an event listener API that maps to Entire lifecycle events through the published `entire-adapter` package.

## Binary

- Entire external-agent binary: `entire-agent-crewai`
- Runtime package: `entire-adapter==0.2.1`
- User package for real CrewAI projects: `entire-adapter[crewai]`
- Entire agent name: `crewai`

## Hook Mechanism

CrewAI projects register `EntireCrewAIListener`, which listens for crew kickoff, agent execution, tool usage, and crew completion events.

The listener directly invokes:

```text
entire hooks crewai session-start
entire hooks crewai turn-start
entire hooks crewai turn-end
entire hooks crewai session-end
```

## Protocol Mapping

The PyPI package implements Entire's external-agent protocol commands, including `info`, `detect`, `parse-hook`, `read-session`, transcript extraction, and compact transcript generation.

## E2E Strategy

Default CI validates CrewAI protocol behavior without live LLM credentials. Full CrewAI runtime smoke coverage is gated by `CREWAI_E2E=1`.
