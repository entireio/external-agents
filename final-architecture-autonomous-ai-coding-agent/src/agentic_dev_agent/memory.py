from __future__ import annotations

import json
import time
from dataclasses import asdict, is_dataclass
from pathlib import Path
from typing import Any


class ConversationMemory:
    """Persists compact conversation turns for future planning runs."""

    def __init__(self, repo_path: Path, limit: int = 20) -> None:
        self.path = repo_path / ".entire" / "conversation.jsonl"
        self.limit = limit
        self.path.parent.mkdir(parents=True, exist_ok=True)

    def load_recent(self) -> list[dict[str, Any]]:
        if not self.path.exists():
            return []
        turns: list[dict[str, Any]] = []
        for line in self.path.read_text(encoding="utf-8", errors="replace").splitlines():
            if not line.strip():
                continue
            try:
                turns.append(json.loads(line))
            except json.JSONDecodeError:
                continue
        return turns[-self.limit :]

    def remember(self, turn: dict[str, Any]) -> None:
        event = {
            "ts": time.time(),
            **_jsonable(turn),
        }
        with self.path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(event, sort_keys=True) + "\n")


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
