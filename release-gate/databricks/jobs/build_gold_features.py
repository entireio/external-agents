"""Gold features: build one ``pr_risk_features`` row per PR revision.

``release_gate.features.build_gold_features`` is a pure function of Silver, so
new features slot in here without touching ingestion or scoring.
"""
from __future__ import annotations

import json

from _common import config, ensure_schemas
from pyspark.sql import SparkSession

from release_gate.features import build_gold_features
from release_gate.silver import to_silver

_KEY = "pr_number = s.pr_number AND revision_sha = s.revision_sha"


def run(spark: SparkSession) -> None:
    cfg = config()
    ensure_schemas(spark, cfg)
    bronze = f"{cfg['bronze']}.raw_evidence"
    table = f"{cfg['gold']}.pr_risk_features"

    rows = []
    for r in spark.table(bronze).select("payload_json").collect():
        bundle = json.loads(r["payload_json"])
        feats = build_gold_features(to_silver(bundle))
        feats["pr_number"] = str(feats.get("pr_number"))
        rows.append(feats)

    if not rows:
        print("[build_gold_features] no bronze rows")
        return

    df = spark.createDataFrame(rows)
    df.limit(0).write.format("delta").mode("ignore").saveAsTable(table)
    df.createOrReplaceTempView("_incoming_features")
    spark.sql(
        f"""MERGE INTO {table} t USING _incoming_features s ON t.{_KEY}
            WHEN MATCHED THEN UPDATE SET *
            WHEN NOT MATCHED THEN INSERT *"""
    )
    print(f"[build_gold_features] merged {len(rows)} feature rows into {table}")


if __name__ == "__main__":
    run(SparkSession.builder.getOrCreate())
