"""Entire observer for Hermes.

entire-agent-hermes observer v1

The observer intentionally uses a field allowlist. It never serializes hook
kwargs wholesale and never reads conversation history, memory, platform IDs,
environment variables, tool arguments, or tool results.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, Iterable, Optional, Tuple

try:
    from agent.redact import redact_sensitive_text as _hermes_redact_sensitive_text  # type: ignore[import-not-found]
except Exception:  # Standalone verification and older Hermes installations.
    _hermes_redact_sensitive_text = None


_PLUGIN_NAME = "entire-observer"
_MAX_TEXT = 32768
_MAX_FILES = 2048
_LOCK = threading.RLock()
_SESSIONS: Dict[str, Dict[str, Any]] = {}

_WORKDIR_FIELDS = {"cwd", "workdir"}
_PATH_FIELDS = {
    "path",
    "paths",
    "file_path",
    "file_paths",
    "filepath",
    "filepaths",
    "filename",
    "filenames",
    "directory",
    "directories",
    "dir",
    "dirs",
    "folder",
    "folders",
}

_SECRET_PATTERNS = (
    re.compile(r"-----BEGIN [^-\r\n]+ PRIVATE KEY-----.*?-----END [^-\r\n]+ PRIVATE KEY-----", re.I | re.S),
    re.compile(r"(?i)\b(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}"),
    re.compile(r"(?i)\b(?:password|passwd|pwd|secret|client_secret|api[_-]?key|access[_-]?token|refresh[_-]?token|authorization)\b\s*[:=]\s*(?:\"[^\"\r\n]*\"|'[^'\r\n]*'|[^\s,;]+)"),
    re.compile(r"\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{12,}\b"),
    re.compile(r"\b(?:sk|pk)-(?:proj-)?[A-Za-z0-9_-]{16,}\b"),
    re.compile(r"\b(?:gh[pousr]|github_pat)_[A-Za-z0-9_]{16,}\b", re.I),
    re.compile(r"\b(?:xox[baprs]-[A-Za-z0-9-]{10,})\b", re.I),
    re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
    re.compile(r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b"),
    re.compile(r"(?i)(?:https?|ssh)://[^\s/@:]+:[^\s/@]+@"),
)

_SENSITIVE_PATH = re.compile(
    r"(?i)(?:^|/)(?:\.env(?:\..*)?|\.npmrc|\.pypirc|credentials(?:\..*)?|"
    r"id_(?:rsa|dsa|ecdsa|ed25519)(?:\.pub)?|[^/]*(?:secret|token|private[_-]?key)[^/]*)$"
)


def _now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def _safe_model(value: Any) -> str:
    text = str(value or "")[:256]
    return re.sub(r"[^A-Za-z0-9._:/+-]+", "_", text)


def _sanitize_text(value: Any) -> str:
    if not isinstance(value, str):
        return ""
    text = "".join(ch if ch in "\n\t" or ord(ch) >= 32 else " " for ch in value)
    if _hermes_redact_sensitive_text is not None:
        try:
            text = _hermes_redact_sensitive_text(
                text,
                force=True,
                redact_url_credentials=True,
            )
        except Exception:
            # Observer failures must never interrupt Hermes; local patterns
            # below remain the independent fail-open privacy boundary.
            pass
    text = re.sub(r"«redacted:[^»]*»", "[REDACTED]", text)
    for pattern in _SECRET_PATTERNS:
        text = pattern.sub("[REDACTED]", text)
    if len(text) > _MAX_TEXT:
        text = text[:_MAX_TEXT] + "\n[TRUNCATED]"
    return text


def _hermes_home() -> Optional[Path]:
    value = os.environ.get("HERMES_HOME", "").strip()
    if not value:
        return None
    try:
        return Path(value).expanduser().resolve()
    except Exception:
        return None


def _load_registry() -> Tuple[Optional[Path], list[Dict[str, str]]]:
    home = _hermes_home()
    if home is None:
        return None, []
    path = home / "plugins" / _PLUGIN_NAME / "repositories.json"
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
        if raw.get("version") != 1 or not isinstance(raw.get("repositories"), list):
            return home, []
        repos = []
        for item in raw["repositories"]:
            if not isinstance(item, dict):
                continue
            repo = item.get("path")
            entire_bin = item.get("entire_bin")
            if isinstance(repo, str) and isinstance(entire_bin, str) and repo and entire_bin:
                repos.append({"path": repo, "entire_bin": entire_bin})
        return home, repos
    except Exception:
        return home, []


def _canonical_path(value: str, base: Optional[Path] = None) -> Optional[Path]:
    if not isinstance(value, str) or not value.strip() or "\x00" in value:
        return None
    try:
        candidate = Path(value).expanduser()
        # Reject explicit traversal rather than normalizing it into a seemingly
        # safe path. This also makes malformed tool targets fail closed while
        # the observer itself remains fail-open.
        if ".." in candidate.parts:
            return None
        if not candidate.is_absolute():
            if base is None:
                base = Path.cwd().resolve()
            candidate = base / candidate
        return candidate.resolve()
    except Exception:
        return None


def _registered_repositories() -> Tuple[Optional[Path], list[Tuple[Path, str]]]:
    home, registrations = _load_registry()
    if home is None:
        return None, []
    repositories: Dict[str, Tuple[Path, str]] = {}
    for item in registrations:
        try:
            repo = Path(item["path"]).resolve()
            repositories[str(repo)] = (repo, item["entire_bin"])
        except Exception:
            continue
    return home, list(repositories.values())


def _is_path_field(key: Any) -> bool:
    if not isinstance(key, str):
        return False
    key = key.lower()
    return key in _PATH_FIELDS or key.endswith(
        ("_path", "_paths", "_file", "_files", "_dir", "_dirs", "_directory", "_directories", "_folder", "_folders")
    )


def _string_values(value: Any) -> Iterable[str]:
    if isinstance(value, str):
        yield value
    elif isinstance(value, (list, tuple)):
        for item in value[:256]:
            if isinstance(item, str):
                yield item


def _path_fields(args: Dict[str, Any]) -> Tuple[list[str], list[str], bool, bool]:
    workdirs: list[str] = []
    paths: list[str] = []
    workdir_seen = False
    path_seen = False
    # Hermes tool schemas expose filesystem targets at the top level. Do not
    # recurse into content/data/payload objects: those are user data and may
    # contain path-like keys unrelated to the tool's actual target.
    for key, value in list(args.items())[:256]:
        normalized = key.lower() if isinstance(key, str) else ""
        if normalized in _WORKDIR_FIELDS:
            workdir_seen = True
            workdirs.extend(_string_values(value))
        elif _is_path_field(key):
            path_seen = True
            paths.extend(_string_values(value))
    return workdirs[:256], paths[:256], workdir_seen, path_seen


def _relative_to_repository(candidate: Path, repo: Path) -> Optional[Path]:
    try:
        return candidate.relative_to(repo)
    except Exception:
        return None


def _match_repository(
    home: Path,
    repositories: list[Tuple[Path, str]],
    candidate: Optional[Path],
    path_evidence: bool,
) -> Optional[Tuple[Path, Path, str]]:
    if candidate is None:
        return None
    matches = []
    for repo, entire_bin in repositories:
        relative = _relative_to_repository(candidate, repo)
        if relative is None:
            continue
        if path_evidence:
            safe = _safe_relative_path(repo, str(candidate))
            if safe is None:
                continue
        elif any(part in {".git", ".entire"} for part in relative.parts):
            continue
        matches.append((repo, entire_bin))
    if not matches:
        return None
    repo, entire_bin = max(matches, key=lambda value: len(value[0].parts))
    return home, repo, entire_bin


def _repositories(args: Any = None) -> list[Tuple[Path, Path, str]]:
    home, registrations = _registered_repositories()
    if home is None or not registrations:
        return []

    workdir_values: list[str] = []
    path_values: list[str] = []
    workdir_seen = False
    path_seen = False
    if isinstance(args, dict):
        workdir_values, path_values, workdir_seen, path_seen = _path_fields(args)

    try:
        process_cwd = Path.cwd().resolve()
    except Exception:
        process_cwd = None
    workdirs = [_canonical_path(value, process_cwd) for value in workdir_values]
    workdirs = [value for value in workdirs if value is not None]

    matches: Dict[str, Tuple[Path, Path, str]] = {}
    if path_seen:
        bases: list[Optional[Path]] = workdirs or [process_cwd]
        for value in path_values:
            raw = Path(value).expanduser() if isinstance(value, str) else None
            candidates = [_canonical_path(value)] if raw is not None and raw.is_absolute() else [_canonical_path(value, base) for base in bases]
            for candidate in candidates:
                match = _match_repository(home, registrations, candidate, True)
                if match is not None:
                    matches[str(match[1])] = match
        # An explicit path is authoritative. Invalid, sensitive, or escaping
        # paths must not silently fall back to a workdir/CWD registration.
        return list(matches.values())

    if workdir_seen:
        for candidate in workdirs:
            match = _match_repository(home, registrations, candidate, False)
            if match is not None:
                matches[str(match[1])] = match
        return list(matches.values())

    # Repository attribution requires an explicit allowlisted path, workdir, or
    # cwd in the current tool payload. Process CWD is not trustworthy for
    # long-running gateways and must never select a projection by itself.
    return []


def _session_file(home: Path, repo: Path, session_id: Any) -> Path:
    repo_digest = hashlib.sha256(str(repo).encode("utf-8")).hexdigest()[:16]
    session_digest = hashlib.sha256(str(session_id or "").encode("utf-8")).hexdigest()
    return home / "entire" / "transcripts" / repo_digest / f"{session_digest}.jsonl"


def _safe_relative_path(repo: Path, value: str) -> Optional[str]:
    if not isinstance(value, str) or not value or "\x00" in value:
        return None
    try:
        candidate = Path(value)
        if candidate.is_absolute():
            candidate = candidate.resolve().relative_to(repo)
        normalized = Path(os.path.normpath(str(candidate)))
        if normalized.is_absolute() or ".." in normalized.parts:
            return None
        text = normalized.as_posix()
    except Exception:
        return None
    if text in {"", "."} or text.startswith((".git/", ".entire/")):
        return None
    if _SENSITIVE_PATH.search(text):
        return None
    return text[:4096]


def _modified_files(repo: Path) -> list[str]:
    try:
        completed = subprocess.run(
            ["git", "status", "--porcelain=v1", "-z", "--untracked-files=all"],
            cwd=str(repo),
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=2,
            check=False,
        )
        if completed.returncode != 0 or len(completed.stdout) > 4 * 1024 * 1024:
            return []
        fields = completed.stdout.decode("utf-8", errors="replace").split("\x00")
        files = set()
        index = 0
        while index < len(fields):
            field = fields[index]
            index += 1
            if len(field) < 4:
                continue
            status = field[:2]
            path = field[3:]
            if "R" in status or "C" in status:
                # In porcelain v1 -z, field[3:] is the destination and the
                # following NUL-delimited field is the source. Consume the
                # source record but keep the destination as modified evidence.
                if index < len(fields):
                    index += 1
            safe = _safe_relative_path(repo, path)
            if safe:
                files.add(safe)
            if len(files) >= _MAX_FILES:
                break
        return sorted(files)
    except Exception:
        return []


def _append(repo_info: Tuple[Path, Path, str], session_id: Any, entry: Dict[str, Any]) -> None:
    home, repo, _ = repo_info
    path = _session_file(home, repo, session_id)
    safe_entry = {"v": 1, "type": entry.get("type", "unknown"), "timestamp": _now()}
    for key in ("content", "model", "name", "status"):
        value = entry.get(key)
        if isinstance(value, str) and value:
            safe_entry[key] = value
    files = entry.get("modified_files")
    if isinstance(files, list) and files:
        safe_entry["modified_files"] = files[:_MAX_FILES]
    data = (json.dumps(safe_entry, ensure_ascii=False, separators=(",", ":")) + "\n").encode("utf-8")
    try:
        with _LOCK:
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
            try:
                os.write(fd, data)
            finally:
                os.close(fd)
    except Exception:
        return


def _forward(repo_info: Tuple[Path, Path, str], hook: str, session_id: Any, **fields: str) -> None:
    home, repo, entire_bin = repo_info
    payload = {
        "session_id": str(session_id or ""),
        "session_ref": str(_session_file(home, repo, session_id)),
        "timestamp": _now(),
    }
    payload.update({key: value for key, value in fields.items() if value})
    try:
        subprocess.run(
            [entire_bin, "hooks", "hermes", hook],
            cwd=str(repo),
            input=json.dumps(payload, separators=(",", ":")).encode("utf-8"),
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=15,
            check=False,
        )
    except Exception:
        return


def _session_key(session_id: Any) -> Optional[str]:
    if not isinstance(session_id, str) or not session_id:
        return None
    return session_id


def _session_state(session_id: Any) -> Optional[Dict[str, Any]]:
    key = _session_key(session_id)
    if key is None:
        return None
    state = _SESSIONS.get(key)
    if state is None:
        state = {
            "prompt": "",
            "model": "",
            "turn": 0,
            "turn_repos": set(),
            "last_turn_repos": set(),
            "tool_seen": False,
            "projections": {},
        }
        _SESSIONS[key] = state
    return state


def _ensure_projection(
    repo_info: Tuple[Path, Path, str],
    session_id: str,
    state: Dict[str, Any],
    *,
    start_turn: bool,
) -> None:
    repo_key = str(repo_info[1])
    projections = state["projections"]
    if repo_key not in projections:
        projections[repo_key] = repo_info
        _append(repo_info, session_id, {"type": "session_start", "model": state["model"]})
        # Forwarding is synchronous. Entire creates the active session from
        # this event; recursively running `entire enable` here would reinstall
        # this plugin during its own callback.
        _forward(repo_info, "on_session_start", session_id, model=state["model"])

    turn_repos = state["turn_repos"]
    if start_turn and repo_key not in turn_repos:
        if state["prompt"]:
            _append(
                repo_info,
                session_id,
                {"type": "user", "content": state["prompt"], "model": state["model"]},
            )
        _forward(
            repo_info,
            "pre_llm_call",
            session_id,
            user_prompt=state["prompt"],
            model=state["model"],
        )
        turn_repos.add(repo_key)


def _on_session_start(**kwargs: Any) -> None:
    try:
        session_id = _session_key(kwargs.get("session_id"))
        if session_id is None:
            return
        with _LOCK:
            state = _session_state(session_id)
            if state is None:
                return
            model = _safe_model(kwargs.get("model"))
            if model:
                state["model"] = model
            # Do not project from process CWD alone. Gateway/CLI startup CWD
            # can differ from the repository a tool actually targets. The
            # first repository-scoped pre_tool_call starts the projection.
    except Exception:
        return


def _on_pre_llm_call(**kwargs: Any) -> None:
    try:
        session_id = _session_key(kwargs.get("session_id"))
        if session_id is None:
            return
        with _LOCK:
            state = _session_state(session_id)
            if state is None:
                return
            prompt = _sanitize_text(kwargs.get("user_message"))
            model = _safe_model(kwargs.get("model"))
            state["prompt"] = prompt
            if model:
                state["model"] = model
            state["turn"] += 1
            state["turn_repos"] = set()
            state["tool_seen"] = False
            # Buffer only. Repository selection is deferred until a tool's
            # allowlisted top-level path/workdir fields establish a match.
    except Exception:
        return


def _on_pre_tool_call(**kwargs: Any) -> None:
    try:
        session_id = _session_key(kwargs.get("session_id"))
        if session_id is None:
            return
        with _LOCK:
            state = _session_state(session_id)
            if state is None:
                return
            state["tool_seen"] = True
            for info in _repositories(kwargs.get("args")):
                _ensure_projection(
                    info,
                    session_id,
                    state,
                    start_turn=True,
                )
    except Exception:
        return


def _on_post_tool_call(**kwargs: Any) -> None:
    try:
        session_id = _session_key(kwargs.get("session_id"))
        if session_id is None:
            return
        status = str(kwargs.get("status") or "ok").lower()
        if status not in {"ok", "error", "blocked"}:
            status = "error" if kwargs.get("error_type") else "ok"
        tool_name = re.sub(r"[^A-Za-z0-9_.-]+", "_", str(kwargs.get("tool_name") or "unknown"))[:128]
        with _LOCK:
            state = _session_state(session_id)
            if state is None:
                return
            state["tool_seen"] = True
            for info in _repositories(kwargs.get("args")):
                _ensure_projection(info, session_id, state, start_turn=True)
                _append(
                    info,
                    session_id,
                    {"type": "tool", "name": tool_name, "status": status, "modified_files": _modified_files(info[1])},
                )
    except Exception:
        return


def _on_post_llm_call(**kwargs: Any) -> None:
    try:
        session_id = _session_key(kwargs.get("session_id"))
        if session_id is None:
            return
        response = _sanitize_text(kwargs.get("assistant_response"))
        model = _safe_model(kwargs.get("model"))
        with _LOCK:
            state = _session_state(session_id)
            if state is None:
                return
            if model:
                state["model"] = model
            if not state["turn_repos"] and not state["tool_seen"]:
                # A genuinely tool-free follow-up remains attributable only to the
                # repositories established by the immediately preceding turn.
                # Do this at post_llm time so a tool targeting a new repository
                # can select that repository without leaking the prompt to the
                # old projection.
                for repo_key in list(state["last_turn_repos"]):
                    info = state["projections"].get(repo_key)
                    if info is not None:
                        _ensure_projection(info, session_id, state, start_turn=True)
            for repo_key in list(state["turn_repos"]):
                info = state["projections"].get(repo_key)
                if info is None:
                    continue
                if response:
                    _append(
                        info,
                        session_id,
                        {"type": "assistant", "content": response, "model": state["model"]},
                    )
                _forward(
                    info,
                    "post_llm_call",
                    session_id,
                    assistant_response=response,
                    model=state["model"],
                )
            if state["turn_repos"]:
                state["last_turn_repos"] = set(state["turn_repos"])
            elif state["tool_seen"]:
                # An unattributed tool turn breaks repository affinity. A later
                # tool-free turn must not inherit a projection across it.
                state["last_turn_repos"] = set()
    except Exception:
        return


def _on_session_end(**kwargs: Any) -> None:
    try:
        session_id = _session_key(kwargs.get("session_id"))
        if session_id is None:
            return
        status = "interrupted" if kwargs.get("interrupted") else "error" if kwargs.get("failed") else "ok"
        with _LOCK:
            state = _session_state(session_id)
            if state is None:
                return
            for repo_key in list(state["turn_repos"]):
                info = state["projections"].get(repo_key)
                if info is None:
                    continue
                _append(info, session_id, {"type": "turn_end", "status": status})
                _forward(info, "on_session_end", session_id)
            state["turn_repos"] = set()
    except Exception:
        return


def _on_session_finalize(**kwargs: Any) -> None:
    try:
        session_id = _session_key(kwargs.get("session_id"))
        if session_id is None:
            return
        with _LOCK:
            state = _SESSIONS.pop(session_id, None)
            if state is None:
                return
            for info in list(state["projections"].values()):
                _append(info, session_id, {"type": "session_end"})
                _forward(info, "on_session_finalize", session_id)
    except Exception:
        return


def register(ctx: Any) -> None:
    """Register observer hooks; callbacks are fail-open and return no directives."""
    ctx.register_hook("on_session_start", _on_session_start)
    ctx.register_hook("pre_llm_call", _on_pre_llm_call)
    ctx.register_hook("pre_tool_call", _on_pre_tool_call)
    ctx.register_hook("post_tool_call", _on_post_tool_call)
    ctx.register_hook("post_llm_call", _on_post_llm_call)
    ctx.register_hook("on_session_end", _on_session_end)
    ctx.register_hook("on_session_finalize", _on_session_finalize)
