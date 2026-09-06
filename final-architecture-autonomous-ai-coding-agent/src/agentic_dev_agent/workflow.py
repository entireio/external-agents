from __future__ import annotations

from pathlib import Path

from .agents import CodeReviewer, DeploymentAgent, ErrorAnalyzer, PlanExecutor, Planner, TaskAnalyzer
from .context import RepositoryScanner
from .databricks import DatabricksObserver
from .entire import EntireTimeline
from .memory import ConversationMemory
from .models import ModelRouter, make_provider
from .state import AgentState
from .tools import ToolLayer


def run_workflow(
    request: str,
    repo_path: str | Path,
    apply_changes: bool = False,
    max_debug_loops: int = 2,
) -> AgentState:
    state = AgentState(
        request=request,
        repo_path=Path(repo_path).resolve(),
        apply_changes=apply_changes,
        max_debug_loops=max_debug_loops,
    )
    timeline = EntireTimeline(state.repo_path)
    observer = DatabricksObserver()
    provider = make_provider()
    state.provider_name = getattr(provider, "name", provider.__class__.__name__)
    tools = ToolLayer(state.repo_path, apply_changes)
    memory = ConversationMemory(state.repo_path)

    timeline.record("user_prompt", {"request": request, "repo_path": state.repo_path})
    observer.trace("workflow_started", {"request": request, "provider": state.provider_name})

    state.context = RepositoryScanner().scan(state.repo_path)
    timeline.record("files_read", {"count": len(state.context.files), "docs": list(state.context.docs)})
    observer.trace("context_scanned", {"files": len(state.context.files), "package_managers": state.context.package_managers})

    state.analysis = TaskAnalyzer().run(state)
    timeline.record("task_analysis", {"analysis": state.analysis})

    state.selected_models = ModelRouter().route(state)
    timeline.record("model_route", {"models": state.selected_models, "provider": state.provider_name})

    state.conversation_history = memory.load_recent()
    timeline.record("conversation_memory_loaded", {"turns": len(state.conversation_history)})

    state.plan = Planner().run(state, provider)
    timeline.record("plan", {"plan": state.plan})

    state.artifacts.extend(PlanExecutor().run(state, provider))
    changed = tools.write_artifacts(state.artifacts)
    timeline.record("files_modified" if apply_changes else "files_proposed", {"files": changed})

    for command in state.plan.test_strategy if state.plan else []:
        result = tools.run(command)
        state.test_results.append(result)
        timeline.record("terminal_command", {"command": command, "exit_code": result.exit_code})
        if result.exit_code != 0:
            state.errors.append(ErrorAnalyzer().run(state))
            timeline.record("errors", {"errors": state.errors[-1]})
            break

    for _ in range(state.max_debug_loops):
        if not any(result.exit_code != 0 for result in state.test_results):
            break
        observer.trace("debug_loop", {"errors": state.errors[-1:]})
        break

    state.review = CodeReviewer().run(state)
    timeline.record("code_review", {"review": state.review})
    observer.evaluate("workflow_review_approved", 1.0 if state.review.approved else 0.0, {"findings": state.review.findings})

    state.deployment_url = DeploymentAgent().run(state)
    if state.deployment_url:
        timeline.record("deployment_event", {"url": state.deployment_url})

    observer.remember("last_workflow", {"request": request, "review": state.review.summary if state.review else None})
    memory.remember(
        {
            "request": request,
            "plan": state.plan,
            "artifacts": [{"path": artifact.path, "rationale": artifact.rationale} for artifact in state.artifacts],
            "review": state.review,
            "deployment_url": state.deployment_url,
        }
    )
    timeline.record("conversation_memory_saved", {"path": memory.path})
    observer.trace("workflow_completed", {"approved": state.review.approved if state.review else False})
    return state
