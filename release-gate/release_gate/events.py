"""Adaptive agent-session transcript ingestion.

Curveball response: the integrated agent released a NEW transcript /
lifecycle-event format (event-named, e.g. ``session_started``, ``file_changed``,
``tool_result``, ``checkpoint_created``) while existing users still emit the
ORIGINAL (``type``-named) format.

Design (no duplication): a single version-detecting parser normalizes BOTH
transcript formats into one internal model, then ``events_to_bundle`` assembles
the same evidence-bundle contract the rest of the pipeline already consumes
(Silver/Gold/scoring/writeback untouched).

Safety rules from the constraint:
  * Unknown event types never crash — counted and reported, not fatal.
  * A corrupt/truncated line is tolerated (counted as a parse error).
  * An incomplete transcript yields a PARTIAL bundle (``ingest.partial``) so the
    product never presents incomplete context as complete/authoritative.
  * Existing checkpoint behaviour stays compatible: ``open_questions`` (new) and
    ``risks`` (original) both map to unresolved-risk signals.
"""
from __future__ import annotations

import json
import re

# Event name (either schema) -> canonical kind. Names absent here but present in
# a schema are "known but carry no evidence" (skipped, not unknown).
_KIND = {
    # new (event-named) transcript
    "session_started": "start", "file_changed": "change", "tool_call": "tool_call",
    "tool_result": "tool_result", "checkpoint_created": "checkpoint",
    "session_ended": "end",
    # original (type-named) transcript
    "start": "start", "edit": "change", "test": "test",
    "checkpoint": "checkpoint", "end": "end",
}
# Recognised events that intentionally contribute no evidence.
_IGNORED = {"user_prompt", "agent_response", "file_read", "usage",
            "prompt", "response", "read"}


def _event_name(obj: dict) -> tuple[str | None, str | None]:
    if "event" in obj:
        return obj["event"], "new"
    if "type" in obj:
        return obj["type"], "original"
    return None, None


def _parse_test_summary(summary: str) -> dict:
    def _n(pat):
        m = re.search(pat, summary or "")
        return int(m.group(1)) if m else 0
    return {"passed": _n(r"(\d+)\s+passed"), "failed": _n(r"(\d+)\s+failed"),
            "skipped": _n(r"(\d+)\s+skipped")}


def parse_events(lines) -> dict:
    ev = {"meta": {}, "changes": [], "checkpoints": [], "tests": None, "ended": False}
    unknown: list[str] = []
    parse_errors = 0
    formats: set[str] = set()
    pending: dict[str, str] = {}  # tool call_id -> command

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

        name, schema = _event_name(obj)
        if schema:
            formats.add(schema)
        if name in _IGNORED:
            continue
        kind = _KIND.get(name)
        if kind is None:
            unknown.append(str(name or "unknown")[:60])
            continue

        if kind == "start":
            agent = obj.get("agent")
            ev["meta"] = {
                "repo": obj.get("repository") or obj.get("repo"),
                "branch": obj.get("branch"),
                "agent": agent.get("name") if isinstance(agent, dict) else agent,
            }
        elif kind == "change":
            ev["changes"].append({
                "file": obj.get("path") or obj.get("file"),
                "added": int(obj.get("lines_added") or obj.get("added") or 0),
                "removed": int(obj.get("lines_removed") or obj.get("removed") or 0),
            })
        elif kind == "tool_call":
            pending[obj.get("call_id")] = (obj.get("input") or {}).get("command", "")
        elif kind == "tool_result":
            cmd = pending.get(obj.get("call_id"), "")
            if "test" in cmd.lower():
                out = obj.get("output") or {}
                ev["tests"] = _parse_test_summary(out.get("summary", ""))
        elif kind == "test":  # original schema reports results directly
            ev["tests"] = {"passed": int(obj.get("passed") or 0),
                           "failed": int(obj.get("failed") or 0),
                           "skipped": int(obj.get("skipped") or 0)}
        elif kind == "checkpoint":
            ev["checkpoints"].append({
                "id": obj.get("checkpoint_id") or obj.get("id"),
                "intent": obj.get("intent", ""),
                "risks": obj.get("open_questions") or obj.get("risks") or [],
                "summary": obj.get("summary", ""),
                "git_commit": obj.get("git_commit", ""),
            })
        elif kind == "end":
            ev["ended"] = True

    has_signal = bool(ev["changes"] or ev["checkpoints"] or ev["tests"] is not None)
    complete = (ev["ended"] and bool(ev["checkpoints"] or ev["changes"])
                and parse_errors == 0 and not unknown)
    return {"events": ev, "unknown": unknown, "parse_errors": parse_errors,
            "formats": sorted(formats), "has_signal": has_signal, "complete": complete}


