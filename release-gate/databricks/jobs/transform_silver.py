"""Silver transform: parse raw bronze payloads into typed Silver tables.

Uses ``release_gate.silver.to_silver`` (the same pure function the local runner
and unit tests use). MERGE keyed on (pr_number, revision_sha) so re-scoring a PR
revision never double-counts.
"""
from __future__ import annotations

import json

from _common import config, ensure_schemas
from pyspark.sql import SparkSession

from release_gate.silver import to_silver

_KEY = "pr_number = s.pr_number AND revision_sha = s.revision_sha"


def _merge(spark, table: str, rows: list[dict]) -> None:
    if not rows:
        return
    df = spark.createDataFrame(rows)
    df.createOrReplaceTempView("_incoming")
    cols = ", ".join(df.columns)
    # Replace this PR revision's rows atomically (delete-then-insert semantics).
    spark.sql(
        f"""MERGE INTO {table} t USING _incoming s ON t.{_KEY}
            WHEN MATCHED THEN UPDATE SET *
            WHEN NOT MATCHED THEN INSERT ({cols}) VALUES ({cols})"""
    )


def run(spark: SparkSession) -> None:
    cfg = config()
    ensure_schemas(spark, cfg)
    bronze = f"{cfg['bronze']}.raw_evidence"

    payloads = [r["payload_json"] for r in spark.table(bronze).select("payload_json").collect()]
    for raw in payloads:
        bundle = json.loads(raw)
        silver = to_silver(bundle)
        pr = silver["pr"]

        # pr dimension
        spark.sql(f"CREATE TABLE IF NOT EXISTS {cfg['silver']}.pr (pr_number STRING) USING DELTA")
        for name, rows in (
            ("graph_impact", silver["graph_impact"]),
            ("checkpoint_signals", silver["checkpoint_signals"]),
            ("test_results", silver["test_results"]),
        ):
            table = f"{cfg['silver']}.{name}"
            if rows:
                spark.createDataFrame(rows).limit(0).write.format("delta").mode(
                    "ignore"
                ).saveAsTable(table)
                _merge(spark, table, rows)
        print(f"[transform_silver] PR {pr['pr_number']}@{pr['revision_sha']} -> silver")


if __name__ == "__main__":
    run(SparkSession.builder.getOrCreate())
