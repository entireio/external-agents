"""Exercise the real CrewAI event bus and listener without an LLM provider."""

from datetime import datetime, timezone
import json
from pathlib import Path

from crewai.events import (
    CrewKickoffCompletedEvent,
    CrewKickoffStartedEvent,
    ToolUsageFinishedEvent,
    ToolUsageStartedEvent,
    crewai_event_bus,
)
from entire_adapter import EntireCrewAIListener


def emit(event):
    # CrewAI dispatches handlers on a thread pool. Wait between events so the
    # start hook has completed before the fixture changes the worktree.
    future = crewai_event_bus.emit(None, event)
    if future is None:
        raise RuntimeError(f"No listener registered for {event.type}")
    future.result(timeout=30)


def main():
    with crewai_event_bus.scoped_handlers():
        listener = EntireCrewAIListener(
            agent_label="e2e-crewai",
            repo_path=str(Path.cwd()),
            strict=True,
            checkpoint_policy={"write_file": "always"},
        )
        try:
            emit(CrewKickoffStartedEvent(
                crew_name="fixture", inputs={"prompt": "Create crewai-output.txt"},
            ))
            tool_args = {"path": "crewai-output.txt", "content": "hello from CrewAI\n"}
            tool_context = {
                "tool_name": "write_file", "tool_args": tool_args,
                "task_id": "fixture-task", "agent_id": "fixture-agent", "run_attempts": 1,
            }
            started_at = datetime.now(timezone.utc)
            emit(ToolUsageStartedEvent(**tool_context))
            Path(tool_args["path"]).write_text(tool_args["content"], encoding="utf-8")
            emit(ToolUsageFinishedEvent(
                **tool_context, started_at=started_at,
                finished_at=datetime.now(timezone.utc), output="wrote crewai-output.txt",
            ))
            emit(CrewKickoffCompletedEvent(crew_name="fixture", output="fixture complete"))
            print(json.dumps({"session_id": listener.session_id, "session_ref": listener.session_ref}))
        finally:
            listener.close()


if __name__ == "__main__":
    main()
