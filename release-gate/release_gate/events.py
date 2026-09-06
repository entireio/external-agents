"""Adaptive lifecycle-event ingestion.

Curveball response: the integrated workflow released a NEW transcript /
lifecycle-event format while existing users still emit the ORIGINAL format.

Design (no duplication): a single version-detecting parser normalizes BOTH
formats into one internal event model, then ``events_to_bundle`` assembles the
same evidence-bundle contract the rest of the pipeline already consumes. So
Silver/Gold/scoring/writeback/handoff are untouched.

Safety rules from the constraint:
  * Unknown events never crash — they are counted and reported, not fatal.
  * A corrupt/truncated line is tolerated (counted as a parse error).
  * Incomplete input yields a PARTIAL bundle (``partial: true``) so the product
    never presents incomplete context as complete/authoritative.
"""
from __future__ import annotations

import json
from typing import Any

# v1 event `type` -> normalized kind
_V1_KINDS = {
    "pr": "pr", "graph_impact": "graph",
    "checkpoint": "checkpoint", "test": "test",
}
# v2 `event` prefix -> normalized kind
_V2_PREFIX = {
    "pull_request": "pr", "graph": "graph",
    "checkpoint": "checkpoint", "test": "test",
}


def _norm_pr_v1(o: dict) -> dict:
    return {k: o.get(k) for k in (
        "repo", "number", "revision_sha", "base_sha", "author", "title")} | {
        "additions": o.get("additions", 0), "deletions": o.get("deletions", 0),
        "files_changed": o.get("files_changed", 0)}


def _norm_pr_v2(p: dict) -> dict:
    churn = p.get("churn", {}) or {}
    return {
        "repo": p.get("repo"), "number": p.get("pr"),
        "revision_sha": p.get("head"), "base_sha": p.get("base"),
        "author": p.get("author"), "title": p.get("title"),
        "additions": churn.get("add", 0), "deletions": churn.get("del", 0),
        "files_changed": churn.get("files", 0),
    }


def _normalize(obj: dict) -> tuple[str | None, Any, int]:
    """Return (kind, data, format_version). kind None => unknown event."""
    if "event" in obj and "payload" in obj:  # v2 (new)
        prefix = str(obj["event"]).split(".")[0]
        kind = _V2_PREFIX.get(prefix)
        p = obj["payload"] or {}
        if kind == "pr":
            return "pr", _norm_pr_v2(p), 2
        if kind == "graph":
            return "graph", {"symbol": p.get("name"), "file": p.get("path"),
                             "kind": p.get("kind"), "relationship": p.get("rel"),
                             "distance": p.get("depth", 1)}, 2
        if kind == "checkpoint":
            return "checkpoint", {"checkpoint_id": p.get("id"),
                                  "created_at": p.get("at"),
                                  "intent": p.get("intent", ""),
                                  "unresolved_risks": p.get("risks", [])}, 2
        if kind == "test":
            return "test", {"name": p.get("name"), "status": p.get("outcome"),
                            "touched_symbols": p.get("symbols", [])}, 2
        return None, obj.get("event"), 2
    if "type" in obj:  # v1 (original)
        kind = _V1_KINDS.get(obj["type"])
        if kind == "pr":
            return "pr", _norm_pr_v1(obj), 1
        if kind == "graph":
            return "graph", {"symbol": obj.get("symbol"), "file": obj.get("file"),
                             "kind": obj.get("kind"), "relationship": obj.get("relationship"),
                             "distance": obj.get("distance", 1)}, 1
        if kind == "checkpoint":
            return "checkpoint", {"checkpoint_id": obj.get("checkpoint_id"),
                                  "created_at": obj.get("created_at"),
                                  "intent": obj.get("intent", ""),
                                  "unresolved_risks": obj.get("unresolved_risks", [])}, 1
        if kind == "test":
            return "test", {"name": obj.get("name"), "status": obj.get("status"),
                            "touched_symbols": obj.get("touched_symbols", [])}, 1
        return None, obj.get("type"), 1
    return None, None, 0


