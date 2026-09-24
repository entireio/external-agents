from __future__ import annotations

from entire_agent_codetriage.blast_radius import compute_blast_radius
from entire_agent_codetriage.telemetry import log_esi_run


def test_telemetry_skips_without_tracking_uri(monkeypatch) -> None:
    monkeypatch.setattr("entire_agent_codetriage.telemetry.load_dotenv_files", lambda: None)
    monkeypatch.delenv("MLFLOW_TRACKING_URI", raising=False)
    monkeypatch.delenv("CODETRIAGE_DISABLE_MLFLOW", raising=False)
    result = compute_blast_radius(["a.py"], reverse_graph={})
    assert log_esi_run(result) is False


def test_telemetry_can_be_disabled(monkeypatch) -> None:
    monkeypatch.setenv("CODETRIAGE_DISABLE_MLFLOW", "1")
    monkeypatch.setenv("MLFLOW_TRACKING_URI", "databricks")
    result = compute_blast_radius(["a.py"], reverse_graph={})
    assert log_esi_run(result) is False
