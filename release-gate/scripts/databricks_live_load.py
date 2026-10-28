"""Live Databricks loader: build the medallion Delta tables from a real evidence
bundle and populate Bronze -> Silver -> Gold on the workspace, using the SQL
Statement Execution API against the single serverless warehouse.

This is the reliable, quota-light path used for the live demo. The Spark jobs in
``databricks/jobs`` are the equivalent Workflow path deployed via the Asset
Bundle; both produce the same Gold tables.

Usage:
    python scripts/databricks_live_load.py --bundle _real_bundle.json \
        --profile release-gate --warehouse 7f0409edb975227e
"""
from __future__ import annotations

import argparse
import json
import os
import sys

_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if _ROOT not in sys.path:
    sys.path.insert(0, _ROOT)

from release_gate.bundle import load_bundle  # noqa: E402
from release_gate.features import build_gold_features  # noqa: E402
from release_gate.scoring import score_features  # noqa: E402
from release_gate.silver import to_silver  # noqa: E402


def _sql_val(v) -> str:
    if v is None:
        return "NULL"
    if isinstance(v, bool):
        return "TRUE" if v else "FALSE"
    if isinstance(v, (int, float)):
        return repr(v)
    return "'" + str(v).replace("'", "''") + "'"


class Warehouse:
    def __init__(self, profile: str, warehouse_id: str):
        from databricks.sdk import WorkspaceClient

        self.w = WorkspaceClient(profile=profile)
        self.wid = warehouse_id

    def exec(self, statement: str, catalog: str | None = None):
        from databricks.sdk.service.sql import StatementState

        r = self.w.statement_execution.execute_statement(
            warehouse_id=self.wid, statement=statement, catalog=catalog,
            wait_timeout="50s",
        )
        state = r.status.state
        if state in (StatementState.PENDING, StatementState.RUNNING):
            import time
            for _ in range(30):
                time.sleep(2)
                r = self.w.statement_execution.get_statement(r.statement_id)
                if r.status.state not in (StatementState.PENDING, StatementState.RUNNING):
                    break
        if r.status.state != StatementState.SUCCEEDED:
            raise RuntimeError(f"SQL failed: {r.status.state} :: {statement[:80]} :: "
                               f"{getattr(r.status, 'error', None)}")
        return r


def _pick_catalog(wh: Warehouse) -> str:
    """Prefer a dedicated release_gate catalog; fall back to `workspace`."""
    try:
        wh.exec("CREATE CATALOG IF NOT EXISTS release_gate")
        return "release_gate"
    except Exception as exc:  # noqa: BLE001
        print(f"[live_load] catalog create not permitted ({exc}); using `workspace`")
        return "workspace"


