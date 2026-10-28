"""Evidence collection for Release Gate.

Turns real Entire Graph + Entire Checkpoint output into one schema-valid
evidence bundle. Lives in the package so both the CLI wrapper
(``integration/ci_hook/collect_evidence.py``) and the native Entire plugin
(``release_gate.plugin``) share the exact same logic.

Design rules: graph/checkpoint parsing is wrapped defensively (degrades to
``available: false`` instead of crashing); graph output is evidence, not an
oracle; no secrets are read or written.
"""
from __future__ import annotations

import datetime as _dt
import json
import re
import subprocess
import sys
import uuid

_RISK_PATTERNS = re.compile(
    r"\b(risk|unresolved|open question|todo|fixme|assumption|not yet|"
    r"untested|blocked|caveat|limitation|hack|workaround)\b",
    re.IGNORECASE,
)


def _run(cmd: list[str], cwd: str | None = None, timeout: int = 180) -> tuple[int, str, str]:
    try:
        proc = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout)
        return proc.returncode, proc.stdout, proc.stderr
    except (OSError, subprocess.SubprocessError) as exc:  # pragma: no cover
        return 1, "", str(exc)


def _now_iso() -> str:
    return _dt.datetime.now(_dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def collect_pr_meta(repo: str, base: str | None, head: str,
                    pr_number: int, pr_repo: str, author: str, title: str) -> dict:
    changed_files: list[str] = []
    additions = deletions = 0
    rng = f"{base}..{head}" if base else f"{head}~1..{head}"
    rc, out, _ = _run(["git", "-C", repo, "diff", "--numstat", rng])
    if rc == 0:
        for line in out.splitlines():
            parts = line.split("\t")
            if len(parts) == 3:
                add, dele, path = parts
                additions += int(add) if add.isdigit() else 0
                deletions += int(dele) if dele.isdigit() else 0
                changed_files.append(path)
    return {
        "repo": pr_repo, "number": pr_number, "revision_sha": head,
        "base_sha": base or "", "author": author, "title": title,
        "changed_files": changed_files,
        "churn": {"additions": additions, "deletions": deletions,
                  "files_changed": len(changed_files)},
    }


def _changed_symbols(repo: str, head: str, base: str | None) -> list[dict]:
    if base:
        cmd = ["entire", "graph", "diff", "--base", base, "--head", head, "--json", "--repo", repo]
    else:
        cmd = ["entire", "graph", "commit", head, "--json", "--repo", repo]
    rc, out, _ = _run(cmd)
    if rc != 0 or not out.strip():
        return []
    try:
        data = json.loads(out)
    except json.JSONDecodeError:
        return []
    wanted = {"function", "method", "class", "type", "interface", "struct", "enum"}
    seen: set[str] = set()
    syms: list[dict] = []
    for f in data.get("files") or []:
        path = f.get("path")
        for ch in f.get("changes") or []:
            name, kind = ch.get("name"), ch.get("kind")
            if name and (kind in wanted) and name not in seen:
                seen.add(name)
                syms.append({"name": name, "file": path, "kind": kind})
    return syms


def _impact_json(repo: str, symbol: str) -> dict | None:
    rc, out, _ = _run(["entire", "graph", "impact", "--symbol", symbol,
                       "--repo", repo, "--format", "json"])
    if rc != 0 or not out.strip():
        return None
    try:
        return json.loads(out)
    except json.JSONDecodeError:
        return None


def _rows_from_impact(data: dict) -> list[dict]:
    rows: list[dict] = []
    for section in ("callers", "callees", "type_consumers"):
        for entry in (data.get(section) or {}).get("entries", []) or []:
            ep = entry.get("endpoint") or {}
            name = ep.get("name")
            if not name:
                continue
            rows.append({
                "symbol": name, "file": ep.get("file_path") or "",
                "kind": ep.get("kind") or "unknown",
                "relationship": entry.get("relation", "RELATED"),
                "distance": int(entry.get("depth", 1) or 1),
            })
    return rows


def _is_test_endpoint(ep: dict) -> bool:
    fp = (ep.get("file_path") or "").lower()
    nm = ep.get("name") or ""
    return "test" in fp or nm.startswith("test_")


def collect_graph_impact(repo: str, head: str, base: str | None,
                         changed_files: list[str], max_symbols: int = 25) -> tuple[dict, dict]:
    try:
        changed = _changed_symbols(repo, head, base)[:max_symbols]
        impacted: dict[str, dict] = {}
        test_touches: dict[str, set] = {}
        for sym in changed:
            data = _impact_json(repo, sym["name"])
            if not data:
                continue
            for row in _rows_from_impact(data):
                impacted.setdefault(f"{row['file']}::{row['symbol']}", row)
            for entry in (data.get("callers") or {}).get("entries", []) or []:
                ep = entry.get("endpoint") or {}
                if _is_test_endpoint(ep) and ep.get("name"):
                    test_touches.setdefault(ep["name"], set()).add(sym["name"])
        rows = list(impacted.values())
        blast = {
            "symbol_count": len({r["symbol"] for r in rows}),
            "file_count": len({r["file"] for r in rows if r["file"]}),
            "max_distance": max((r["distance"] for r in rows), default=0),
        }
        block = {"available": bool(changed), "impacted_symbols": rows, "blast_radius": blast}
        return block, {k: sorted(v) for k, v in test_touches.items()}
    except Exception as exc:  # noqa: BLE001
        sys.stderr.write(f"[collect] graph impact unavailable: {exc}\n")
        return ({"available": False, "impacted_symbols": [],
                 "blast_radius": {"symbol_count": 0, "file_count": 0, "max_distance": 0}}, {})


def _extract_risks(text: str) -> list[str]:
    risks: list[str] = []
    for line in (text or "").splitlines():
        if _RISK_PATTERNS.search(line):
            cleaned = line.strip("-* \t")
            if cleaned:
                risks.append(cleaned[:200])
    return risks


def collect_checkpoint_signals(repo: str, changed_files: list[str], limit: int = 20) -> dict:
    try:
        rc, out, _ = _run(["entire", "checkpoint", "list", "--json"])
        if rc != 0 or not out.strip():
            return {"available": False, "checkpoints": [], "unresolved_risk_count": 0}
        listing = json.loads(out)[:limit]
        checkpoints: list[dict] = []
        total_risk = 0
        for cp in listing:
            cid = cp.get("checkpoint_id")
            intent = cp.get("message", "") or ""
            rc2, out2, _ = _run(["entire", "checkpoint", "explain", cid, "--json", "--short"])
            if rc2 == 0 and out2.strip():
                try:
                    env = json.loads(out2)
                    intent = env.get("intent") or env.get("summary") or intent
                except json.JSONDecodeError:
                    pass
            risks = _extract_risks(intent + "\n" + cp.get("message", ""))
            total_risk += len(risks)
            checkpoints.append({
                "checkpoint_id": cid, "created_at": cp.get("date", _now_iso()),
                "intent_summary": intent[:500], "rejected_options": [],
                "assumptions": [], "unresolved_risks": risks, "referenced_paths": [],
            })
        return {"available": True, "checkpoints": checkpoints,
                "unresolved_risk_count": total_risk}
    except Exception as exc:  # noqa: BLE001
        sys.stderr.write(f"[collect] checkpoint signals unavailable: {exc}\n")
        return {"available": False, "checkpoints": [], "unresolved_risk_count": 0}


def collect_test_results(repo: str, run_tests: bool, test_touches: dict | None = None) -> dict:
    test_touches = test_touches or {}
    if not run_tests:
        return {"available": False,
                "summary": {"total": 0, "passed": 0, "failed": 0, "skipped": 0, "duration_s": 0.0},
                "cases": []}
    rc, out, err = _run([sys.executable, "-m", "pytest", "-v", "--no-header"], cwd=repo)
    text = out + "\n" + err
    cases: list[dict] = []
    counts = {"passed": 0, "failed": 0, "skipped": 0}
    for line in text.splitlines():
        m = re.search(r"::(\w+)\s+(PASSED|FAILED|SKIPPED)", line)
        if not m:
            continue
        name, status = m.group(1), m.group(2).lower()
        counts[status] = counts.get(status, 0) + 1
        cases.append({"name": name, "status": status, "duration_s": 0.0,
                      "touched_symbols": test_touches.get(name, [])})
    total = sum(counts.values())
    return {"available": total > 0,
            "summary": {"total": total, "passed": counts["passed"],
                        "failed": counts["failed"], "skipped": counts["skipped"],
                        "duration_s": 0.0},
            "cases": cases}


def build_bundle(repo: str = ".", base: str | None = None, head: str = "HEAD",
                 pr_number: int = 0, pr_repo: str = "", author: str = "",
                 title: str = "", run_tests: bool = False) -> dict:
    pr = collect_pr_meta(repo, base, head, pr_number, pr_repo, author, title)
    graph_impact, test_touches = collect_graph_impact(repo, head, base, pr["changed_files"])
    return {
        "schema_version": "1.0.0",
        "bundle_id": str(uuid.uuid4()),
        "generated_at": _now_iso(),
        "source": "entire-ci-hook",
        "pr": pr,
        "graph_impact": graph_impact,
        "checkpoint_signals": collect_checkpoint_signals(repo, pr["changed_files"]),
        "test_results": collect_test_results(repo, run_tests, test_touches),
    }
