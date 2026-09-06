"""Agent handoff / context bridge.

Reconstructs the *previous* development context from Entire Checkpoints (+ Graph)
so a fresh coding-agent session can safely continue: intent, decisions, rejected
options, failed attempts, open risks, unresolved work, and what the change
touches. This is the "Agent A -> checkpoint -> Agent B resumes" workflow.
"""
from __future__ import annotations

import glob
import os
import re

from .collect import collect_checkpoint_signals, collect_graph_impact

# Category -> line matcher. Heuristic (documented as such), applied to checkpoint
# intent text and committed architecture/decision docs.
_CATEGORIES = {
    "decisions": re.compile(r"\b(decid|chose|use |adopt|switch to|will use|selected)\b", re.I),
    "rejected": re.compile(r"\b(reject|instead of|ruled out|avoid|not use|discard)\b", re.I),
    "failed": re.compile(r"\b(fail|didn'?t work|broke|regress|error)\b", re.I),
    "risks": re.compile(r"\b(risk|caveat|limitation|assumption|hack|workaround)\b", re.I),
    "unresolved": re.compile(r"\b(unresolved|open question|todo|fixme|not yet|pending|next step)\b", re.I),
}


def _categorize(text: str) -> dict[str, list[str]]:
    out: dict[str, list[str]] = {k: [] for k in _CATEGORIES}
    for raw in (text or "").splitlines():
        line = raw.strip("-*# \t")
        if len(line) < 6:
            continue
        for cat, pat in _CATEGORIES.items():
            if pat.search(line):
                out[cat].append(line[:200])
                break
    return {k: _dedupe(v)[:6] for k, v in out.items()}


def _dedupe(items: list[str]) -> list[str]:
    seen, out = set(), []
    for i in items:
        key = i.lower()
        if key not in seen:
            seen.add(key)
            out.append(i)
    return out


def _doc_text(repo: str) -> str:
    parts = []
    for path in sorted(glob.glob(os.path.join(repo, "docs", "**", "*.md"), recursive=True)):
        try:
            with open(path, "r", encoding="utf-8") as fh:
                parts.append(fh.read())
        except OSError:
            continue
    return "\n".join(parts)


# Heading keyword -> category, for section-accurate extraction from docs.
_HEADING_MAP = [
    ("rejected", "rejected"),
    ("decision", "decisions"),
    ("failed", "failed"),
    ("unresolved", "unresolved"),
    ("open question", "unresolved"),
    ("open risk", "risks"),
    ("technical risk", "risks"),
    ("risk", "risks"),
]


def _doc_sections(repo: str) -> dict[str, list[str]]:
    """Extract bullet lines under headings that map to a category."""
    out: dict[str, list[str]] = {c: [] for c in _CATEGORIES}
    cat = None
    for line in _doc_text(repo).splitlines():
        if line.lstrip().startswith("#"):
            heading = line.strip("# ").lower()
            cat = next((c for kw, c in _HEADING_MAP if kw in heading), None)
            continue
        if cat and line.lstrip().startswith(("-", "*")):
            item = line.strip("-*# \t")
            if len(item) >= 6:
                out[cat].append(item[:200])
    return {k: _dedupe(v)[:6] for k, v in out.items()}


def build_handoff(repo: str = ".", base: str | None = None, head: str = "HEAD") -> dict:
    cps = collect_checkpoint_signals(repo, [])
    checkpoints = cps.get("checkpoints", [])
    intents = [c.get("intent_summary", "") for c in checkpoints if c.get("intent_summary")]

    # Section-accurate signals from committed decision/architecture docs, plus
    # keyword signals from checkpoint intents.
    docs = _doc_sections(repo)
    intent_cats = _categorize("\n".join(intents))

    def _merge(cat):
        return _dedupe(docs.get(cat, []) + intent_cats.get(cat, []))[:6]

    risks = []
    for c in checkpoints:
        risks.extend(c.get("unresolved_risks", []))
    open_risks = _dedupe(docs.get("risks", []) + risks)[:6]

    graph, _ = collect_graph_impact(repo, head, base, [])
    blast = graph.get("blast_radius", {})

    next_step = (
        f"Address the top open risk: {open_risks[0]}" if open_risks
        else "No unresolved risks recorded; proceed with the stated intent."
    )

    return {
        "checkpoint_count": len(checkpoints),
        "latest_intent": intents[0] if intents else "",
        "decisions": _merge("decisions"),
        "rejected": _merge("rejected"),
        "failed": _merge("failed"),
        "open_risks": open_risks,
        "unresolved": _merge("unresolved"),
        "graph": {
            "available": graph.get("available", False),
            "symbols": blast.get("symbol_count", 0),
            "files": blast.get("file_count", 0),
            "max_depth": blast.get("max_distance", 0),
        },
        "next_step": next_step,
    }


def render_handoff(brief: dict) -> str:
    def _bullets(items):
        return "\n".join(f"- {i}" for i in items) if items else "- (none recorded)"

    g = brief["graph"]
    return "\n".join([
        "# Release Gate - Agent Handoff Brief",
        f"_Reconstructed from {brief['checkpoint_count']} Entire checkpoint(s) + Graph._",
        "",
        "## Intent (what the previous agent was doing)",
        brief["latest_intent"] or "- (none recorded)",
        "",
        "## Decisions", _bullets(brief["decisions"]),
        "",
        "## Rejected options", _bullets(brief["rejected"]),
        "",
        "## Failed attempts", _bullets(brief["failed"]),
        "",
        "## Open risks", _bullets(brief["open_risks"]),
        "",
        "## Unresolved work", _bullets(brief["unresolved"]),
        "",
        "## What a change here touches (Entire Graph)",
        (f"- {g['symbols']} symbols across {g['files']} files (max depth {g['max_depth']})"
         if g["available"] else "- graph evidence unavailable"),
        "",
        "## Suggested next step",
        f"- {brief['next_step']}",
    ])
