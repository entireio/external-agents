from __future__ import annotations

import json
from dataclasses import asdict, is_dataclass
from pathlib import Path
from typing import Any

import streamlit as st

from .workflow import run_workflow


DEFAULT_REQUEST = "Create a FastAPI health check endpoint with tests"


def main() -> None:
    st.set_page_config(
        page_title="DODO AI Coding Agent",
        page_icon=":material/code:",
        layout="wide",
        initial_sidebar_state="expanded",
    )
    _styles()

    st.title("Autonomous AI Coding Agent")
    st.caption("Context scan -> plan -> route models -> code -> test -> debug -> review -> timeline")

    with st.sidebar:
        st.header("Run")
        repo_path = st.text_input("Repository path", value=str(Path.cwd()))
        request = st.text_area("Natural language request", value=DEFAULT_REQUEST, height=140)
        mode = st.radio("Change mode", ["Dry run", "Apply changes"], horizontal=False)
        max_debug_loops = st.slider("Debug loops", min_value=0, max_value=5, value=2)
        run_clicked = st.button("Run workflow", type="primary", use_container_width=True)

        st.divider()
        st.header("Integrations")
        st.caption("Enabled through environment variables")
        st.code("OPENAI_API_KEY\nDATABRICKS_*\nENTIRE_*", language="text")
        st.caption("Conversation memory is saved per repository in `.entire/conversation.jsonl`.")

    if "last_state" not in st.session_state:
        st.session_state.last_state = None
        st.session_state.last_timeline = []

    if run_clicked:
        _run(repo_path, request, mode == "Apply changes", max_debug_loops)

    if st.session_state.last_state is None:
        _empty_state()
        return

    state = st.session_state.last_state
    _summary(state)
    _tabs(state, st.session_state.last_timeline)


def _run(repo_path: str, request: str, apply_changes: bool, max_debug_loops: int) -> None:
    if not request.strip():
        st.error("Enter a development request before running the workflow.")
        return

    target = Path(repo_path).expanduser()
    target.mkdir(parents=True, exist_ok=True)

    with st.status("Running autonomous workflow", expanded=True) as status:
        st.write("Scanning repository and documentation")
        st.write("Analyzing task and planning implementation")
        st.write("Routing model roles")
        st.write("Generating code and tests")
        st.write("Executing validation and review")
        try:
            state = run_workflow(
                request.strip(),
                target,
                apply_changes=apply_changes,
                max_debug_loops=max_debug_loops,
            )
        except Exception as exc:  # pragma: no cover - Streamlit surface
            status.update(label="Workflow failed", state="error")
            st.exception(exc)
            return

        timeline_path = state.repo_path / ".entire" / "timeline.jsonl"
        st.session_state.last_state = state
        st.session_state.last_timeline = _read_timeline(timeline_path)
        status.update(label="Workflow complete", state="complete")


def _empty_state() -> None:
    left, middle, right = st.columns(3)
    left.metric("Context", "Waiting")
    middle.metric("Review", "Not run")
    right.metric("Timeline", "0 events")

    st.info("Enter a repository path and request, then run the workflow.")
    st.subheader("Architecture Flow")
    st.graphviz_chart(
        """
        digraph {
          rankdir=LR;
          node [shape=box, style="rounded,filled", color="#ccd3dc", fillcolor="#f8fafc"];
          User -> Context -> Analyzer -> Planner -> Router -> Coder -> Tests -> Review -> Approval -> Deploy;
          Coder -> Entire;
          Tests -> Entire;
          Review -> Databricks;
          Databricks -> Entire;
        }
        """
    )


def _summary(state: Any) -> None:
    context = state.context
    review = state.review
    tests_failed = sum(1 for result in state.test_results if result.exit_code != 0)
    tests_passed = len(state.test_results) - tests_failed

    a, b, c, d = st.columns(4)
    a.metric("Files scanned", len(context.files) if context else 0)
    b.metric("Task risk", state.analysis.risk if state.analysis else "unknown")
    c.metric("Validation", f"{tests_passed} pass / {tests_failed} fail")
    d.metric("Review", "Approved" if review and review.approved else "Needs work")
    st.caption(f"LLM provider: `{state.provider_name}`")
    if state.provider_name == "local-deterministic":
        st.warning("OpenAI is not configured for this run; generated output is local deterministic demo data.")

    if review and review.approved:
        st.success(review.summary)
    elif review:
        st.warning(review.summary)


