# Autonomous AI Coding Agent

This is a runnable reference implementation for the final architecture:

- project context layer for new or existing repositories
- task analysis, planning, model routing, coding, testing, debugging, review, and deployment stages
- specialized coding/reasoning/fast model roles
- Databricks hooks for model gateway, observability, evaluation, and memory
- Entire timeline capture for prompts, plans, files, commands, diffs, tests, errors, and deployment events

The default mode is safe and local. It scans a repository, produces a plan, proposes code artifacts, runs available validation commands, writes an Entire-style timeline JSONL file, and leaves real file modifications behind a flag.

## Quick Start

```bash
python -m venv .venv
.venv\Scripts\activate
pip install -e ".[dev]"
agentic-dev --repo . --request "Add a health check endpoint" --dry-run
pytest
```

Run the Streamlit UI:

```bash
streamlit run streamlit_app.py
```

To allow the agent to create files in the target repository:

```bash
agentic-dev --repo C:\path\to\repo --request "Create a FastAPI app with tests" --apply
```

## Service Integration

LLM calls use a deterministic local provider by default. Install `.[llm]` and set `OPENAI_API_KEY` to call an OpenAI-compatible gateway, including a Databricks model serving gateway if exposed through an OpenAI-compatible endpoint.

Databricks and Entire adapters are intentionally thin. They always record locally and can be extended to write to Databricks SQL tables or an Entire API by setting the environment variables in `.env.example`.

## UI

The Streamlit UI provides a hackathon-ready operator console with:

- request and repository controls
- dry-run or apply mode
- project context summary
- plan, model routing, generated artifacts, test output, and review status
- Entire timeline table and JSON download
