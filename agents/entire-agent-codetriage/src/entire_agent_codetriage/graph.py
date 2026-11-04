"""Entire Graph reverse-dependency loader."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
from collections import defaultdict
from pathlib import Path
from typing import Iterable

from entire_agent_codetriage.env import repo_root


def normalize_path(path: str, root: Path | None = None) -> str:
    value = path.replace("\\", "/").strip()
    if root is not None:
        try:
            value = Path(path).resolve().relative_to(root.resolve()).as_posix()
        except Exception:
            value = Path(path).as_posix()
    return value.lstrip("./")


def load_reverse_graph(root: Path | None = None) -> dict[str, set[str]]:
    """Return file -> files that depend on it (reverse edges)."""
    repo = root or repo_root()
    fixture = os.environ.get("CODETRIAGE_GRAPH_JSON")
    if fixture and Path(fixture).is_file():
        return _from_fixture(Path(fixture), repo)
    local_fixture = repo / ".codetriage" / "graph.json"
    if local_fixture.is_file():
        return _from_fixture(local_fixture, repo)
    return _from_entire_graph(repo)


def _from_fixture(path: Path, repo: Path) -> dict[str, set[str]]:
    data = json.loads(path.read_text(encoding="utf-8"))
    edges = defaultdict(set)
    raw_edges = data.get("reverse_dependencies") or data.get("reverse") or data.get("edges") or data
    if isinstance(raw_edges, dict):
        for src, dests in raw_edges.items():
            if src in {"files", "symbols", "relations"}:
                continue
            src_n = normalize_path(str(src), repo)
            if isinstance(dests, dict):
                continue
            for dest in dests or []:
                edges[src_n].add(normalize_path(str(dest), repo))
    for relation in data.get("relations") or []:
        if not isinstance(relation, dict):
            continue
        src = relation.get("from") or relation.get("from_path") or relation.get("from_id")
        dest = relation.get("to") or relation.get("to_path") or relation.get("to_id")
        if src and dest:
            # A depends on B means B -> A in reverse graph.
            edges[normalize_path(str(dest), repo)].add(normalize_path(str(src), repo))
    return dict(edges)


def _from_entire_graph(repo: Path) -> dict[str, set[str]]:
    entire = shutil.which("entire")
    if not entire:
        return {}

    files: dict[str, str] = {}
    symbols: dict[str, str] = {}
    edges: dict[str, set[str]] = defaultdict(set)

    for args in (
        [entire, "graph", "edges", "--repo", str(repo), "--format", "ndjson"],
        [entire, "graph", "snapshot", "--repo", str(repo), "--format", "ndjson", "--worktree"],
    ):
        records = _run_ndjson(args)
        if records:
            _ingest_records(records, files, symbols, edges)
            if edges:
                break
    return dict(edges)


def _run_ndjson(args: list[str]) -> list[dict]:
    try:
        proc = subprocess.run(
            args,
            check=False,
            capture_output=True,
            text=True,
            timeout=60,
        )
    except (OSError, subprocess.TimeoutExpired):
        return []
    if proc.returncode != 0:
        return []
    records = []
    for line in proc.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            record = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(record, dict):
            records.append(record)
    return records


def _ingest_records(
    records: Iterable[dict],
    files: dict[str, str],
    symbols: dict[str, str],
    edges: dict[str, set[str]],
) -> None:
    for record in records:
        kind = record.get("record_type") or record.get("type")
        if kind == "file":
            record_id = str(record.get("id") or "")
            path = record.get("path") or record.get("file_path")
            if record_id and path:
                files[record_id] = normalize_path(str(path))
        elif kind == "symbol":
            record_id = str(record.get("id") or "")
            path = record.get("file_path") or record.get("path")
            if record_id and path:
                symbols[record_id] = normalize_path(str(path))
        elif kind == "relation":
            from_id = str(record.get("from_id") or record.get("from") or "")
            to_id = str(record.get("to_id") or record.get("to") or "")
            from_path = _resolve_path(from_id, files, symbols, record.get("from_path"))
            to_path = _resolve_path(to_id, files, symbols, record.get("to_path"))
            if from_path and to_path and from_path != to_path:
                # Reverse: dependents of `to` include `from`.
                edges[to_path].add(from_path)


def _resolve_path(
    record_id: str,
    files: dict[str, str],
    symbols: dict[str, str],
    explicit: object,
) -> str | None:
    if explicit:
        return normalize_path(str(explicit))
    if record_id in files:
        return files[record_id]
    if record_id in symbols:
        return symbols[record_id]
    if record_id.endswith((".py", ".go", ".ts", ".js", ".java", ".rs")):
        return normalize_path(record_id)
    return None
