"""Databricks MLflow telemetry for ESI commit-gate runs."""

from __future__ import annotations

import os
from typing import Any

from entire_agent_codetriage.blast_radius import BlastRadius
from entire_agent_codetriage.env import load_dotenv_files


def log_esi_run(result: BlastRadius, extra: dict[str, Any] | None = None) -> bool:
    """Log ESI telemetry. Never raises — commit gating must not depend on MLflow."""
    load_dotenv_files()
    if os.environ.get("CODETRIAGE_DISABLE_MLFLOW") == "1":
        return False
    try:
        import mlflow
    except ImportError:
        return False

    tracking_uri = os.environ.get("MLFLOW_TRACKING_URI")
    if not tracking_uri:
        return False
    if tracking_uri.startswith("https://") and "databricks" in tracking_uri.lower():
        tracking_uri = "databricks"

    try:
        _utf8_stdio()
        os.environ.setdefault("MLFLOW_DISABLE_AGENT_HINT", "1")
        mlflow.set_tracking_uri(tracking_uri)
        experiment = os.environ.get("MLFLOW_EXPERIMENT_NAME", "/Shared/codetriage-esi")
        experiment_id = os.environ.get("MLFLOW_EXPERIMENT_ID")
        if experiment_id:
            mlflow.set_experiment(experiment_id=experiment_id)
        else:
            if tracking_uri == "databricks" and experiment and not experiment.startswith("/"):
                experiment = f"/Shared/{experiment.lstrip('/')}"
            mlflow.set_experiment(experiment)

        with mlflow.start_run(run_name="codetriage-commit-gate"):
            mlflow.log_params(
                {
                    "esi_level": str(result.esi_level),
                    "blocked": str(result.blocked).lower(),
                    "seed_count": str(len(result.seeds)),
                }
            )
            mlflow.log_metrics(
                {
                    "esi_level": float(result.esi_level),
                    "impacted_count": float(result.impacted_count),
                    "depth": float(result.depth),
                    "blocked": 1.0 if result.blocked else 0.0,
                }
            )
            if extra:
                safe_params = {str(k): str(v)[:250] for k, v in extra.items()}
                mlflow.log_params(safe_params)
        return True
    except Exception:
        return False


def _utf8_stdio() -> None:
    """MLflow prints emoji run URLs; Windows cp1252 consoles would otherwise abort the run."""
    import sys

    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if callable(reconfigure):
            try:
                reconfigure(encoding="utf-8", errors="replace")
            except Exception:
                pass
