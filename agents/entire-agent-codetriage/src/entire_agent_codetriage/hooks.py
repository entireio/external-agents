"""start / stop / commit hook install + parse + ESI commit gate."""

from __future__ import annotations

import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from entire_agent_codetriage import HOOK_NAMES
from entire_agent_codetriage.blast_radius import BlastRadius, compute_blast_radius
from entire_agent_codetriage.env import repo_root
from entire_agent_codetriage.graph import normalize_path
from entire_agent_codetriage.telemetry import log_esi_run
from entire_agent_codetriage.transcript import adapt_payload, adapt_raw, coerce_files

HOOK_MARKER = ".codetriage/hooks.json"
GIT_HOOK_NAME = "pre-commit"
LEGACY_GIT_HOOK_NAME = "pre-commit-codetriage"

EVENT_SESSION_START = 1
EVENT_TURN_END = 3
EVENT_SESSION_END = 5


def hook_marker_path(root: Path | None = None) -> Path:
    return (root or repo_root()) / HOOK_MARKER


def are_hooks_installed(root: Path | None = None) -> bool:
    path = hook_marker_path(root)
    if not path.is_file():
        return False
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return False
    names = data.get("hooks") or []
    return all(name in names for name in HOOK_NAMES)


def install_hooks(force: bool = False, local_dev: bool = False, root: Path | None = None) -> int:
    del local_dev  # protocol flag is a no-op
    repo = root or repo_root()
    path = hook_marker_path(repo)
    if path.is_file() and not force:
        if are_hooks_installed(repo):
            _write_git_commit_hook(repo)
            return 0
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = {
        "agent": "codetriage",
        "hooks": list(HOOK_NAMES),
        "installed_at": _now(),
        "commands": {
            "start": "entire-agent-codetriage parse-hook --hook start",
            "stop": "entire-agent-codetriage parse-hook --hook stop",
            "commit": "entire-agent-codetriage parse-hook --hook commit",
        },
    }
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    _write_git_commit_hook(repo)
    return len(HOOK_NAMES)


def uninstall_hooks(root: Path | None = None) -> None:
    repo = root or repo_root()
    path = hook_marker_path(repo)
    if path.is_file():
        path.unlink()
    hooks_dir = repo / ".git" / "hooks"
    for name in (GIT_HOOK_NAME, LEGACY_GIT_HOOK_NAME):
        git_hook = hooks_dir / name
        if git_hook.is_file() and (name == LEGACY_GIT_HOOK_NAME or _is_codetriage_git_hook(git_hook)):
            git_hook.unlink()


def parse_hook(hook_name: str, raw: bytes) -> dict[str, Any] | None:
    payload = adapt_raw(raw)
    if payload is None:
        return None

    session_id = str(payload.get("session_id") or "")
    event: dict[str, Any] = {
        "type": _event_type(hook_name),
        "session_id": session_id,
        "timestamp": payload.get("timestamp") or _now(),
    }
    if payload.get("session_ref"):
        event["session_ref"] = payload["session_ref"]
    if payload.get("user_prompt"):
        event["prompt"] = payload["user_prompt"]
    if payload.get("tool_input") is not None:
        event["tool_input"] = payload["tool_input"]

    if hook_name == "commit":
        result = evaluate_commit(payload)
        event["metadata"] = {
            "esi_level": str(result.esi_level),
            "impacted_count": str(result.impacted_count),
            "depth": str(result.depth),
            "blocked": "true" if result.blocked else "false",
            "decision": "block" if result.blocked else "allow",
        }
        if result.blocked:
            event["response_message"] = _rejection_message(result)
        log_esi_run(
            result,
            extra={"session_id": session_id, "hook": "commit"},
        )
    return event


def evaluate_commit(payload: dict[str, Any]) -> BlastRadius:
    files = identify_modified_files(payload)
    return compute_blast_radius(files)


