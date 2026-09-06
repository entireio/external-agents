from __future__ import annotations

import json
from pathlib import Path

from entire_agent_codetriage.hooks import (
    are_hooks_installed,
    evaluate_commit,
    install_hooks,
    parse_hook,
    uninstall_hooks,
)


def test_install_writes_git_pre_commit_hook(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENTIRE_REPO_ROOT", str(tmp_path))
    (tmp_path / ".git" / "hooks").mkdir(parents=True)
    legacy = tmp_path / ".git" / "hooks" / "pre-commit-codetriage"
    legacy.write_text("#!/bin/sh\necho leftover\n", encoding="utf-8")

    assert install_hooks(root=tmp_path) == 3
    hook = tmp_path / ".git" / "hooks" / "pre-commit"
    assert hook.is_file()
    text = hook.read_text(encoding="utf-8")
    assert "entire-agent-codetriage parse-hook --hook commit" in text
    assert not legacy.exists()

    assert install_hooks(root=tmp_path) == 0
    assert hook.is_file()

    uninstall_hooks(tmp_path)
    assert not hook.exists()
    assert are_hooks_installed(tmp_path) is False


def test_install_uninstall_roundtrip(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENTIRE_REPO_ROOT", str(tmp_path))
    assert are_hooks_installed(tmp_path) is False
    assert install_hooks(root=tmp_path) == 3
    assert are_hooks_installed(tmp_path) is True
    assert install_hooks(root=tmp_path) == 0
    uninstall_hooks(tmp_path)
    assert are_hooks_installed(tmp_path) is False


def test_parse_start_and_stop() -> None:
    payload = json.dumps(
        {
            "hook_type": "start",
            "session_id": "s1",
            "timestamp": "2026-09-06T12:00:00Z",
            "user_prompt": "hello",
        }
    ).encode()
    start = parse_hook("start", payload)
    assert start is not None
    assert start["type"] == 1
    assert start["session_id"] == "s1"

    stop = parse_hook("stop", payload)
    assert stop is not None
    assert stop["type"] == 5


def test_parse_empty_is_null() -> None:
    assert parse_hook("commit", b"") is None


def test_commit_blocks_on_critical_graph(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("ENTIRE_REPO_ROOT", str(tmp_path))
    monkeypatch.setenv("CODETRIAGE_DISABLE_MLFLOW", "1")
    graph = tmp_path / ".codetriage" / "graph.json"
    graph.parent.mkdir(parents=True)
    chain = {
        "reverse_dependencies": {
            "core.py": ["a.py"],
            "a.py": ["b.py"],
            "b.py": ["c.py"],
        }
    }
    graph.write_text(json.dumps(chain), encoding="utf-8")
    payload = {
        "session_id": "gate",
        "timestamp": "2026-09-06T12:00:00Z",
        "modified_files": ["core.py"],
    }
    result = evaluate_commit(payload)
    assert result.blocked is True
    event = parse_hook("commit", json.dumps(payload).encode())
    assert event is not None
    assert event["metadata"]["blocked"] == "true"
    assert "ESI Level 1" in event["response_message"]
