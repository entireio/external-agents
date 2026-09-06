"""Bronze evidence bundle -> typed Silver rows.

Each Silver row is stamped with the (pr_number, revision_sha) idempotency key so
the same MERGE key flows through every Delta layer.
"""
from __future__ import annotations

from typing import Any


def to_silver(bundle: dict[str, Any]) -> dict[str, Any]:
    pr = bundle.get("pr", {}) or {}
    number = pr.get("number")
    sha = pr.get("revision_sha")
    churn = pr.get("churn", {}) or {}

    graph = bundle.get("graph_impact", {}) or {}
    checks = bundle.get("checkpoint_signals", {}) or {}
    tests = bundle.get("test_results", {}) or {}

    pr_row = {
        "pr_number": number,
        "revision_sha": sha,
        "repo": pr.get("repo"),
        "base_sha": pr.get("base_sha"),
        "author": pr.get("author"),
        "title": pr.get("title"),
        "additions": int(churn.get("additions", 0) or 0),
        "deletions": int(churn.get("deletions", 0) or 0),
        "files_changed": int(churn.get("files_changed", 0) or 0),
    }

    graph_rows = [
        {
            "pr_number": number,
            "revision_sha": sha,
            "symbol": s.get("symbol"),
            "file": s.get("file"),
            "kind": s.get("kind"),
            "relationship": s.get("relationship"),
            "distance": int(s.get("distance", 0) or 0),
        }
        for s in (graph.get("impacted_symbols") or [])
    ]

    checkpoint_rows = [
        {
            "pr_number": number,
            "revision_sha": sha,
            "checkpoint_id": c.get("checkpoint_id"),
            "created_at": c.get("created_at"),
            "intent_summary": c.get("intent_summary"),
            "rejected_option_count": len(c.get("rejected_options") or []),
            "assumption_count": len(c.get("assumptions") or []),
            "unresolved_risk_count": len(c.get("unresolved_risks") or []),
            "unresolved_risks": list(c.get("unresolved_risks") or []),
            "referenced_path_count": len(c.get("referenced_paths") or []),
        }
        for c in (checks.get("checkpoints") or [])
    ]

    test_rows = [
        {
            "pr_number": number,
            "revision_sha": sha,
            "name": t.get("name"),
            "status": t.get("status"),
            "duration_s": float(t.get("duration_s", 0) or 0),
            "touched_symbols": list(t.get("touched_symbols") or []),
        }
        for t in (tests.get("cases") or [])
    ]

    return {
        "pr": pr_row,
        "graph_impact": graph_rows,
        "checkpoint_signals": checkpoint_rows,
        "test_results": test_rows,
        "graph_available": bool(graph.get("available", False)),
        "checkpoints_available": bool(checks.get("available", False)),
        "tests_available": bool(tests.get("available", False)),
        # Aggregates carried through from bronze so Gold stays a pure function
        # of Silver even when a layer degrades to "unavailable".
        "blast_radius": graph.get("blast_radius", {}) or {},
        "unresolved_risk_count_reported": int(
            checks.get("unresolved_risk_count", 0) or 0
        ),
        "test_summary": tests.get("summary", {}) or {},
    }
