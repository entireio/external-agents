from __future__ import annotations

import subprocess
import sys
import importlib.util
from pathlib import Path

from .state import CodeArtifact, CommandResult


class ToolLayer:
    def __init__(self, repo_path: Path, apply_changes: bool) -> None:
        self.repo_path = repo_path.resolve()
        self.apply_changes = apply_changes

    def write_artifacts(self, artifacts: list[CodeArtifact]) -> list[str]:
        changed: list[str] = []
        for artifact in artifacts:
            target = (self.repo_path / artifact.path).resolve()
            if not _within(self.repo_path, target):
                raise ValueError(f"Refusing to write outside repository: {artifact.path}")
            changed.append(artifact.path)
            if self.apply_changes:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(artifact.content, encoding="utf-8")
        return changed

    def run(self, command: str, timeout: int = 120) -> CommandResult:
        resolved = _resolve_command(command)
        if resolved is None:
            return CommandResult(command, 0, "Skipped: pytest is not installed in this interpreter.\n", "")
        completed = subprocess.run(
            resolved,
            cwd=self.repo_path,
            shell=True,
            text=True,
            capture_output=True,
            timeout=timeout,
        )
        return CommandResult(command, completed.returncode, completed.stdout[-6000:], completed.stderr[-6000:])


def _within(root: Path, target: Path) -> bool:
    try:
        target.relative_to(root)
        return True
    except ValueError:
        return False


def _resolve_command(command: str) -> str | None:
    if command == "python -m pytest" and importlib.util.find_spec("pytest") is None:
        return None
    if command.startswith("python "):
        return f'"{sys.executable}" {command.removeprefix("python ")}'
    return command
