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

## API Endpoints

### Health Check

The FastAPI application exposes a health check endpoint.

- **GET /health**
  - **Response:** 200 OK
  - **Content:** `{ "status": "healthy" }`
  - **Usage:** Use to verify the agent API service is up and reachable.

Example:

```bash
curl http://localhost:8000/health
# {"status": "healthy"}
```

### Portfolio Website

A modern portfolio website for a generative AI developer is served at:

- **GET /portfolio**
  - **Response:** 200 OK (HTML page with profile, skills, selected projects, and a contact form)
- **Static assets:** Loaded under `/portfolio-static/portfolio.css` (CSS) and `/portfolio-static/portfolio.js` (JS)

#### Access the Portfolio

Run the FastAPI app (e.g. with `uvicorn src.agentic_dev_agent.__init__:app`), then visit:

- [http://localhost:8000/portfolio](http://localhost:8000/portfolio)

#### Portfolio Contents

- **About/Introduction**: Overview of the developer profile and philosophy
- **Skills**: Technologies, frameworks, and areas of expertise for generative AI
- **Projects**: Hand-picked, detailed LLM, agent, and creative ML work
- **Contact**: Simple frontend contact form (demo, no backend email integration)

To customize, edit the files in `src/agentic_dev_agent/ui/`:
- `portfolio.html` — structure & content
- `portfolio.css` — theme, colors, layout
- `portfolio.js` — form interactivity or frontend logic

## Service Integration

LLM calls use a deterministic local provider by default. Install `.[llm]` and set `OPENAI_API_KEY` to call an OpenAI-compatible gateway, including a Databricks model serving gateway if exposed through an OpenAI-compatible endpoint. The agent reads `.env` automatically and uses `OPENAI_MODEL` by default, or the role-specific `CODING_MODEL`, `REASONING_MODEL`, `FAST_MODEL`, and `DEBUGGER_MODEL` values when set.

When `OPENAI_API_KEY` is present, the workflow requires the OpenAI SDK and will fail loudly instead of falling back to local generated output. Install it with:

```bash
pip install -e ".[llm]"
```

The autonomous loop now asks the model for a structured JSON plan, records that plan, then asks the coding model to execute it by returning repository-relative file artifacts. Previous runs are remembered per repository in `.entire/conversation.jsonl` and are included in future planning and implementation prompts.

Databricks and Entire adapters are intentionally thin. They always record locally and can be extended to write to Databricks SQL tables or an Entire API by setting the environment variables in `.env.example`.

## UI

The Streamlit UI provides a hackathon-ready operator console with:

- request and repository controls
- dry-run or apply mode
- project context summary
- plan, model routing, generated artifacts, test output, and review status
- Entire timeline table and JSON download

---

### Portfolio Feature Test Coverage

API and static asset tests for the /portfolio endpoint are provided in `tests/test_portfolio.py`. You can run them with:

```bash
pytest tests/test_portfolio.py
```
