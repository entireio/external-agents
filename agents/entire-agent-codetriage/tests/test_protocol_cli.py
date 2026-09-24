from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

AGENT_DIR = Path(__file__).resolve().parents[1]
LAUNCHER = AGENT_DIR / "entire-agent-codetriage"


def run_agent(
    args: list[str],
    stdin: bytes | None = None,
    repo: Path | None = None,
) -> subprocess.CompletedProcess[bytes]:
    env = os.environ.copy()
    env["PYTHONPATH"] = str(AGENT_DIR / "src") + os.pathsep + env.get("PYTHONPATH", "")
    env["CODETRIAGE_DISABLE_MLFLOW"] = "1"
    if repo is not None:
        env["ENTIRE_REPO_ROOT"] = str(repo)
    return subprocess.run(
        [sys.executable, str(LAUNCHER), *args],
        input=stdin,
        capture_output=True,
        cwd=str(repo or AGENT_DIR),
        env=env,
        check=False,
    )


def test_info_shape() -> None:
    res = run_agent(["info"])
    assert res.returncode == 0
    info = json.loads(res.stdout)
    assert info["protocol_version"] == 1
    assert info["name"] == "codetriage"
    assert info["capabilities"]["hooks"] is True
    assert info["hook_names"] == ["start", "stop", "commit"]


def test_unknown_subcommand_fails() -> None:
    res = run_agent(["this-subcommand-does-not-exist"])
    assert res.returncode != 0
    assert res.stderr


def test_no_subcommand_fails() -> None:
    res = run_agent([])
    assert res.returncode != 0


def test_session_roundtrip(tmp_path: Path) -> None:
    env_repo = tmp_path
    dir_res = run_agent(["get-session-dir", "--repo-path", str(env_repo)], repo=env_repo)
    session_dir = json.loads(dir_res.stdout)["session_dir"]
    file_res = run_agent(
        ["resolve-session-file", "--session-dir", session_dir, "--session-id", "abc123"],
        repo=env_repo,
    )
    session_file = json.loads(file_res.stdout)["session_file"]
    assert "abc123" in session_file

    session = {
        "session_id": "abc123",
        "agent_name": "codetriage",
        "repo_path": str(env_repo),
        "session_ref": session_file,
        "start_time": "2026-09-06T12:00:00Z",
        "native_data": "eyJ0ZXN0IjogdHJ1ZX0=",
        "modified_files": ["file1.go"],
        "new_files": ["file2.go"],
        "deleted_files": [],
    }
    write = run_agent(["write-session"], stdin=json.dumps(session).encode(), repo=env_repo)
    assert write.returncode == 0
    hook = {
        "hook_type": "agent-spawn",
        "session_id": "abc123",
        "session_ref": session_file,
    }
    read = run_agent(["read-session"], stdin=json.dumps(hook).encode(), repo=env_repo)
    assert read.returncode == 0
    back = json.loads(read.stdout)
    assert back["session_id"] == "abc123"
    assert back["modified_files"] == ["file1.go"]
    assert back["new_files"] is not None
    assert back["deleted_files"] is not None


def test_chunk_and_reassemble() -> None:
    original = (b"The quick brown fox jumps over the lazy dog.\n") * 100
    chunked = run_agent(["chunk-transcript", "--max-size", "512"], stdin=original)
    assert chunked.returncode == 0
    payload = json.loads(chunked.stdout)
    assert len(payload["chunks"]) >= 2
    rebuilt = run_agent(["reassemble-transcript"], stdin=json.dumps(payload).encode())
    assert rebuilt.returncode == 0
    assert rebuilt.stdout == original


def test_invalid_json_and_max_size_fail() -> None:
    assert run_agent(["get-session-id"], stdin=b'{"session_id":').returncode != 0
    assert run_agent(["chunk-transcript", "--max-size", "0"], stdin=b"x").returncode != 0


def test_hooks_and_commit_rejection(tmp_path: Path) -> None:
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
    installed = run_agent(["install-hooks"], repo=tmp_path)
    assert json.loads(installed.stdout)["hooks_installed"] == 3
    status = run_agent(["are-hooks-installed"], repo=tmp_path)
    assert json.loads(status.stdout)["installed"] is True

    payload = {
        "hook_type": "commit",
        "session_id": "s-block",
        "timestamp": "2026-09-06T12:00:00Z",
        "modified_files": ["core.py"],
    }
    blocked = run_agent(["parse-hook", "--hook", "commit"], stdin=json.dumps(payload).encode(), repo=tmp_path)
    assert blocked.returncode != 0
    event = json.loads(blocked.stdout)
    assert event["metadata"]["blocked"] == "true"
    assert event["metadata"]["esi_level"] == "1"


def test_parse_hook_accepts_acmecode_jsonl(tmp_path: Path) -> None:
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
    jsonl = (
        json.dumps({"event": "session_start", "session_id": "acme-cli"})
        + "\n"
        + json.dumps({"event": "file_changed", "path": "core.py"})
        + "\n"
    )
    blocked = run_agent(["parse-hook", "--hook", "commit"], stdin=jsonl.encode(), repo=tmp_path)
    assert blocked.returncode != 0
    event = json.loads(blocked.stdout)
    assert event["session_id"] == "acme-cli"
    assert event["metadata"]["blocked"] == "true"
