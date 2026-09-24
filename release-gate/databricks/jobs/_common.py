"""Shared helpers for the Release Gate Databricks jobs.

Kept tiny on purpose. Each job stays runnable on the single serverless SQL
warehouse / a single job cluster within Free Edition limits.
"""
from __future__ import annotations

import os
import sys

# Ensure the repo root is importable so jobs can `import release_gate`.
_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
if _ROOT not in sys.path:
    sys.path.insert(0, _ROOT)


def get_param(name: str, default: str) -> str:
    """Read a Databricks job/widget param, falling back to env then default."""
    try:
        from pyspark.dbutils import DBUtils  # type: ignore
        from pyspark.sql import SparkSession

        dbutils = DBUtils(SparkSession.builder.getOrCreate())
        val = dbutils.widgets.get(name)
        if val:
            return val
    except Exception:  # noqa: BLE001 - not on Databricks, or widget absent
        pass
    return os.environ.get(name.upper(), default)


def config() -> dict:
    catalog = get_param("catalog", "release_gate")
    return {
        "catalog": catalog,
        "bronze": f"{catalog}.bronze",
        "silver": f"{catalog}.silver",
        "gold": f"{catalog}.gold",
    }


def ensure_schemas(spark, cfg: dict) -> None:
    spark.sql(f"CREATE CATALOG IF NOT EXISTS {cfg['catalog']}")
    for layer in ("bronze", "silver", "gold"):
        spark.sql(f"CREATE SCHEMA IF NOT EXISTS {cfg[layer]}")
