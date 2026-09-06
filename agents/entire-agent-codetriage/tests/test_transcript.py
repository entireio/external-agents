from __future__ import annotations

import json
from pathlib import Path

from entire_agent_codetriage.hooks import (
    evaluate_commit,
    identify_modified_files,
    parse_hook,
)
from entire_agent_codetriage.transcript import adapt_raw


def _acme_jsonl(*events: dict) -> bytes:
    return "".join(json.dumps(event) + "\n" for event in events).encode()


def test_original_entire_lifecycle_format(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENTIRE_REPO_ROOT", str(tmp_path))
    monkeypatch.setenv("CODETRIAGE_DISABLE_MLFLOW", "1")
    payload = {
        "hook_type": "commit",
        "session_id": "entire-1",
        "timestamp": "2026-09-06T12:00:00Z",
        "modified_files": ["src/util.py"],
        "tool_input": {"path": "src/app.py"},
    }
    files = identify_modified_files(payload)
    assert "src/util.py" in files
    assert "src/app.py" in files

    event = parse_hook("commit", json.dumps(payload).encode())
    assert event is not None
    assert event["session_id"] == "entire-1"
    assert event["type"] == 3
    assert event["metadata"]["blocked"] == "false"
    assert event["metadata"]["decision"] == "allow"


def test_acmecode_jsonl_file_changed_events(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENTIRE_REPO_ROOT", str(tmp_path))
    monkeypatch.setenv("CODETRIAGE_DISABLE_MLFLOW", "1")
    raw = _acme_jsonl(
        {
            "event": "session_start",
            "session_id": "acme-1",
            "timestamp": "2026-09-06T12:00:00Z",
        },
        {"event": "file_changed", "path": "src/core.py"},
        {"type": "file_changed", "file_path": "src/api.py"},
        {"event": "file_changed", "data": {"path": "src/jobs.py"}},
    )
    payload = adapt_raw(raw)
    assert payload is not None
    assert payload["session_id"] == "acme-1"
    files = identify_modified_files(payload)
    assert files == ["src/core.py", "src/api.py", "src/jobs.py"]

    event = parse_hook("commit", raw)
    assert event is not None
    assert event["session_id"] == "acme-1"
    assert event["metadata"]["decision"] in {"allow", "block"}


def test_unknown_json_events_are_skipped(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENTIRE_REPO_ROOT", str(tmp_path))
    monkeypatch.setenv("CODETRIAGE_DISABLE_MLFLOW", "1")
    raw = _acme_jsonl(
        {"event": "telemetry", "cpu": 0.4},
        {"event": "mystery", "foo": True},
        {"event": "file_changed", "path": "keep.py"},
        {"not": "an event we know", "value": [1, 2, 3]},
    )
    blob = raw + b'"just-a-string"\ntrue\n[1,2]\nnot-json-at-all\n42\n'
    event = parse_hook("commit", blob)
    assert event is not None
    files = identify_modified_files(adapt_raw(blob) or {})
    assert files == ["keep.py"]


def test_truncated_jsonl_returns_partial_result(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENTIRE_REPO_ROOT", str(tmp_path))
    monkeypatch.setenv("CODETRIAGE_DISABLE_MLFLOW", "1")
    raw = (
        json.dumps({"event": "session_start", "session_id": "partial-1"})
        + "\n"
        + json.dumps({"event": "file_changed", "path": "ok.py"})
        + "\n"
        + json.dumps({"event": "file_changed", "path": "also.py"})[:-8]
        + "\n"
        + '{"event":"file_changed","path":"never'
    )
    payload = adapt_raw(raw.encode())
    assert payload is not None
    assert payload["session_id"] == "partial-1"
    files = identify_modified_files(payload)
    assert files == ["ok.py"]
    event = parse_hook("commit", raw.encode())
    assert event is not None
    assert event["session_id"] == "partial-1"
    assert "metadata" in event


def test_jsonl_file_changed_still_triggers_esi_level_1(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENTIRE_REPO_ROOT", str(tmp_path))
    monkeypatch.setenv("CODETRIAGE_DISABLE_MLFLOW", "1")
    graph = tmp_path / ".codetriage" / "graph.json"
    graph.parent.mkdir(parents=True)
    graph.write_text(
        json.dumps(
            {
                "reverse_dependencies": {
                    "core.py": ["a.py"],
                    "a.py": ["b.py"],
                    "b.py": ["c.py"],
                }
            }
        ),
        encoding="utf-8",
    )
    raw = _acme_jsonl(
        {"event": "session_start", "session_id": "gate"},
        {"event": "file_changed", "path": "core.py"},
    )
    result = evaluate_commit(adapt_raw(raw) or {})
    assert result.blocked is True
    event = parse_hook("commit", raw)
    assert event is not None
    assert event["metadata"]["blocked"] == "true"
    assert event["metadata"]["esi_level"] == "1"