def _tabs(state: Any, timeline: list[dict[str, Any]]) -> None:
    overview, plan, artifacts, tests, memory, timeline_tab, raw = st.tabs(
        ["Overview", "Plan", "Artifacts", "Tests", "Memory", "Entire Timeline", "Raw State"]
    )

    with overview:
        col1, col2 = st.columns([1, 1])
        with col1:
            st.subheader("Task Analysis")
            st.json(_to_jsonable(state.analysis))
        with col2:
            st.subheader("Model Routing")
            st.json(state.selected_models)

        st.subheader("Project Context")
        context = state.context
        if context:
            st.write(f"Repository: `{context.repo_path}`")
            st.write(", ".join(context.package_managers) or "No package manager detected")
            st.dataframe(
                [_to_jsonable(file) for file in context.files[:200]],
                use_container_width=True,
                hide_index=True,
            )

    with plan:
        if state.plan:
            st.subheader("Implementation Steps")
            for index, step in enumerate(state.plan.steps, start=1):
                st.write(f"{index}. {step}")
            st.subheader("Files To Modify")
            st.table({"path": state.plan.files_to_modify})
            st.subheader("Acceptance Criteria")
            for item in state.plan.acceptance_criteria:
                st.checkbox(item, value=False, disabled=True)

    with artifacts:
        for artifact in state.artifacts:
            with st.expander(artifact.path, expanded=True):
                st.caption(artifact.rationale)
                st.code(artifact.content, language=_language_for_path(artifact.path))

    with tests:
        if not state.test_results:
            st.info("No validation commands were run.")
        for result in state.test_results:
            label = "PASS" if result.exit_code == 0 else "FAIL"
            with st.expander(f"{label} - {result.command}", expanded=result.exit_code != 0):
                st.write(f"Exit code: `{result.exit_code}`")
                if result.stdout:
                    st.code(result.stdout, language="text")
                if result.stderr:
                    st.code(result.stderr, language="text")

    with memory:
        history = state.conversation_history
        if not history:
            st.info("No prior conversation memory found for this repository.")
        for turn in reversed(history[-10:]):
            label = turn.get("request", "Previous run")
            with st.expander(label, expanded=False):
                st.json(_to_jsonable(turn))

    with timeline_tab:
        if not timeline:
            st.info("No Entire events found yet.")
        else:
            st.dataframe(timeline, use_container_width=True, hide_index=True)
            st.download_button(
                "Download timeline JSON",
                data=json.dumps(timeline, indent=2),
                file_name="entire-timeline.json",
                mime="application/json",
            )

    with raw:
        st.json(_to_jsonable(state))


def _read_timeline(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    events: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if line.strip():
            events.append(json.loads(line))
    return events


def _to_jsonable(value: Any) -> Any:
    if is_dataclass(value):
        return _to_jsonable(asdict(value))
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, dict):
        return {str(key): _to_jsonable(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_to_jsonable(item) for item in value]
    return value


def _language_for_path(path: str) -> str:
    suffix = Path(path).suffix.lower()
    return {
        ".py": "python",
        ".md": "markdown",
        ".json": "json",
        ".toml": "toml",
        ".yaml": "yaml",
        ".yml": "yaml",
    }.get(suffix, "text")


def _styles() -> None:
    st.markdown(
        """
        <style>
        .block-container {
            padding-top: 1.5rem;
        }
        [data-testid="stMetric"] {
            background: #f8fafc;
            border: 1px solid #e5e7eb;
            padding: 0.8rem 1rem;
            border-radius: 8px;
        }
        div[data-testid="stExpander"] {
            border-radius: 8px;
        }
        </style>
        """,
        unsafe_allow_html=True,
    )