def events_to_bundle(parsed: dict, pr_number: int = 0, pr_repo: str = "",
                     bundle_id: str = "transcript", generated_at: str = "") -> dict:
    ev = parsed["events"]
    meta = ev["meta"]
    changes = ev["changes"]
    adds = sum(c["added"] for c in changes)
    rems = sum(c["removed"] for c in changes)
    files = sorted({c["file"] for c in changes if c["file"]})
    last_cp = ev["checkpoints"][-1] if ev["checkpoints"] else {}

    pr = {
        "repo": meta.get("repo") or pr_repo, "number": pr_number,
        "revision_sha": last_cp.get("git_commit", ""), "base_sha": "",
        "author": meta.get("agent", "") or "", "title": meta.get("branch") or last_cp.get("summary", "") or "",
        "changed_files": files,
        "churn": {"additions": adds, "deletions": rems, "files_changed": len(files)},
    }

    cps = [{"checkpoint_id": c.get("id") or "", "created_at": "",
            "intent_summary": c.get("intent") or c.get("summary") or "",
            "rejected_options": [], "assumptions": [],
            "unresolved_risks": list(c.get("risks") or []), "referenced_paths": []}
           for c in ev["checkpoints"]]
    risk_total = sum(len(c["unresolved_risks"]) for c in cps)

    t = ev["tests"] or {}
    passed, failed, skipped = (int(t.get("passed", 0)), int(t.get("failed", 0)),
                               int(t.get("skipped", 0)))
    total = passed + failed + skipped
    # The transcript reports aggregate counts only; synthesize cases so the
    # cases-driven downstream (Silver/Gold) reflects them without changes there.
    cases = ([{"name": f"passed_{i}", "status": "passed", "duration_s": 0.0, "touched_symbols": []} for i in range(passed)]
             + [{"name": f"failed_{i}", "status": "failed", "duration_s": 0.0, "touched_symbols": []} for i in range(failed)]
             + [{"name": f"skipped_{i}", "status": "skipped", "duration_s": 0.0, "touched_symbols": []} for i in range(skipped)])

    partial = (not parsed["complete"]) or parsed["parse_errors"] > 0 or bool(parsed["unknown"])
    return {
        "schema_version": "1.0.0",
        "bundle_id": bundle_id,
        "generated_at": generated_at or "1970-01-01T00:00:00Z",
        "source": "agent-transcript",
        "pr": pr,
        # A transcript carries no static-graph evidence; that arrives separately
        # via `entire graph`. Marked unavailable so the score degrades honestly.
        "graph_impact": {"available": False, "impacted_symbols": [],
                         "blast_radius": {"symbol_count": 0, "file_count": 0, "max_distance": 0}},
        "checkpoint_signals": {"available": bool(cps), "checkpoints": cps,
                               "unresolved_risk_count": risk_total},
        "test_results": {"available": ev["tests"] is not None,
                         "summary": {"total": total, "passed": passed,
                                     "failed": failed, "skipped": skipped, "duration_s": 0.0},
                         "cases": cases},
        "ingest": {"partial": partial, "formats": parsed["formats"],
                   "unknown_events": len(parsed["unknown"]),
                   "parse_errors": parsed["parse_errors"]},
    }
