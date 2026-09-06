"""Environment and .env loading for protocol + Databricks MLflow."""

from __future__ import annotations

import os
from pathlib import Path


def repo_root() -> Path:
    raw = os.environ.get("ENTIRE_REPO_ROOT") or os.getcwd()
    return Path(raw).resolve()


def load_dotenv_files() -> None:
    try:
        from dotenv import load_dotenv
    except ImportError:
        load_dotenv = None  # type: ignore[assignment]

    candidates = [
        repo_root() / ".env",
        Path(__file__).resolve().parents[2] / ".env",
        Path.cwd() / ".env",
    ]
    seen: set[Path] = set()
    for path in candidates:
        resolved = path.resolve()
        if resolved in seen or not resolved.is_file():
            continue
        seen.add(resolved)
        if load_dotenv is not None:
            load_dotenv(resolved, override=False)
        else:
            _parse_dotenv(resolved)


def _parse_dotenv(path: Path) -> None:
    for line in path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, value = stripped.split("=", 1)
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        os.environ.setdefault(key, value)
