"""Level-tracked BFS blast radius and Emergency Severity Index."""

from __future__ import annotations

from collections import deque
from dataclasses import dataclass, field
from pathlib import Path

from entire_agent_codetriage.graph import load_reverse_graph, normalize_path

CRITICAL_DEPTH = 3
CRITICAL_IMPACT = 10
ESI_CRITICAL = 1
ESI_LOW = 5


@dataclass
class BlastRadius:
    seeds: list[str]
    impacted: list[str]
    depth: int
    esi_level: int
    blocked: bool
    levels: dict[int, list[str]] = field(default_factory=dict)

    @property
    def impacted_count(self) -> int:
        return len(self.impacted)


def classify_esi(depth: int, impacted_count: int) -> int:
    if depth >= CRITICAL_DEPTH or impacted_count >= CRITICAL_IMPACT:
        return ESI_CRITICAL
    return ESI_LOW


def compute_blast_radius(
    seed_files: list[str],
    reverse_graph: dict[str, set[str]] | None = None,
    repo: Path | None = None,
) -> BlastRadius:
    seeds = [normalize_path(path, repo) for path in seed_files if path]
    seeds = list(dict.fromkeys(seeds))
    graph = reverse_graph if reverse_graph is not None else load_reverse_graph(repo)

    visited: set[str] = set()
    levels: dict[int, list[str]] = {}
    max_depth = 0
    queue: deque[tuple[str, int]] = deque((seed, 0) for seed in seeds)

    while queue:
        node, depth = queue.popleft()
        if node in visited:
            continue
        visited.add(node)
        levels.setdefault(depth, []).append(node)
        if depth > max_depth:
            max_depth = depth
        for neighbor in sorted(graph.get(node, ())):
            if neighbor not in visited:
                queue.append((neighbor, depth + 1))

    impacted = [path for _, paths in sorted(levels.items()) for path in paths]
    esi = classify_esi(max_depth if seeds else 0, len(impacted))
    return BlastRadius(
        seeds=seeds,
        impacted=impacted,
        depth=max_depth if seeds else 0,
        esi_level=esi,
        blocked=esi == ESI_CRITICAL,
        levels=levels,
    )