def parse_events(lines) -> dict:
    events: dict[str, Any] = {"pr": None, "graph": [], "checkpoint": [], "test": []}
    unknown: list[str] = []
    parse_errors = 0
    formats: set[int] = set()

    for line in lines:
        line = (line or "").strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except (json.JSONDecodeError, TypeError):
            parse_errors += 1  # truncated/corrupt line tolerated
            continue
        if not isinstance(obj, dict):
            unknown.append(str(obj)[:40])
            continue
        kind, data, ver = _normalize(obj)
        if ver:
            formats.add(ver)
        if kind is None:
            unknown.append(str(data or "unknown")[:60])
            continue
        if kind == "pr":
            events["pr"] = data
        else:
            events[kind].append(data)

    has_signal = any((events["pr"], events["graph"], events["checkpoint"], events["test"]))
    complete = bool(events["pr"]) and not parse_errors and any(
        (events["graph"], events["checkpoint"], events["test"]))
    return {
        "events": events, "unknown": unknown, "parse_errors": parse_errors,
        "formats": sorted(formats), "has_signal": has_signal, "complete": complete,
    }


def events_to_bundle(parsed: dict, pr_number: int = 0, pr_repo: str = "",
                     bundle_id: str = "events", generated_at: str = "") -> dict:
    ev = parsed["events"]
    pr = ev["pr"] or {"repo": pr_repo, "number": pr_number, "revision_sha": "",
                      "base_sha": "", "author": "", "title": "",
                      "additions": 0, "deletions": 0, "files_changed": 0}
    pr = {**pr, "number": pr.get("number") or pr_number,
          "repo": pr.get("repo") or pr_repo,
          "changed_files": [],
          "churn": {"additions": pr.get("additions", 0),
                    "deletions": pr.get("deletions", 0),
                    "files_changed": pr.get("files_changed", 0)}}

    graph_rows = [{"symbol": g.get("symbol") or "", "file": g.get("file") or "",
                   "kind": g.get("kind") or "unknown",
                   "relationship": g.get("relationship") or "RELATED",
                   "distance": int(g.get("distance") or 1)} for g in ev["graph"]]
    blast = {"symbol_count": len({g["symbol"] for g in graph_rows if g["symbol"]}),
             "file_count": len({g["file"] for g in graph_rows if g["file"]}),
             "max_distance": max((g["distance"] for g in graph_rows), default=0)}

    cps = [{"checkpoint_id": c.get("checkpoint_id") or "", "created_at": c.get("created_at") or "",
            "intent_summary": c.get("intent") or "", "rejected_options": [],
            "assumptions": [], "unresolved_risks": list(c.get("unresolved_risks") or []),
            "referenced_paths": []} for c in ev["checkpoint"]]
    risk_total = sum(len(c["unresolved_risks"]) for c in cps)

    tests = ev["test"]
    passed = sum(1 for t in tests if t.get("status") == "passed")
    failed = sum(1 for t in tests if t.get("status") == "failed")
    skipped = sum(1 for t in tests if t.get("status") == "skipped")
    cases = [{"name": t.get("name") or "", "status": t.get("status") or "skipped",
              "duration_s": 0.0, "touched_symbols": list(t.get("symbols") or t.get("touched_symbols") or [])}
             for t in tests]

    partial = (not parsed["complete"]) or parsed["parse_errors"] > 0 or bool(parsed["unknown"])
    return {
        "schema_version": "1.0.0",
        "bundle_id": bundle_id,
        "generated_at": generated_at or "1970-01-01T00:00:00Z",
        "source": "lifecycle-events",
        "pr": pr,
        "graph_impact": {"available": bool(graph_rows), "impacted_symbols": graph_rows,
                         "blast_radius": blast},
        "checkpoint_signals": {"available": bool(cps), "checkpoints": cps,
                               "unresolved_risk_count": risk_total},
        "test_results": {"available": bool(cases),
                         "summary": {"total": len(cases), "passed": passed, "failed": failed,
                                     "skipped": skipped, "duration_s": 0.0},
                         "cases": cases},
        # Completeness metadata so downstream never presents partial as authoritative.
        "ingest": {"partial": partial, "formats": parsed["formats"],
                   "unknown_events": len(parsed["unknown"]),
                   "parse_errors": parsed["parse_errors"]},
    }
