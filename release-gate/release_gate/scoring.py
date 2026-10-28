"""Gold features -> release-risk score and gate decision.

Phase-2 scorer is an explainable heuristic. It is deliberately *decoupled* from
the model (extension seam c): Phase 6 swaps this for a served MLflow model by
replacing ``score_features`` with a call to the endpoint, keeping the same
input/output contract, and this heuristic remains the automatic fallback if the
endpoint is unreachable.
"""
from __future__ import annotations

from typing import Any

MODEL_NAME = "heuristic-v1"

# Normalisation caps: the value at which a factor is considered "maxed out".
CAPS = {
    "blast": 20.0,      # impacted symbols
    "distance": 4.0,    # graph hops
    "risk": 5.0,        # unresolved checkpoint risks
    "churn": 400.0,     # lines added + deleted
}

# Weights sum to 1.0. Checkpoint risk and graph blast radius dominate on
# purpose: they are the two signals only Entire can provide.
WEIGHTS = {
    "unresolved_risk": 0.25,
    "blast_radius": 0.22,
    "test_failure": 0.18,
    "churn": 0.13,
    "coverage_gap": 0.12,
    "impact_distance": 0.10,
}

# Gate thresholds on the final risk score.
GATE_PASS = 0.34
GATE_REVIEW = 0.67

FACTOR_LABELS = {
    "unresolved_risk": "Unresolved risks in checkpoints (Entire Checkpoints)",
    "blast_radius": "Structural blast radius (Entire Graph)",
    "test_failure": "Failing tests",
    "churn": "Code churn",
    "coverage_gap": "Impacted symbols without test coverage",
    "impact_distance": "Depth of structural impact",
}


def _cap(value: float, cap: float) -> float:
    if cap <= 0:
        return 0.0
    return max(0.0, min(value / cap, 1.0))


def score_features(features: dict[str, Any]) -> dict[str, Any]:
    factors = {
        "unresolved_risk": _cap(features.get("unresolved_risk_count", 0), CAPS["risk"]),
        "blast_radius": _cap(features.get("blast_radius_symbols", 0), CAPS["blast"]),
        "test_failure": float(features.get("test_failure_rate", 0.0) or 0.0),
        "churn": _cap(features.get("churn_total", 0), CAPS["churn"]),
        "coverage_gap": 1.0 - float(features.get("impacted_symbol_test_coverage", 1.0) or 1.0),
        "impact_distance": _cap(features.get("max_impact_distance", 0), CAPS["distance"]),
    }

    contributions = {k: WEIGHTS[k] * factors[k] for k in WEIGHTS}
    risk_score = round(min(sum(contributions.values()), 1.0), 4)

    if risk_score < GATE_PASS:
        gate = "PASS"
    elif risk_score < GATE_REVIEW:
        gate = "REVIEW"
    else:
        gate = "BLOCK"

    top_factors = [
        {
            "name": k,
            "label": FACTOR_LABELS[k],
            "contribution": round(contributions[k], 4),
            "raw": round(factors[k], 4),
        }
        for k in sorted(contributions, key=contributions.get, reverse=True)
        if contributions[k] > 0
    ][:3]

    evidence_gaps = [
        name
        for name, ok in (
            ("graph", features.get("graph_available", False)),
            ("checkpoints", features.get("checkpoints_available", False)),
            ("tests", features.get("tests_available", False)),
        )
        if not ok
    ]

    return {
        "pr_number": features.get("pr_number"),
        "revision_sha": features.get("revision_sha"),
        "repo": features.get("repo"),
        "risk_score": risk_score,
        "gate": gate,
        "model": MODEL_NAME,
        "top_factors": top_factors,
        "evidence_gaps": evidence_gaps,
    }
