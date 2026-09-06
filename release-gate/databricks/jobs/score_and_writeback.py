"""Score PR revisions and write results to ``gold.pr_risk_scores``.

Scoring is decoupled from the model: today it calls the heuristic
``release_gate.scoring.score_features``; Phase 6 swaps in a Databricks Model
Serving endpoint by setting the ``scoring_endpoint`` param, keeping the heuristic
as the automatic fallback when the endpoint is unreachable. The Gold scores
table enforces ``risk_score BETWEEN 0 AND 1`` via a Delta CHECK constraint.
"""
from __future__ import annotations

import json

from _common import config, ensure_schemas, get_param
from pyspark.sql import SparkSession
from pyspark.sql import functions as F

from release_gate.scoring import score_features

_KEY = "pr_number = s.pr_number AND revision_sha = s.revision_sha"


def _score_row(feats: dict, endpoint: str) -> dict:
    if endpoint:
        try:
            return _score_via_endpoint(feats, endpoint)
        except Exception as exc:  # noqa: BLE001 - protect the demo, fall back
            print(f"[score] endpoint unreachable ({exc}); using heuristic fallback")
    return score_features(feats)


def _score_via_endpoint(feats: dict, endpoint: str) -> dict:
    import os

    import requests

    token = os.environ["DATABRICKS_TOKEN"]
    r = requests.post(
        endpoint,
        headers={"Authorization": f"Bearer {token}"},
        json={"dataframe_records": [feats]},
        timeout=30,
    )
    r.raise_for_status()
    pred = r.json()["predictions"][0]
    score = float(pred if not isinstance(pred, dict) else pred.get("risk_score"))
    gate = "PASS" if score < 0.34 else "REVIEW" if score < 0.67 else "BLOCK"
    return {
        "pr_number": feats.get("pr_number"),
        "revision_sha": feats.get("revision_sha"),
        "repo": feats.get("repo"),
        "risk_score": round(score, 4),
        "gate": gate,
        "model": "served-model",
        "top_factors": [],
        "evidence_gaps": [],
    }


def run(spark: SparkSession) -> None:
    cfg = config()
    ensure_schemas(spark, cfg)
    features_tbl = f"{cfg['gold']}.pr_risk_features"
    scores_tbl = f"{cfg['gold']}.pr_risk_scores"
    endpoint = get_param("scoring_endpoint", "")

    spark.sql(
        f"""CREATE TABLE IF NOT EXISTS {scores_tbl} (
              pr_number STRING, revision_sha STRING, repo STRING,
              risk_score DOUBLE, gate STRING, model STRING,
              top_factors STRING, evidence_gaps STRING, scored_at TIMESTAMP
            ) USING DELTA"""
    )
    # Enforce the score range as a table invariant (idempotent).
    try:
        spark.sql(
            f"ALTER TABLE {scores_tbl} ADD CONSTRAINT risk_range "
            f"CHECK (risk_score BETWEEN 0 AND 1)"
        )
    except Exception:  # noqa: BLE001 - constraint already exists
        pass

    rows = []
    for r in spark.table(features_tbl).collect():
        feats = r.asDict()
        s = _score_row(feats, endpoint)
        s["pr_number"] = str(s.get("pr_number"))
        s["top_factors"] = json.dumps(s.get("top_factors", []))
        s["evidence_gaps"] = json.dumps(s.get("evidence_gaps", []))
        rows.append(s)

    if not rows:
        print("[score_and_writeback] no feature rows")
        return

    df = spark.createDataFrame(rows).withColumn("scored_at", F.current_timestamp())
    df.createOrReplaceTempView("_incoming_scores")
    spark.sql(
        f"""MERGE INTO {scores_tbl} t USING _incoming_scores s ON t.{_KEY}
            WHEN MATCHED THEN UPDATE SET *
            WHEN NOT MATCHED THEN INSERT *"""
    )
    spark.sql(f"OPTIMIZE {scores_tbl} ZORDER BY (pr_number)")
    print(f"[score_and_writeback] scored {len(rows)} PR revisions -> {scores_tbl}")


if __name__ == "__main__":
    run(SparkSession.builder.getOrCreate())
