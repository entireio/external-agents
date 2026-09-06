"""AI risk narrative via a Databricks-hosted foundation model.

Turns the structured evidence into a concise, human-readable review: what makes
the PR risky and what a reviewer should check. Uses a serverless hosted
foundation model (no GPU on our side). Fails soft: returns ``None`` if the model
is unreachable, so the gate always still works.
"""
from __future__ import annotations

from typing import Any

DEFAULT_MODEL = "databricks-meta-llama-3-3-70b-instruct"

_SYSTEM = (
    "You are Release Gate, a senior release-risk reviewer. The gate decision "
    "(PASS/REVIEW/BLOCK) is FINAL and authoritative - explain it, never override "
    "it. REVIEW means a human must look before merge; BLOCK means do not merge. "
    "Given the evidence, write a crisp review in EXACTLY this form:\n"
    "Verdict: <one sentence consistent with the gate>\n"
    "Why: <one sentence citing the strongest 1-2 signals>\n"
    "Check: <two short imperative checks a reviewer should do>\n"
    "Be specific, no fluff, no markdown headers."
)


def _prompt(features: dict, score: dict) -> str:
    risks = features.get("_risk_texts") or []
    return (
        f"Gate={score.get('gate')} risk={score.get('risk_score')} model={score.get('model')}.\n"
        f"Blast radius (Entire Graph): {features.get('blast_radius_symbols', 0)} symbols "
        f"across {features.get('blast_radius_files', 0)} files, max depth "
        f"{features.get('max_impact_distance', 0)}.\n"
        f"Unresolved risks (Entire Checkpoints): {features.get('unresolved_risk_count', 0)}"
        + (f" -> {risks[:4]}" if risks else "") + ".\n"
        f"Tests: {features.get('tests_passed', 0)} passed / {features.get('tests_failed', 0)} "
        f"failed; impacted-symbol coverage {features.get('impacted_symbol_test_coverage', 0)}.\n"
        f"Churn: {features.get('churn_total', 0)} lines across {features.get('files_changed', 0)} files."
    )


def risk_narrative(features: dict[str, Any], score: dict[str, Any],
                   profile: str = "release-gate", model: str = DEFAULT_MODEL,
                   risk_texts: list[str] | None = None) -> str | None:
    try:
        from databricks.sdk import WorkspaceClient

        feats = dict(features)
        if risk_texts:
            feats["_risk_texts"] = risk_texts
        w = WorkspaceClient(profile=profile)
        resp = w.api_client.do(
            "POST", f"/serving-endpoints/{model}/invocations",
            body={
                "messages": [
                    {"role": "system", "content": _SYSTEM},
                    {"role": "user", "content": _prompt(feats, score)},
                ],
                "max_tokens": 220, "temperature": 0.2,
            },
        )
        return resp["choices"][0]["message"]["content"].strip()
    except Exception:  # noqa: BLE001 - narrative is optional; gate must still work
        return None