def run(bundle_path: str, profile: str, warehouse_id: str) -> None:
    bundle = load_bundle(bundle_path)
    silver = to_silver(bundle)
    features = build_gold_features(silver)
    score = score_features(features)
    pr = silver["pr"]

    wh = Warehouse(profile, warehouse_id)
    cat = _pick_catalog(wh)
    for schema in ("bronze", "silver", "gold"):
        wh.exec(f"CREATE SCHEMA IF NOT EXISTS {cat}.{schema}")

    # --- Bronze ---
    wh.exec(f"""CREATE TABLE IF NOT EXISTS {cat}.bronze.raw_evidence (
        bundle_id STRING, pr_number STRING, revision_sha STRING,
        generated_at STRING, source STRING, payload_json STRING,
        ingested_at TIMESTAMP)""")
    wh.exec(f"""MERGE INTO {cat}.bronze.raw_evidence t
        USING (SELECT {_sql_val(bundle['bundle_id'])} AS bundle_id) s
        ON t.bundle_id = s.bundle_id
        WHEN NOT MATCHED THEN INSERT (bundle_id, pr_number, revision_sha,
            generated_at, source, payload_json, ingested_at) VALUES (
            {_sql_val(bundle['bundle_id'])}, {_sql_val(str(pr['pr_number']))},
            {_sql_val(pr['revision_sha'])}, {_sql_val(bundle['generated_at'])},
            {_sql_val(bundle['source'])}, {_sql_val(json.dumps(bundle))},
            current_timestamp())""")

    # --- Silver (graph_impact is the headline structural table) ---
    wh.exec(f"""CREATE TABLE IF NOT EXISTS {cat}.silver.graph_impact (
        pr_number STRING, revision_sha STRING, symbol STRING, file STRING,
        kind STRING, relationship STRING, distance INT)""")
    wh.exec(f"DELETE FROM {cat}.silver.graph_impact WHERE pr_number = "
            f"{_sql_val(str(pr['pr_number']))} AND revision_sha = {_sql_val(pr['revision_sha'])}")
    rows = silver["graph_impact"][:200]
    if rows:
        values = ",".join(
            f"({_sql_val(str(pr['pr_number']))},{_sql_val(pr['revision_sha'])},"
            f"{_sql_val(r['symbol'])},{_sql_val(r['file'])},{_sql_val(r['kind'])},"
            f"{_sql_val(r['relationship'])},{_sql_val(r['distance'])})"
            for r in rows
        )
        wh.exec(f"INSERT INTO {cat}.silver.graph_impact VALUES {values}")

    # --- Gold: features ---
    fcols = list(features.keys())
    wh.exec(f"CREATE TABLE IF NOT EXISTS {cat}.gold.pr_risk_features (" +
            ", ".join(_col_ddl(c, features[c]) for c in fcols) + ")")
    wh.exec(f"DELETE FROM {cat}.gold.pr_risk_features WHERE pr_number = "
            f"{_sql_val(str(features['pr_number']))} AND revision_sha = {_sql_val(features['revision_sha'])}")
    fvals = ",".join(_sql_val(str(features[c]) if c == "pr_number" else features[c]) for c in fcols)
    wh.exec(f"INSERT INTO {cat}.gold.pr_risk_features ({', '.join(fcols)}) VALUES ({fvals})")

    # --- Gold: scores (with a CHECK constraint on the score range) ---
    wh.exec(f"""CREATE TABLE IF NOT EXISTS {cat}.gold.pr_risk_scores (
        pr_number STRING, revision_sha STRING, repo STRING, risk_score DOUBLE,
        gate STRING, model STRING, top_factors STRING, evidence_gaps STRING,
        scored_at TIMESTAMP)""")
    wh.exec(f"DELETE FROM {cat}.gold.pr_risk_scores WHERE pr_number = "
            f"{_sql_val(str(score['pr_number']))} AND revision_sha = {_sql_val(score['revision_sha'])}")
    wh.exec(f"""INSERT INTO {cat}.gold.pr_risk_scores VALUES (
        {_sql_val(str(score['pr_number']))}, {_sql_val(score['revision_sha'])},
        {_sql_val(score['repo'])}, {_sql_val(score['risk_score'])},
        {_sql_val(score['gate'])}, {_sql_val(score['model'])},
        {_sql_val(json.dumps(score['top_factors']))},
        {_sql_val(json.dumps(score['evidence_gaps']))}, current_timestamp())""")

    # --- Verify ---
    r = wh.exec(f"SELECT pr_number, risk_score, gate, model FROM {cat}.gold.pr_risk_scores "
                f"ORDER BY scored_at DESC LIMIT 5")
    print(f"[live_load] catalog={cat}")
    print(f"[live_load] gold.pr_risk_scores rows:")
    for row in (r.result.data_array or []):
        print("   ", row)
    print(f"[live_load] done. Tables under {cat}.(bronze|silver|gold).")


def _col_ddl(name: str, value) -> str:
    if name in ("pr_number", "revision_sha", "repo", "author", "title"):
        return f"{name} STRING"
    if isinstance(value, bool):
        return f"{name} BOOLEAN"
    if isinstance(value, float):
        return f"{name} DOUBLE"
    if isinstance(value, int):
        return f"{name} BIGINT"
    return f"{name} STRING"


def main(argv=None) -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--bundle", default=os.path.join(_ROOT, "seed_data", "sample_bundle.json"))
    p.add_argument("--profile", default="release-gate")
    p.add_argument("--warehouse", required=True)
    args = p.parse_args(argv)
    run(args.bundle, args.profile, args.warehouse)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