def identify_modified_files(payload: dict[str, Any]) -> list[str]:
    payload = adapt_payload(payload)
    files: list[str] = []
    raw_data = payload.get("raw_data")
    if isinstance(raw_data, dict):
        files.extend(coerce_files(raw_data.get("files")))
        files.extend(coerce_files(raw_data.get("modified_files")))
        files.extend(coerce_files(raw_data.get("changed_files")))

    tool_input = payload.get("tool_input")
    if isinstance(tool_input, dict):
        for key in ("path", "file_path", "file", "filename"):
            if tool_input.get(key):
                files.append(str(tool_input[key]))
        files.extend(coerce_files(tool_input.get("files")))
        files.extend(coerce_files(tool_input.get("paths")))

    for key in ("modified_files", "files", "changed_files"):
        files.extend(coerce_files(payload.get(key)))

    session_ref = payload.get("session_ref")
    if session_ref:
        try:
            from entire_agent_codetriage.sessions import read_session

            session = read_session(payload)
            files.extend(coerce_files(session.get("modified_files")))
            files.extend(coerce_files(session.get("new_files")))
            native = session.get("native_data")
            if native:
                nested = adapt_raw(native if isinstance(native, (bytes, str)) else b"")
                if nested:
                    files.extend(coerce_files(nested.get("modified_files")))
        except Exception:
            pass

    if not files:
        files.extend(_git_changed_files())

    repo = repo_root()
    normalized = []
    seen = set()
    for item in files:
        path = normalize_path(str(item), repo)
        if path and path not in seen:
            seen.add(path)
            normalized.append(path)
    return normalized


def write_hook_response(message: str) -> bytes:
    return (json.dumps({"message": message}, separators=(",", ":")) + "\n").encode("utf-8")


def _event_type(hook_name: str) -> int:
    mapping = {
        "start": EVENT_SESSION_START,
        "session-start": EVENT_SESSION_START,
        "session_start": EVENT_SESSION_START,
        "stop": EVENT_SESSION_END,
        "session-end": EVENT_SESSION_END,
        "session_end": EVENT_SESSION_END,
        "commit": EVENT_TURN_END,
    }
    return mapping.get(hook_name, EVENT_TURN_END)


def _rejection_message(result: BlastRadius) -> str:
    preview = ", ".join(result.impacted[:8])
    if len(result.impacted) > 8:
        preview += ", ..."
    return (
        "CodeTriage blocked this commit: ESI Level 1 (CRITICAL). "
        f"Depth={result.depth} (>= {3} triggers) or impacted_files="
        f"{result.impacted_count} (>= {10} triggers). "
        f"Impacted: {preview}"
    )


def _git_changed_files() -> list[str]:
    root = repo_root()
    files: list[str] = []
    for args in (
        ["git", "-C", str(root), "diff", "--cached", "--name-only", "--diff-filter=ACMR"],
        ["git", "-C", str(root), "diff", "--name-only", "--diff-filter=ACMR"],
    ):
        try:
            proc = subprocess.run(args, check=False, capture_output=True, text=True, timeout=15)
        except (OSError, subprocess.TimeoutExpired):
            continue
        if proc.returncode == 0:
            files.extend(line.strip() for line in proc.stdout.splitlines() if line.strip())
    return files


def _write_git_commit_hook(repo: Path) -> None:
    git_dir = repo / ".git"
    if not git_dir.exists():
        return
    hooks_dir = git_dir / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)
    legacy = hooks_dir / LEGACY_GIT_HOOK_NAME
    if legacy.is_file():
        try:
            legacy.unlink()
        except OSError:
            pass
    hook = hooks_dir / GIT_HOOK_NAME
    hook.write_text(
        "#!/bin/sh\n"
        "# entire-agent-codetriage\n"
        "exec entire-agent-codetriage parse-hook --hook commit\n",
        encoding="utf-8",
    )
    try:
        os.chmod(hook, 0o755)
    except OSError:
        pass


def _is_codetriage_git_hook(path: Path) -> bool:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return False
    return "entire-agent-codetriage" in text and "parse-hook" in text


def _now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
