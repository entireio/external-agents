"""Render a templated Release Gate PR comment from score + evidence.

The template is data-driven (extension seam d): the Noon Curveball can add rows
or sections without changing the writeback callers.
"""
from __future__ import annotations

from typing import Any

_GATE_BADGE = {
    "PASS": "\U0001F7E2 PASS",
    "REVIEW": "\U0001F7E1 REVIEW",
    "BLOCK": "\U0001F534 BLOCK",
}

MARKER = "<!-- release-gate -->"


def render_comment(
    bundle: dict[str, Any],
    features: dict[str, Any],
    score: dict[str, Any],
) -> str:
    pr = bundle.get("pr", {}) or {}
    gate = score.get("gate", "REVIEW")
    badge = _GATE_BADGE.get(gate, gate)
    risk_pct = round(float(score.get("risk_score", 0.0)) * 100)

    lines: list[str] = [
        MARKER,
        f"## Release Gate: {badge} - risk {risk_pct}/100",
        "",
        f"Scored PR #{pr.get('number')} @ `{str(pr.get('revision_sha') or '')[:8]}` "
        f"by model `{score.get('model')}`.",
        "",
        "### Top risk drivers",
    ]

    if score.get("top_factors"):
        lines.append("| Driver | Contribution |")
        lines.append("| --- | --- |")
        for f in score["top_factors"]:
            lines.append(f"| {f['label']} | +{f['contribution']:.2f} |")
    else:
        lines.append("_No material risk drivers detected._")

    lines += [
        "",
        "### Evidence",
        f"- **Blast radius (Entire Graph):** {features.get('blast_radius_symbols', 0)} "
        f"symbols across {features.get('blast_radius_files', 0)} files "
        f"(max depth {features.get('max_impact_distance', 0)})",
        f"- **Unresolved risks (Entire Checkpoints):** "
        f"{features.get('unresolved_risk_count', 0)} across "
        f"{features.get('checkpoint_count', 0)} checkpoints",
        f"- **Tests:** {features.get('tests_passed', 0)} passed / "
        f"{features.get('tests_failed', 0)} failed / "
        f"{features.get('tests_total', 0)} total; impacted-symbol coverage "
        f"{round(float(features.get('impacted_symbol_test_coverage', 0)) * 100)}%",
        f"- **Churn:** {features.get('churn_total', 0)} lines across "
        f"{features.get('files_changed', 0)} files",
    ]

    risks = _unresolved_risk_excerpts(bundle)
    if risks:
        lines += ["", "### Open questions from checkpoints"]
        lines += [f"- {r}" for r in risks[:5]]

    if score.get("evidence_gaps"):
        gaps = ", ".join(score["evidence_gaps"])
        lines += [
            "",
            f"> :warning: Degraded evidence: {gaps} unavailable. "
            "Score computed on available signals only.",
        ]

    return "\n".join(lines)


def _unresolved_risk_excerpts(bundle: dict[str, Any]) -> list[str]:
    out: list[str] = []
    for c in (bundle.get("checkpoint_signals", {}) or {}).get("checkpoints", []) or []:
        for risk in c.get("unresolved_risks") or []:
            out.append(str(risk))
    return out
