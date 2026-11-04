from __future__ import annotations

import json
import os
import time
from typing import Any


class DatabricksObserver:
    """Thin adapter for Databricks observability, evaluation, and memory hooks."""

    def __init__(self) -> None:
        self.enabled = bool(
            os.getenv("DATABRICKS_SERVER_HOSTNAME")
            and os.getenv("DATABRICKS_HTTP_PATH")
            and os.getenv("DATABRICKS_TOKEN")
        )
        self.trace_table = os.getenv("DATABRICKS_TRACE_TABLE", "agent_traces")

    def trace(self, stage: str, payload: dict[str, Any]) -> None:
        event = {
            "ts": time.time(),
            "stage": stage,
            "payload": payload,
        }
        if not self.enabled:
            return
        self._write_event(event)

    def evaluate(self, name: str, score: float, metadata: dict[str, Any]) -> None:
        self.trace("evaluation", {"name": name, "score": score, "metadata": metadata})

    def remember(self, key: str, value: dict[str, Any]) -> None:
        self.trace("memory", {"key": key, "value": value})

    def _write_event(self, event: dict[str, Any]) -> None:
        try:
            from databricks import sql  # type: ignore
        except ImportError:
            return

        with sql.connect(
            server_hostname=os.environ["DATABRICKS_SERVER_HOSTNAME"],
            http_path=os.environ["DATABRICKS_HTTP_PATH"],
            access_token=os.environ["DATABRICKS_TOKEN"],
        ) as connection:
            with connection.cursor() as cursor:
                cursor.execute(
                    f"INSERT INTO {self.trace_table} (ts, stage, payload_json) VALUES (?, ?, ?)",
                    (event["ts"], event["stage"], json.dumps(event["payload"])),
                )
