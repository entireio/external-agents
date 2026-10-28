"""Shared model feature contract.

Training (``databricks/ml/train_model.py``) and serving/scoring must agree on
the exact feature ordering, so both import ``FEATURE_COLUMNS`` from here.
"""
from __future__ import annotations

from typing import Any

FEATURE_COLUMNS = [
    "blast_radius_symbols",
    "blast_radius_files",
    "max_impact_distance",
    "unresolved_risk_count",
    "checkpoint_count",
    "rejected_option_count",
    "assumption_count",
    "additions",
    "deletions",
    "churn_total",
    "files_changed",
    "tests_total",
    "tests_passed",
    "tests_failed",
    "tests_skipped",
    "test_failure_rate",
    "impacted_symbols_tested",
    "impacted_symbol_test_coverage",
]

LABEL_COLUMN = "label_incident"


def to_vector(features: dict[str, Any]) -> list[float]:
    """Project a Gold feature row onto the ordered model feature vector."""
    return [float(features.get(col, 0) or 0) for col in FEATURE_COLUMNS]
