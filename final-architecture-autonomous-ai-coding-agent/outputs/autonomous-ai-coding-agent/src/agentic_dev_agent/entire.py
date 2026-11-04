from __future__ import annotations

import json
import os
import time
import urllib.request
from dataclasses import asdict, is_dataclass
from pathlib import Path
from typing import Any


class EntireTimeline:
    """Records the development timeline: what changed, why, and how it was verified."""

    def __init__(self, repo_path: Path) -> None:
        self.repo_path = repo_path
        self.local_path = repo_path / ".entire" / "timeline.jsonl"
        self.local_path.parent.mkdir(parents=True, exist_ok=True)
        self.api_url = os.getenv("ENTIRE_API_URL")
        self.api_key = os.getenv("ENTIRE_API_KEY")

    def record(self, event_type: str, payload: dict[str, Any], post_remote: bool = True) -> None:
        event = {
            "ts": time.time(),
            "event_type": event_type,
            "payload": _jsonable(payload),
        }
        with self.local_path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(event, sort_keys=True) + "\n")
        if post_remote and self.api_url and self.api_key:
            self._post(event)

    def _post(self, event: dict[str, Any]) -> None:
        body = json.dumps(event).encode("utf-8")
        request = urllib.request.Request(
            self.api_url,
            data=body,
            headers={
                "Authorization": f"Bearer {self.api_key}",
                "Content-Type": "application/json",
            },
            method="POST",
        )
        try:
            urllib.request.urlopen(request, timeout=5).read()
        except OSError:
            self.record("entire_delivery_failed", {"api_url": self.api_url}, post_remote=False)


def _jsonable(value: Any) -> Any:
    if is_dataclass(value):
        return _jsonable(asdict(value))
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, dict):
        return {str(k): _jsonable(v) for k, v in value.items()}
    if isinstance(value, list):
        return [_jsonable(v) for v in value]
    return value
