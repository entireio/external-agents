"""Session directory, file, and transcript helpers."""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
from typing import Any

from entire_agent_codetriage import AGENT_NAME
from entire_agent_codetriage.env import repo_root
from entire_agent_codetriage.jsonutil import decode_bytes, encode_bytes


def session_dir(repo_path: str | None = None) -> Path:
    root = Path(repo_path).resolve() if repo_path else repo_root()
    digest = hashlib.sha256(str(root).encode("utf-8")).hexdigest()[:12]
    tmp = os.environ.get("TMPDIR") or os.environ.get("TEMP") or os.environ.get("TMP")
    base = Path(tmp) if tmp else Path(os.path.expanduser("~")) / ".cache"
    path = base / "entire-codetriage" / digest
    path.mkdir(parents=True, exist_ok=True)
    marker = root / ".entire" / "tmp"
    marker.mkdir(parents=True, exist_ok=True)
    return path


def session_file(session_directory: str | Path, session_id: str) -> Path:
    safe = session_id.replace("/", "_").replace("\\", "_") or "unknown"
    return Path(session_directory) / f"{safe}.json"


def write_session(payload: dict[str, Any]) -> Path:
    session_id = str(payload.get("session_id") or "unknown")
    repo_path = str(payload.get("repo_path") or repo_root())
    ref = payload.get("session_ref")
    if ref:
        path = Path(str(ref))
    else:
        path = session_file(session_dir(repo_path), session_id)
    path.parent.mkdir(parents=True, exist_ok=True)

    native = payload.get("native_data")
    stored = {
        "session_id": session_id,
        "agent_name": payload.get("agent_name") or AGENT_NAME,
        "repo_path": repo_path,
        "session_ref": str(path),
        "start_time": payload.get("start_time") or "",
        "native_data": encode_bytes(decode_bytes(native)) if native not in (None, "") else None,
        "modified_files": list(payload.get("modified_files") or []),
        "new_files": list(payload.get("new_files") or []),
        "deleted_files": list(payload.get("deleted_files") or []),
    }
    path.write_text(json.dumps(stored, indent=2), encoding="utf-8")
    return path


def read_session(hook_input: dict[str, Any]) -> dict[str, Any]:
    session_id = str(hook_input.get("session_id") or "")
    session_ref = hook_input.get("session_ref")
    root = str(hook_input.get("repo_path") or repo_root())
    path = Path(str(session_ref)) if session_ref else session_file(session_dir(root), session_id)

    if path.is_file():
        raw = json.loads(path.read_text(encoding="utf-8"))
        native = raw.get("native_data")
        return {
            "session_id": raw.get("session_id") or session_id,
            "agent_name": raw.get("agent_name") or AGENT_NAME,
            "repo_path": raw.get("repo_path") or root,
            "session_ref": raw.get("session_ref") or str(path),
            "start_time": raw.get("start_time") or "",
            "native_data": decode_bytes(native) if native not in (None, "") else b"",
            "modified_files": list(raw.get("modified_files") or []),
            "new_files": list(raw.get("new_files") or []),
            "deleted_files": list(raw.get("deleted_files") or []),
        }

    return {
        "session_id": session_id,
        "agent_name": AGENT_NAME,
        "repo_path": root,
        "session_ref": str(path),
        "start_time": hook_input.get("timestamp") or "",
        "native_data": b"",
        "modified_files": [],
        "new_files": [],
        "deleted_files": [],
    }


def session_to_protocol(session: dict[str, Any]) -> dict[str, Any]:
    native = session.get("native_data")
    return {
        "session_id": session.get("session_id") or "",
        "agent_name": session.get("agent_name") or AGENT_NAME,
        "repo_path": session.get("repo_path") or str(repo_root()),
        "session_ref": session.get("session_ref") or "",
        "start_time": session.get("start_time") or "",
        "native_data": encode_bytes(decode_bytes(native)) if native not in (None, "") else None,
        "modified_files": list(session.get("modified_files") or []),
        "new_files": list(session.get("new_files") or []),
        "deleted_files": list(session.get("deleted_files") or []),
    }


def chunk_transcript(data: bytes, max_size: int) -> list[bytes]:
    if max_size <= 0:
        raise ValueError("max-size must be > 0")
    if not data:
        return [b""]
    return [data[i : i + max_size] for i in range(0, len(data), max_size)]


def reassemble_transcript(chunks: list[Any]) -> bytes:
    return b"".join(decode_bytes(chunk) for chunk in chunks)
