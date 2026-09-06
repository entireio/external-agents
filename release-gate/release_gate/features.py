"""Silver rows -> one Gold feature row per PR revision.

`build_gold_features` is a *pure function of the Silver tables* (extension seam
b): new risk features can be added here without touching ingestion or scoring.
The two most important features -- ``unresolved_risk_count`` (from Entire
Checkpoints) and ``blast_radius_symbols`` (from Entire Graph) -- are
uncomputable without Entire.
"""
from __future__ import annotations

from typing import Any


def build_gold_features(silver: dict[str, Any]) -> dict[str, Any]:
    pr = silver.get("pr", {}) or {}
    graph_rows = silver.get("graph_impact", []) or []
    checkpoint_rows = silver.get("checkpoint_signals", []) or []
    test_rows = silver.get("test_results", []) or []
    blast = silver.get("blast_radius", {}) or {}

    # --- Graph (structural) features ---
    impacted_symbols = {r.get("symbol") for r in graph_rows if r.get("symbol")}
    blast_radius_symbols = int(blast.get("symbol_count") or len(impacted_symbols))
    blast_radius_files = int(
        blast.get("file_count")
        or len({r.get("file") for r in graph_rows if r.get("file")})
    )
    max_impact_distance = int(
        blast.get("max_distance")
        or max((r.get("distance", 0) for r in graph_rows), default=0)
    )

    # --- Checkpoint (intent/risk) features ---
    unresolved_risk_count = sum(
        r.get("unresolved_risk_count", 0) for r in checkpoint_rows
    )
    if not unresolved_risk_count:
        # Fall back to the bronze-reported aggregate when per-checkpoint
        # extraction is empty but the collector reported a total.
        unresolved_risk_count = silver.get("unresolved_risk_count_reported", 0)
    checkpoint_count = len(checkpoint_rows)
    rejected_option_count = sum(r.get("rejected_option_count", 0) for r in checkpoint_rows)
    assumption_count = sum(r.get("assumption_count", 0) for r in checkpoint_rows)

    # --- Churn features ---
    additions = int(pr.get("additions", 0) or 0)
    deletions = int(pr.get("deletions", 0) or 0)
    churn_total = additions + deletions
    files_changed = int(pr.get("files_changed", 0) or 0)

    # --- Test (correctness) features ---
    tests_total = len(test_rows)
    tests_passed = sum(1 for r in test_rows if r.get("status") == "passed")
    tests_failed = sum(1 for r in test_rows if r.get("status") == "failed")
    tests_skipped = sum(1 for r in test_rows if r.get("status") == "skipped")
    test_failure_rate = (tests_failed / tests_total) if tests_total else 0.0

    # Coverage of impacted symbols by tests (join Silver test_results to graph_impact).
    tested_symbols: set[str] = set()
    for r in test_rows:
        tested_symbols.update(r.get("touched_symbols") or [])
    covered = impacted_symbols & tested_symbols
    if impacted_symbols:
        impacted_symbol_test_coverage = len(covered) / len(impacted_symbols)
    else:
        impacted_symbol_test_coverage = 1.0  # nothing impacted -> no coverage gap

    return {
        "pr_number": pr.get("pr_number"),
        "revision_sha": pr.get("revision_sha"),
        "repo": pr.get("repo"),
        "author": pr.get("author"),
        "title": pr.get("title"),
        # graph
        "blast_radius_symbols": blast_radius_symbols,
        "blast_radius_files": blast_radius_files,
        "max_impact_distance": max_impact_distance,
        # checkpoints
        "unresolved_risk_count": int(unresolved_risk_count),
        "checkpoint_count": checkpoint_count,
        "rejected_option_count": rejected_option_count,
        "assumption_count": assumption_count,
        # churn
        "additions": additions,
        "deletions": deletions,
        "churn_total": churn_total,
        "files_changed": files_changed,
        # tests
        "tests_total": tests_total,
        "tests_passed": tests_passed,
        "tests_failed": tests_failed,
        "tests_skipped": tests_skipped,
        "test_failure_rate": round(test_failure_rate, 4),
        "impacted_symbols_tested": len(covered),
        "impacted_symbol_test_coverage": round(impacted_symbol_test_coverage, 4),
        # provenance / degradation flags
        "graph_available": bool(silver.get("graph_available", False)),
        "checkpoints_available": bool(silver.get("checkpoints_available", False)),
        "tests_available": bool(silver.get("tests_available", False)),
    }
