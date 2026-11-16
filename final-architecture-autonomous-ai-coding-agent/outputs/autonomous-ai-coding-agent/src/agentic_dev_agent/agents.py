from __future__ import annotations

import json
from pathlib import Path

from .models import LLMProvider
from .state import AgentState, CodeArtifact, Plan, ReviewResult, TaskAnalysis


class TaskAnalyzer:
    def run(self, state: AgentState) -> TaskAnalysis:
        request = state.request.lower()
        task_type = "existing_repo_change" if state.context and state.context.files else "new_project"
        if "readme" in request or "doc" in request:
            task_type = "docs"
        if "test" in request or "failing" in request:
            task_type = "test_fix"
        risk = "high" if any(word in request for word in ["auth", "payment", "security", "database"]) else "medium"
        if task_type in {"docs", "new_project"}:
            risk = "low"
        requirements = [part.strip(" .") for part in state.request.split(" and ") if part.strip()]
        return TaskAnalysis(task_type, requirements or [state.request], risk, "Heuristic task analysis complete.")


class Planner:
    def run(self, state: AgentState, llm: LLMProvider) -> Plan:
        model = state.selected_models.get("reasoning", "reasoning-local")
        prompt = {
            "request": state.request,
            "analysis": state.analysis,
            "files": [f.path for f in (state.context.files if state.context else [])[:80]],
        }
        llm.complete(model, "Create a concise implementation plan.", json.dumps(prompt, default=str))
        return Plan(
            steps=[
                "Inspect project conventions and relevant files.",
                "Create or modify the smallest set of files needed.",
                "Generate focused tests for the requested behavior.",
                "Run build, lint, tests, and review the final diff.",
            ],
            files_to_modify=_guess_files(state),
            test_strategy=_guess_tests(state),
            acceptance_criteria=[
                "Requested behavior is implemented.",
                "Relevant tests pass.",
                "Entire timeline explains what changed and why.",
                "Databricks observer receives trace/evaluation hooks when configured.",
            ],
        )


class CodingAgent:
    def run(self, state: AgentState, llm: LLMProvider) -> list[CodeArtifact]:
        if state.context and state.context.files:
            return [
                CodeArtifact(
                    "AGENT_PROPOSAL.md",
                    _proposal_markdown(state),
                    "For existing repositories, propose a reviewed patch plan before applying invasive edits.",
                )
            ]
        return [
            CodeArtifact("app/main.py", NEW_PROJECT_APP, "Minimal application entry point."),
            CodeArtifact("tests/test_app.py", NEW_PROJECT_TEST, "Smoke test for generated application."),
            CodeArtifact("README.md", _new_project_readme(state.request), "Project documentation."),
        ]


class TestGenerator:
    def run(self, state: AgentState) -> list[CodeArtifact]:
        if state.context and state.context.files:
            return [
                CodeArtifact(
                    "tests/test_agent_acceptance.py",
                    "def test_acceptance_placeholder():\n    assert True\n",
                    "Placeholder acceptance test for the workflow; replace with domain tests after review.",
                )
            ]
        return []


class ErrorAnalyzer:
    def run(self, state: AgentState) -> str:
        failures = [r for r in state.test_results if r.exit_code != 0]
        if not failures:
            return "No failures detected."
        return "\n\n".join(f"{r.command}\nSTDOUT:\n{r.stdout}\nSTDERR:\n{r.stderr}" for r in failures)


class CodeReviewer:
    def run(self, state: AgentState) -> ReviewResult:
        findings: list[str] = []
        if not state.artifacts:
            findings.append("No code artifacts were generated.")
        failed = [r for r in state.test_results if r.exit_code != 0]
        if failed:
            findings.append(f"{len(failed)} validation command(s) failed.")
        approved = not findings
        summary = "Ready for human approval." if approved else "Needs another debug or implementation pass."
        return ReviewResult(approved, findings, summary)


class DeploymentAgent:
    def run(self, state: AgentState) -> str | None:
        if state.apply_changes and state.review and state.review.approved:
            return "local://application-ready"
        return None


def _guess_files(state: AgentState) -> list[str]:
    if state.context and state.context.files:
        likely = [f.path for f in state.context.files if Path(f.path).name in {"README.md", "pyproject.toml", "package.json"}]
        return likely[:5] or ["AGENT_PROPOSAL.md"]
    return ["app/main.py", "tests/test_app.py", "README.md"]


def _guess_tests(state: AgentState) -> list[str]:
    commands = state.context.test_commands if state.context else []
    return commands or ["python -m pytest"]


def _proposal_markdown(state: AgentState) -> str:
    plan = state.plan or Plan([], [], [], [])
    return "\n".join(
        [
            "# Agent Proposal",
            "",
            f"Request: {state.request}",
            "",
            "## Plan",
            *[f"- {step}" for step in plan.steps],
            "",
            "## Files",
            *[f"- {path}" for path in plan.files_to_modify],
            "",
            "## Acceptance Criteria",
            *[f"- {item}" for item in plan.acceptance_criteria],
            "",
        ]
    )


def _new_project_readme(request: str) -> str:
    return f"# Generated Project\n\nRequest: {request}\n\nRun with:\n\n```bash\npython app/main.py\n```\n"


NEW_PROJECT_APP = """def handle_request(message: str) -> str:
    return f"Agent workflow received: {message}"


if __name__ == "__main__":
    print(handle_request("hello"))
"""

NEW_PROJECT_TEST = """from app.main import handle_request


def test_handle_request():
    assert "hello" in handle_request("hello")
"""
