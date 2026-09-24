"""JSON helpers that match Go's encoding/json []byte (standard base64) convention."""

from __future__ import annotations

import base64
import json
from typing import Any


def encode_bytes(data: bytes | None) -> str | None:
    if data is None:
        return None
    return base64.b64encode(data).decode("ascii")


def decode_bytes(value: Any) -> bytes:
    if value is None:
        return b""
    if isinstance(value, bytes):
        return value
    if isinstance(value, list):
        return bytes(value)
    if isinstance(value, str):
        if value == "":
            return b""
        try:
            return base64.b64decode(value, validate=False)
        except Exception:
            return value.encode("utf-8")
    return json.dumps(value).encode("utf-8")


def dumps(payload: Any) -> str:
    return json.dumps(payload, separators=(",", ":"), ensure_ascii=True)


def loads(data: bytes | str) -> Any:
    if isinstance(data, bytes):
        data = data.decode("utf-8")
    return json.loads(data)
