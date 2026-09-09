# entire-agent-crewai

External agent wrapper that lets Entire CLI work with CrewAI projects through the published Python package `entire-adapter`.

## Installation

Most users should install from PyPI:

```bash
pip install "entire-adapter[crewai]"
entire enable --agent crewai --telemetry=false
```

That installs `entire-agent-crewai` on `PATH`, which Entire discovers when external agents are enabled.

## Source Build

From this directory:

```bash
mise run build
./entire-agent-crewai info
```

The source build creates a local `.venv`, installs `entire-adapter==0.2.2`, and writes `./entire-agent-crewai`. By default, the build does not install CrewAI itself, keeping protocol CI lightweight.

## How It Works

CrewAI projects register `EntireCrewAIListener` with CrewAI's event bus:

```python
from entire_adapter import EntireCrewAIListener

listener = EntireCrewAIListener(agent_label="research-crew")
```

The listener maps crew kickoff, agent execution, tool usage, and crew completion events to Entire lifecycle hooks and transcript records.

`install-hooks` writes a small marker under `.entire/` because CrewAI hooks are emitted by Python event listeners in the user process, not by framework-managed project hook files.

## Capabilities

| Capability | Status |
| ---------- | ------ |
| hooks | Yes, emitted by CrewAI event listeners through `entire hooks crewai <hook>` |
| transcript_analyzer | Yes, parses adapter JSONL transcripts |
| compact_transcript | Yes, emits compact JSONL |
| transcript_preparer | No |
| token_calculator | No |

## Development

```bash
mise run test
mise run clean
```

Run the optional runtime lifecycle test from the repository root:

```bash
CREWAI_E2E=1 E2E_AGENT=crewai mise run test:e2e:lifecycle
```

With `CREWAI_E2E=1`, the build additionally installs `crewai==1.15.20` using Python 3.13 in a separate `.venv-crewai` runtime. Protocol commands keep using the lightweight `.venv` so importing CrewAI cannot delay hook handling. The deterministic fixture emits real CrewAI kickoff, tool-start, tool-finish, and completion events through CrewAI's event bus into `EntireCrewAIListener`. It writes a file and verifies its Entire checkpoint and transcript after committing. No LLM credentials are needed; this tests listener integration, not a model-driven Crew run.
