from pathlib import Path

from agentic_dev_agent.workflow import run_workflow


def test_new_project_workflow_generates_artifacts(tmp_path: Path, monkeypatch):
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)

    state = run_workflow("Create a tiny app", tmp_path, apply_changes=True)

    assert (tmp_path / "app" / "main.py").exists()
    assert (tmp_path / ".entire" / "timeline.jsonl").exists()
    assert (tmp_path / ".entire" / "conversation.jsonl").exists()
    assert state.review is not None


def test_existing_repo_dry_run_does_not_write_proposal(tmp_path: Path, monkeypatch):
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)

    (tmp_path / "README.md").write_text("# Demo\n", encoding="utf-8")

    state = run_workflow("Update the docs", tmp_path, apply_changes=False)

    assert any(artifact.path == "AGENT_PROPOSAL.md" for artifact in state.artifacts)
    assert not (tmp_path / "AGENT_PROPOSAL.md").exists()


def test_workflow_loads_previous_conversation_memory(tmp_path: Path, monkeypatch):
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)

    run_workflow("Create a tiny app", tmp_path, apply_changes=True)
    state = run_workflow("Add a second feature using prior context", tmp_path, apply_changes=False)

    assert state.conversation_history
    assert state.conversation_history[-1]["request"] == "Create a tiny app"


def test_workflow_uses_provider_plan_and_executes_artifacts(tmp_path: Path, monkeypatch):
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)

    class StructuredProvider:
        def complete(self, model: str, system: str, prompt: str) -> str:
            if "approved implementation plan" not in system and "implementation plan" in system:
                return """
                {
                  "steps": ["Create the requested generated file."],
                  "files_to_modify": ["generated.txt"],
                  "test_strategy": [],
                  "acceptance_criteria": ["generated.txt exists"]
                }
                """
            return """
            {
              "artifacts": [
                {
                  "path": "generated.txt",
                  "content": "created from provider plan\\n",
                  "rationale": "Executes the structured provider plan."
                }
              ]
            }
            """

    monkeypatch.setattr("agentic_dev_agent.workflow.make_provider", lambda: StructuredProvider())

    state = run_workflow("Create a generated text file", tmp_path, apply_changes=True)

    assert state.plan is not None
    assert state.plan.steps == ["Create the requested generated file."]
    assert (tmp_path / "generated.txt").read_text(encoding="utf-8") == "created from provider plan\n"
