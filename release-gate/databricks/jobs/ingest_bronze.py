"""Bronze ingestion: land a raw evidence bundle in ``bronze.raw_evidence``.

Append-only, schema-on-write, idempotent via MERGE on ``bundle_id``. The raw
JSON payload is preserved verbatim for audit; typed parsing happens in Silver.

Run on Databricks:
    %run or job task with params: catalog, bundle_path (a Unity Catalog Volume
    path or workspace file), e.g. /Volumes/release_gate/bronze/inbox/xxx.json
"""
from __future__ import annotations

import datetime as _dt
import json

from _common import config, ensure_schemas, get_param
from pyspark.sql import SparkSession
from pyspark.sql import functions as F
from pyspark.sql.types import (StringType, StructField, StructType,
                               TimestampType)

_SCHEMA = StructType([
    StructField("bundle_id", StringType(), False),
    StructField("pr_number", StringType(), True),
    StructField("revision_sha", StringType(), True),
    StructField("generated_at", StringType(), True),
    StructField("source", StringType(), True),
    StructField("payload_json", StringType(), False),
    StructField("ingested_at", TimestampType(), False),
])


def run(spark: SparkSession) -> None:
    cfg = config()
    ensure_schemas(spark, cfg)
    table = f"{cfg['bronze']}.raw_evidence"

    bundle_path = get_param("bundle_path", "")
    if not bundle_path:
        raise SystemExit("bundle_path param required")

    raw = "".join(spark.read.text(bundle_path).toPandas()["value"].tolist())
    bundle = json.loads(raw)
    pr = bundle.get("pr", {})

    row = [(
        bundle["bundle_id"],
        str(pr.get("number")),
        pr.get("revision_sha"),
        bundle.get("generated_at"),
        bundle.get("source"),
        json.dumps(bundle),
        _dt.datetime.utcnow(),
    )]
    df = spark.createDataFrame(row, schema=_SCHEMA)

    spark.sql(
        f"""CREATE TABLE IF NOT EXISTS {table} (
              bundle_id STRING, pr_number STRING, revision_sha STRING,
              generated_at STRING, source STRING, payload_json STRING,
              ingested_at TIMESTAMP
            ) USING DELTA"""
    )
    df.createOrReplaceTempView("_incoming_bronze")
    spark.sql(
        f"""MERGE INTO {table} t USING _incoming_bronze s
            ON t.bundle_id = s.bundle_id
            WHEN NOT MATCHED THEN INSERT *"""
    )
    print(f"[ingest_bronze] merged bundle {bundle['bundle_id']} into {table}")


if __name__ == "__main__":
    run(SparkSession.builder.getOrCreate())
