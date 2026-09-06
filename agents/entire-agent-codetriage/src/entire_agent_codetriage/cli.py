"""Entire external-agent protocol CLI."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path
from typing import Any, Sequence

from entire_agent_codetriage import AGENT_NAME, AGENT_TYPE, HOOK_NAMES
from entire_agent_codetriage.env import load_dotenv_files, repo_root
from entire_agent_codetriage.hooks import (
    are_hooks_installed,
    install_hooks,
    parse_hook,
    uninstall_hooks,
    write_hook_response,
)
from entire_agent_codetriage.jsonutil import dumps, encode_bytes, loads
from entire_agent_codetriage.sessions import (
    chunk_transcript,
    read_session,
    reassemble_transcript,
    session_dir,
    session_file,
    session_to_protocol,
    write_session,
)


def main(argv: Sequence[str] | None = None) -> int:
    load_dotenv_files()
    argv = list(sys.argv[1:] if argv is None else argv)
    if not argv:
        _err("missing subcommand")
        return 1

    command = argv[0]
    handlers = {
        "info": _cmd_info,
        "detect": _cmd_detect,
        "get-session-id": _cmd_get_session_id,
        "get-session-dir": _cmd_get_session_dir,
        "resolve-session-file": _cmd_resolve_session_file,
        "read-session": _cmd_read_session,
        "write-session": _cmd_write_session,
        "read-transcript": _cmd_read_transcript,
        "chunk-transcript": _cmd_chunk_transcript,
        "reassemble-transcript": _cmd_reassemble_transcript,
        "format-resume-command": _cmd_format_resume,
        "parse-hook": _cmd_parse_hook,
        "install-hooks": _cmd_install_hooks,
        "uninstall-hooks": _cmd_uninstall_hooks,
        "are-hooks-installed": _cmd_are_hooks_installed,
        "write-hook-response": _cmd_write_hook_response,
    }
    handler = handlers.get(command)
    if handler is None:
        _err(f"unknown subcommand: {command}")
        return 1
    try:
        return handler(argv[1:])
    except ProtocolError as exc:
        _err(str(exc))
        return 1
    except Exception as exc:  # noqa: BLE001 — protocol binaries must not traceback on stdout
        _err(str(exc))
        return 1


class ProtocolError(Exception):
    pass


def _cmd_info(_args: list[str]) -> int:
    _out(
        {
            "protocol_version": 1,
            "name": AGENT_NAME,
            "type": AGENT_TYPE,
            "description": (
                "CodeTriage pre-commit ESI blast-radius gatekeeper "
                "using Entire Graph reverse dependencies"
            ),
            "is_preview": True,
            "protected_dirs": [".codetriage", ".entire"],
            "hook_names": list(HOOK_NAMES),
            "capabilities": {
                "hooks": True,
                "transcript_analyzer": False,
                "transcript_preparer": False,
                "token_calculator": False,
                "text_generator": False,
                "hook_response_writer": True,
                "subagent_aware_extractor": False,
            },
        }
    )
    return 0


def _cmd_detect(_args: list[str]) -> int:
    root = repo_root()
    present = (root / ".git").exists() or (root / ".entire").exists() or True
    _out({"present": bool(present)})
    return 0


def _cmd_get_session_id(_args: list[str]) -> int:
    payload = _stdin_json()
    _out({"session_id": str(payload.get("session_id") or "")})
    return 0


def _cmd_get_session_dir(args: list[str]) -> int:
    ns = _parse_flags(args, repo_path=None)
    path = session_dir(ns.repo_path)
    _out({"session_dir": str(path)})
    return 0


def _cmd_resolve_session_file(args: list[str]) -> int:
    ns = _parse_flags(args, session_dir=None, session_id=None)
    if not ns.session_dir or not ns.session_id:
        raise ProtocolError("resolve-session-file requires --session-dir and --session-id")
    path = session_file(ns.session_dir, ns.session_id)
    _out({"session_file": str(path)})
    return 0


def _cmd_read_session(_args: list[str]) -> int:
    payload = _stdin_json()
    session = session_to_protocol(read_session(payload))
    _out(session)
    return 0


def _cmd_write_session(_args: list[str]) -> int:
    payload = _stdin_json()
    write_session(payload)
    return 0


def _cmd_read_transcript(args: list[str]) -> int:
    ns = _parse_flags(args, session_ref=None)
    if not ns.session_ref:
        raise ProtocolError("read-transcript requires --session-ref")
    path = Path(ns.session_ref)
    if not path.is_file():
        raise ProtocolError(f"transcript not found: {path}")
    sys.stdout.buffer.write(path.read_bytes())
    return 0


def _cmd_chunk_transcript(args: list[str]) -> int:
    ns = _parse_flags(args, max_size=None)
    if ns.max_size is None:
        raise ProtocolError("chunk-transcript requires --max-size")
    try:
        max_size = int(ns.max_size)
    except ValueError as exc:
        raise ProtocolError("invalid --max-size") from exc
    data = sys.stdin.buffer.read()
    chunks = chunk_transcript(data, max_size)
    _out({"chunks": [encode_bytes(chunk) for chunk in chunks]})
    return 0


def _cmd_reassemble_transcript(_args: list[str]) -> int:
    payload = _stdin_json()
    chunks = payload.get("chunks")
    if not isinstance(chunks, list):
        raise ProtocolError("reassemble-transcript requires a chunks array")
    sys.stdout.buffer.write(reassemble_transcript(chunks))
    return 0


def _cmd_format_resume(args: list[str]) -> int:
    ns = _parse_flags(args, session_id=None)
    session_id = ns.session_id or ""
    _out({"command": f"entire-agent-codetriage --resume {session_id}"})
    return 0


def _cmd_parse_hook(args: list[str]) -> int:
    ns = _parse_flags(args, hook=None)
    if not ns.hook:
        raise ProtocolError("parse-hook requires --hook")
    raw = sys.stdin.buffer.read()
    event = parse_hook(ns.hook, raw)
    if event is None:
        sys.stdout.write("null\n")
        return 0
    _out(event)
    metadata = event.get("metadata") or {}
    if metadata.get("blocked") == "true":
        _err(event.get("response_message") or "commit blocked by CodeTriage ESI Level 1")
        return 1
    return 0


def _cmd_install_hooks(args: list[str]) -> int:
    ns = _parse_flags(args, force=False, local_dev=False)
    count = install_hooks(force=bool(ns.force), local_dev=bool(ns.local_dev))
    _out({"hooks_installed": count})
    return 0


def _cmd_uninstall_hooks(_args: list[str]) -> int:
    uninstall_hooks()
    return 0


def _cmd_are_hooks_installed(_args: list[str]) -> int:
    _out({"installed": are_hooks_installed()})
    return 0


def _cmd_write_hook_response(args: list[str]) -> int:
    ns = _parse_flags(args, message=None)
    if ns.message is None:
        raise ProtocolError("write-hook-response requires --message")
    sys.stdout.buffer.write(write_hook_response(ns.message))
    return 0


def _parse_flags(args: list[str], **defaults: Any) -> argparse.Namespace:
    parser = argparse.ArgumentParser(add_help=False)
    for name, value in defaults.items():
        flag = f"--{name.replace('_', '-')}"
        if isinstance(value, bool):
            parser.add_argument(flag, action="store_true")
        else:
            parser.add_argument(flag, default=value)
    parsed, _unknown = parser.parse_known_args(args)
    return parsed


def _stdin_json() -> dict[str, Any]:
    raw = sys.stdin.buffer.read()
    if not raw.strip():
        raise ProtocolError("expected JSON on stdin")
    try:
        payload = loads(raw)
    except Exception as exc:
        raise ProtocolError("invalid JSON on stdin") from exc
    if not isinstance(payload, dict):
        raise ProtocolError("expected a JSON object on stdin")
    return payload


def _out(payload: Any) -> None:
    sys.stdout.write(dumps(payload) + "\n")


def _err(message: str) -> None:
    sys.stderr.write(message + "\n")
