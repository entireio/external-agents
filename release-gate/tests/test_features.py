from release_gate.features import build_gold_features
from release_gate.silver import to_silver


def _bundle(**overrides):
    base = {
        "schema_version": "1.0.0",
        "bundle_id": "t1",
        "generated_at": "2026-09-06T00:00:00Z",
        "source": "test",
        "pr": {
            "repo": "o/r",
            "number": 7,
            "revision_sha": "abc123",
            "base_sha": "def456",
            "author": "dev",
            "title": "change",
            "changed_files": ["a.py"],
            "churn": {"additions": 100, "deletions": 20, "files_changed": 2},
        },
        "graph_impact": {
            "available": True,
            "impacted_symbols": [
                {"symbol": "foo", "file": "a.py", "kind": "function", "relationship": "CALLS", "distance": 1},
                {"symbol": "bar", "file": "b.py", "kind": "function", "relationship": "CALLS", "distance": 2},
            ],
            "blast_radius": {"symbol_count": 2, "file_count": 2, "max_distance": 2},
        },
        "checkpoint_signals": {
            "available": True,
            "checkpoints": [
                {
                    "checkpoint_id": "c1",
                    "created_at": "2026-09-06T00:00:00Z",
                    "intent_summary": "why",
                    "rejected_options": ["x"],
                    "assumptions": ["y"],
                    "unresolved_risks": ["risk a", "risk b", "risk c"],
                    "referenced_paths": ["a.py"],
                }
            ],
            "unresolved_risk_count": 3,
        },
        "test_results": {
            "available": True,
            "summary": {"total": 2, "passed": 1, "failed": 1, "skipped": 0, "duration_s": 0.1},
            "cases": [
                {"name": "t_foo", "status": "passed", "duration_s": 0.05, "touched_symbols": ["foo"]},
                {"name": "t_x", "status": "failed", "duration_s": 0.05, "touched_symbols": []},
            ],
        },
    }
    base.update(overrides)
    return base


def test_build_gold_features_basic():
    f = build_gold_features(to_silver(_bundle()))
    assert f["pr_number"] == 7
    assert f["blast_radius_symbols"] == 2
    assert f["max_impact_distance"] == 2
    assert f["unresolved_risk_count"] == 3
    assert f["churn_total"] == 120
    assert f["tests_total"] == 2 and f["tests_failed"] == 1
    assert f["test_failure_rate"] == 0.5


def test_impacted_symbol_coverage():
    # foo is tested, bar is not -> coverage 0.5
    f = build_gold_features(to_silver(_bundle()))
    assert f["impacted_symbols_tested"] == 1
    assert f["impacted_symbol_test_coverage"] == 0.5


def test_no_impacted_symbols_has_full_coverage():
    b = _bundle()
    b["graph_impact"] = {"available": True, "impacted_symbols": [], "blast_radius": {"symbol_count": 0, "file_count": 0, "max_distance": 0}}
    f = build_gold_features(to_silver(b))
    assert f["impacted_symbol_test_coverage"] == 1.0
