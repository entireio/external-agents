from __future__ import annotations

import json
from dataclasses import asdict, is_dataclass
from pathlib import Path
from typing import Any

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
            "analysis": _jsonable(state.analysis),
            "conversation_history": state.conversation_history,
            "docs": state.context.docs if state.context else {},
            "files": [f.path for f in (state.context.files if state.context else [])[:80]],
        }
        response = llm.complete(
            model,
            (
                "Create a concise implementation plan. Return only JSON with keys "
                "steps, files_to_modify, test_strategy, and acceptance_criteria. "
                "Each value must be an array of strings."
            ),
            json.dumps(prompt, default=str),
        )
        parsed = _extract_json_object(response)
        if parsed:
            plan = _plan_from_json(parsed)
            if plan:
                return plan
        if getattr(llm, "name", "") != "local-deterministic":
            raise RuntimeError(
                "The LLM did not return a valid JSON plan. Update the prompt/model or retry; "
                "the workflow will not use the hardcoded local plan while OpenAI is configured."
            )
        return _fallback_plan(state)


class PlanExecutor:
    def run(self, state: AgentState, llm: LLMProvider) -> list[CodeArtifact]:
        model = state.selected_models.get("coding", "coding-local")
        prompt = {
            "request": state.request,
            "analysis": _jsonable(state.analysis),
            "plan": _jsonable(state.plan),
            "conversation_history": state.conversation_history,
            "docs": state.context.docs if state.context else {},
            "files": [f.path for f in (state.context.files if state.context else [])[:120]],
            "instructions": [
                "Generate complete file contents for every artifact needed to execute the plan.",
                "Use repository-relative paths.",
                "Do not include files outside the repository.",
            ],
        }
        response = llm.complete(
            model,
            (
                "You are executing the approved implementation plan. Return only JSON with an "
                "artifacts array. Each item must have path, content, and rationale strings."
            ),
            json.dumps(prompt, default=str),
        )
        artifacts = _artifacts_from_json(_extract_json_object(response))
        if artifacts:
            return artifacts
        if getattr(llm, "name", "") != "local-deterministic":
            raise RuntimeError(
                "The LLM did not return valid JSON artifacts. The workflow will not use "
                "hardcoded generated files while OpenAI is configured."
            )
        return CodingAgent().run(state, llm)


def _fallback_plan(state: AgentState) -> Plan:
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


def _plan_from_json(data: dict[str, Any]) -> Plan | None:
    steps = _string_list(data.get("steps"))
    files_to_modify = _string_list(data.get("files_to_modify"))
    test_strategy = _string_list(data.get("test_strategy"))
    acceptance_criteria = _string_list(data.get("acceptance_criteria"))
    if not steps:
        return None
    return Plan(
        steps=steps,
        files_to_modify=files_to_modify,
        test_strategy=test_strategy,
        acceptance_criteria=acceptance_criteria,
    )


def _artifacts_from_json(data: dict[str, Any] | None) -> list[CodeArtifact]:
    if not data:
        return []
    raw_artifacts = data.get("artifacts")
    if not isinstance(raw_artifacts, list):
        return []
    artifacts: list[CodeArtifact] = []
    for item in raw_artifacts:
        if not isinstance(item, dict):
            continue
        path = item.get("path")
        content = item.get("content")
        rationale = item.get("rationale", "Generated by the plan executor.")
        if isinstance(path, str) and isinstance(content, str) and isinstance(rationale, str):
            artifacts.append(CodeArtifact(path, content, rationale))
    return artifacts


def _string_list(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and item.strip()]


def _extract_json_object(text: str) -> dict[str, Any] | None:
    decoder = json.JSONDecoder()
    cleaned = text.strip()
    if cleaned.startswith("```"):
        cleaned = cleaned.removeprefix("```json").removeprefix("```").strip()
        if cleaned.endswith("```"):
            cleaned = cleaned[:-3].strip()
    for index, char in enumerate(cleaned):
        if char != "{":
            continue
        try:
            value, _ = decoder.raw_decode(cleaned[index:])
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            return value
    return None


def _jsonable(value: Any) -> Any:
    if is_dataclass(value):
        return _jsonable(asdict(value))
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, dict):
        return {str(key): _jsonable(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_jsonable(item) for item in value]
    return value


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
