from __future__ import annotations

from entire_agent_codetriage.blast_radius import classify_esi, compute_blast_radius


def test_classify_esi_depth_threshold() -> None:
    assert classify_esi(3, 1) == 1
    assert classify_esi(2, 1) == 5


def test_classify_esi_impact_threshold() -> None:
    assert classify_esi(1, 10) == 1
    assert classify_esi(1, 9) == 5


def test_bfs_depth_three_is_critical() -> None:
    graph = {
        "core.py": {"mid.py"},
        "mid.py": {"edge.py"},
        "edge.py": {"leaf.py"},
    }
    result = compute_blast_radius(["core.py"], reverse_graph=graph)
    assert result.depth >= 3
    assert result.esi_level == 1
    assert result.blocked is True
    assert result.impacted == ["core.py", "mid.py", "edge.py", "leaf.py"]


def test_bfs_wide_fanout_is_critical() -> None:
    dependents = {f"svc_{i}.py" for i in range(12)}
    graph = {"shared.py": dependents}
    result = compute_blast_radius(["shared.py"], reverse_graph=graph)
    assert result.impacted_count >= 10
    assert result.esi_level == 1
    assert result.blocked is True


def test_small_change_is_allowed() -> None:
    graph = {"util.py": {"app.py"}}
    result = compute_blast_radius(["util.py"], reverse_graph=graph)
    assert result.depth == 1
    assert result.impacted_count == 2
    assert result.esi_level == 5
    assert result.blocked is False


def test_empty_seeds_are_safe() -> None:
    result = compute_blast_radius([], reverse_graph={"a.py": {"b.py"}})
    assert result.depth == 0
    assert result.impacted_count == 0
    assert result.blocked is False
