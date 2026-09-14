from pathlib import Path

from agentic_dev_agent.workflow import run_workflow


def test_new_project_workflow_generates_artifacts(tmp_path: Path):
    state = run_workflow("Create a tiny app", tmp_path, apply_changes=True)

    assert (tmp_path / "app" / "main.py").exists()
    assert (tmp_path / ".entire" / "timeline.jsonl").exists()
    assert state.review is not None


def test_existing_repo_dry_run_does_not_write_proposal(tmp_path: Path):
    (tmp_path / "README.md").write_text("# Demo\n", encoding="utf-8")

    state = run_workflow("Update the docs", tmp_path, apply_changes=False)

    assert any(artifact.path == "AGENT_PROPOSAL.md" for artifact in state.artifacts)
    assert not (tmp_path / "AGENT_PROPOSAL.md").exists()
