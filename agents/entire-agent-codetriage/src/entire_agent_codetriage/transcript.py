"""Unified adapter for Entire lifecycle JSON and AcmeCode JSONL transcripts.

Both formats are normalized into one dict the commit gate already understands
(`modified_files`, `session_id`, `tool_input`, …). ESI / checkpoint writers are
not duplicated — they keep consuming that canonical shape.
"""

from __future__ import annotations

import json
from typing import Any, Iterable

FILE_CHANGED_EVENTS = frozenset(
    {
        "file_changed",
        "filechanged",
        "files_changed",
        "file_change",
        "files_changed_event",
    }
)

_META_KEYS = (
    "session_id",
    "session_ref",
    "timestamp",
    "user_prompt",
    "tool_input",
    "hook_type",
    "repo_path",
)

_NESTED_TRANSCRIPT_KEYS = ("transcript", "jsonl", "raw_transcript")


def adapt_raw(raw: bytes | str) -> dict[str, Any] | None:
    """Decode stdin / transcript bytes into a canonical payload dict.

    Empty input returns None (same as the original parse-hook contract).
    Invalid or truncated JSONL yields whatever objects parsed, never raises.
    """
    if raw is None:
        return None
    if isinstance(raw, bytes):
        if not raw.strip():
            return None
        try:
            text = raw.decode("utf-8")
        except UnicodeDecodeError:
            text = raw.decode("utf-8", errors="replace")
    else:
        text = raw
    if not str(text).strip():
        return None

    records = _iter_json_objects(text)
    if not records:
        return None
    if len(records) == 1:
        return adapt_payload(records[0])
    return adapt_payload(_merge_records(records))


def adapt_payload(payload: dict[str, Any]) -> dict[str, Any]:
    """Lift AcmeCode `file_changed` events into `modified_files` without dropping Entire fields."""
    if not isinstance(payload, dict):
        return {}
    out = dict(payload)
    extra_files: list[str] = []

    if _is_file_changed(out):
        extra_files.extend(_files_from_event(out))

    events = out.get("events")
    if isinstance(events, list):
        extra_files.extend(_files_from_records(events))
        _fill_meta(out, events)

    for key in _NESTED_TRANSCRIPT_KEYS:
        extra_files.extend(_files_from_nested_blob(out.get(key), out))

    raw_data = out.get("raw_data")
    if isinstance(raw_data, (str, bytes)):
        extra_files.extend(_files_from_nested_blob(raw_data, out))
    elif isinstance(raw_data, dict):
        extra_files.extend(_files_from_records([raw_data]))
        extra_files.extend(coerce_files(raw_data.get("modified_files")))
        _fill_meta(out, [raw_data])
    elif isinstance(raw_data, list):
        extra_files.extend(_files_from_records(raw_data))
        _fill_meta(out, raw_data)

    native = out.get("native_data")
    if isinstance(native, (str, bytes)):
        extra_files.extend(_files_from_nested_blob(native, out))

    if extra_files:
        out["modified_files"] = _dedupe(coerce_files(out.get("modified_files")) + extra_files)
    return out


def coerce_files(value: Any) -> list[str]:
    if not value:
        return []
    if isinstance(value, (str, bytes)):
        text = value.decode("utf-8", errors="replace") if isinstance(value, bytes) else value
        text = text.strip()
        return [text] if text else []
    if isinstance(value, list):
        files: list[str] = []
        for item in value:
            if isinstance(item, dict):
                files.extend(_files_from_event(item))
            elif item:
                files.append(str(item))
        return files
    return []


def _iter_json_objects(text: str) -> list[dict[str, Any]]:
    stripped = text.strip()
    if not stripped:
        return []
    try:
        parsed = json_loads(stripped)
    except ValueError:
        parsed = None
    if isinstance(parsed, dict):
        return [parsed]
    if isinstance(parsed, list):
        return [item for item in parsed if isinstance(item, dict)]
    return list(_iter_jsonl_lines(text))


def _iter_jsonl_lines(text: str) -> Iterable[dict[str, Any]]:
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            parsed = json_loads(line)
        except ValueError:
            continue
        if isinstance(parsed, dict):
            yield parsed


def json_loads(text: str) -> Any:
    return json.loads(text)


def _merge_records(records: list[dict[str, Any]]) -> dict[str, Any]:
    merged: dict[str, Any] = {}
    files: list[str] = []
    for event in records:
        if not isinstance(event, dict):
            continue
        _fill_meta(merged, [event])
        files.extend(coerce_files(event.get("modified_files")))
        files.extend(coerce_files(event.get("files")))
        files.extend(coerce_files(event.get("changed_files")))
        files.extend(coerce_files(event.get("new_files")))
        if _is_file_changed(event):
            files.extend(_files_from_event(event))
        for key in _NESTED_TRANSCRIPT_KEYS:
            files.extend(_files_from_nested_blob(event.get(key), merged))
    if files:
        merged["modified_files"] = _dedupe(files)
    return merged


def _files_from_nested_blob(blob: Any, meta_target: dict[str, Any]) -> list[str]:
    if not isinstance(blob, (str, bytes)):
        return []
    nested = adapt_raw(blob)
    if not nested:
        return []
    _fill_meta(meta_target, [nested])
    return coerce_files(nested.get("modified_files"))


def _files_from_records(records: Iterable[Any]) -> list[str]:
    files: list[str] = []
    for event in records:
        if not isinstance(event, dict):
            continue
        files.extend(coerce_files(event.get("modified_files")))
        files.extend(coerce_files(event.get("files")))
        files.extend(coerce_files(event.get("changed_files")))
        if _is_file_changed(event):
            files.extend(_files_from_event(event))
    return files


def _fill_meta(target: dict[str, Any], events: Iterable[Any]) -> None:
    for event in events:
        if not isinstance(event, dict):
            continue
        for key in _META_KEYS:
            if target.get(key) in (None, "") and event.get(key) not in (None, ""):
                target[key] = event[key]


def _is_file_changed(event: dict[str, Any]) -> bool:
    return _event_kind(event) in FILE_CHANGED_EVENTS


def _event_kind(event: dict[str, Any]) -> str:
    for key in ("event", "type", "event_type", "name"):
        value = event.get(key)
        if value is None or isinstance(value, (dict, list)):
            continue
        kind = str(value).strip().lower().replace("-", "_").replace(" ", "_")
        if kind:
            return kind
    return ""


def _files_from_event(event: dict[str, Any]) -> list[str]:
    files: list[str] = []
    blobs: list[dict[str, Any]] = [event]
    for key in ("data", "payload", "file_changed"):
        nested = event.get(key)
        if isinstance(nested, dict):
            blobs.append(nested)
    for blob in blobs:
        for key in ("path", "file_path", "file", "filename", "filepath"):
            value = blob.get(key)
            if isinstance(value, dict):
                files.extend(_files_from_event(value))
            else:
                files.extend(coerce_files(value))
        files.extend(coerce_files(blob.get("files")))
        files.extend(coerce_files(blob.get("paths")))
        files.extend(coerce_files(blob.get("modified_files")))
        files.extend(coerce_files(blob.get("changed_files")))
    return files


def _dedupe(files: list[str]) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for item in files:
        path = str(item).strip()
        if path and path not in seen:
            seen.add(path)
            out.append(path)
    return out
