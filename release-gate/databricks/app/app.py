"""Release Gate dashboard - a single-page Databricks App.

Judge-facing surface: latest PR risk scores, the Entire Graph + Checkpoint
evidence behind each score, and a before/after-Curveball panel. Reads the Gold
tables from the single serverless SQL warehouse.
"""
from __future__ import annotations

import json
import os

import streamlit as st

CATALOG = os.environ.get("RG_CATALOG", "release_gate")


@st.cache_resource
def _connection():
    from databricks import sql
    from databricks.sdk.core import Config

    cfg = Config()  # picks up app service-principal credentials
    return sql.connect(
        server_hostname=cfg.host.replace("https://", ""),
        http_path=os.environ["DATABRICKS_HTTP_PATH"],
        credentials_provider=lambda: cfg.authenticate,
    )


def _query(sql_text: str):
    with _connection().cursor() as cur:
        cur.execute(sql_text)
        cols = [c[0] for c in cur.description]
        return [dict(zip(cols, row)) for row in cur.fetchall()]


def main() -> None:
    st.set_page_config(page_title="Release Gate", layout="wide")
    st.title("Release Gate - PR release-risk intelligence")
    st.caption("Powered by Entire Checkpoints (why) + Entire Graph (what's affected) on Databricks.")

    try:
        scores = _query(
            f"SELECT pr_number, revision_sha, risk_score, gate, model, top_factors, "
            f"evidence_gaps, scored_at FROM {CATALOG}.gold.pr_risk_scores "
            f"ORDER BY scored_at DESC LIMIT 50"
        )
    except Exception as exc:  # noqa: BLE001
        st.error(f"Could not read scores: {exc}")
        st.info("Run the release_gate_pipeline job to populate gold.pr_risk_scores.")
        return

    if not scores:
        st.warning("No scored PRs yet.")
        return

    gate_emoji = {"PASS": "🟢", "REVIEW": "🟡", "BLOCK": "🔴"}
    cols = st.columns(4)
    cols[0].metric("PRs scored", len(scores))
    cols[1].metric("Latest gate", f"{gate_emoji.get(scores[0]['gate'],'')} {scores[0]['gate']}")
    cols[2].metric("Latest risk", f"{round(scores[0]['risk_score']*100)}/100")
    cols[3].metric("Model", scores[0]["model"])

    st.subheader("Scored pull requests")
    st.dataframe(
        [{"PR": s["pr_number"], "gate": s["gate"],
          "risk": round(s["risk_score"] * 100),
          "model": s["model"], "scored_at": str(s["scored_at"])}
         for s in scores],
        use_container_width=True,
    )

    st.subheader("Evidence drill-down")
    pick = st.selectbox("PR", [s["pr_number"] for s in scores])
    feats = _query(
        f"SELECT * FROM {CATALOG}.gold.pr_risk_features WHERE pr_number = '{pick}' LIMIT 1"
    )
    chosen = next(s for s in scores if s["pr_number"] == pick)
    left, right = st.columns(2)
    with left:
        st.markdown("**Entire Graph — structural blast radius**")
        if feats:
            f = feats[0]
            st.write({
                "impacted symbols": f.get("blast_radius_symbols"),
                "impacted files": f.get("blast_radius_files"),
                "max depth": f.get("max_impact_distance"),
                "impacted-symbol test coverage": f.get("impacted_symbol_test_coverage"),
            })
    with right:
        st.markdown("**Entire Checkpoints — intent & unresolved risk**")
        if feats:
            f = feats[0]
            st.write({
                "unresolved risks": f.get("unresolved_risk_count"),
                "checkpoints referenced": f.get("checkpoint_count"),
            })
        st.markdown("**Top risk drivers**")
        try:
            st.write(json.loads(chosen.get("top_factors") or "[]"))
        except json.JSONDecodeError:
            st.write(chosen.get("top_factors"))

    st.subheader("Noon Curveball — before / after")
    st.info("Populated after the Curveball response lands; compares pipeline "
            "behavior on the same PR across the pre- and post-Curveball model/rules.")


if __name__ == "__main__":
    main()
