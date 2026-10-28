"""Self-contained interactive HTML dashboard for a scored PR.

Renders a single modern HTML page (no server, no build step) with a risk gauge,
gate badge, top risk drivers, an Entire-Graph blast-radius network, evidence
cards, unresolved-risk list, and the AI review. Uses vis-network from a CDN for
the graph; everything else is inline so it opens offline for the demo.
"""
from __future__ import annotations

import html
import json
from typing import Any

_GATE_COLOR = {"PASS": "#22c55e", "REVIEW": "#eab308", "BLOCK": "#ef4444"}


def _network_data(bundle: dict) -> tuple[list, list]:
    gi = bundle.get("graph_impact", {}) or {}
    nodes = [{"id": 0, "label": "PR change", "color": "#6366f1", "size": 28,
              "font": {"color": "#fff", "size": 16}}]
    edges = []
    palette = {0: "#f472b6", 1: "#f59e0b", 2: "#38bdf8", 3: "#a3a3a3"}
    for i, s in enumerate(gi.get("impacted_symbols", [])[:40], start=1):
        d = int(s.get("distance", 1) or 1)
        nodes.append({
            "id": i, "label": s.get("symbol") or "?",
            "title": f"{s.get('file')} · {s.get('kind')} · {s.get('relationship')} · d{d}",
            "color": palette.get(d, "#a3a3a3"), "size": max(10, 22 - d * 3),
            "font": {"color": "#cbd5e1", "size": 11},
        })
        edges.append({"from": 0, "to": i, "color": {"color": palette.get(d, "#a3a3a3"),
                      "opacity": 0.5}, "length": 90 + d * 60})
    return nodes, edges


def render_dashboard(bundle: dict[str, Any], features: dict[str, Any],
                     score: dict[str, Any], narrative: str | None = None) -> str:
    pr = bundle.get("pr", {}) or {}
    gate = score.get("gate", "REVIEW")
    color = _GATE_COLOR.get(gate, "#eab308")
    risk = int(round(float(score.get("risk_score", 0)) * 100))
    nodes, edges = _network_data(bundle)

    drivers = "".join(
        f"<div class='bar'><span>{html.escape(f['label'])}</span>"
        f"<div class='track'><div class='fill' style='width:{min(100, int(f['contribution']*100/0.35))}%;"
        f"background:{color}'></div></div><b>+{f['contribution']:.2f}</b></div>"
        for f in score.get("top_factors", [])
    ) or "<p class='muted'>No material risk drivers.</p>"

    risks = []
    for c in (bundle.get("checkpoint_signals", {}) or {}).get("checkpoints", []) or []:
        risks += c.get("unresolved_risks", []) or []
    risk_items = "".join(f"<li>{html.escape(str(r))}</li>" for r in risks[:8]) or \
        "<li class='muted'>None recorded.</li>"

    ingest = bundle.get("ingest", {}) or {}
    partial_banner = ""
    if ingest.get("partial"):
        partial_banner = (
            f"<div class='banner'>⚠ PARTIAL / INCOMPLETE evidence — provisional, "
            f"not authoritative. formats={ingest.get('formats')} · "
            f"unknown_events={ingest.get('unknown_events', 0)} · "
            f"parse_errors={ingest.get('parse_errors', 0)}</div>")

    ai_panel = ""
    if narrative:
        ai_panel = (
            "<section class='card ai'><h3>🤖 AI review "
            "<span class='chip'>Databricks Llama 3.3 70B</span></h3>"
            f"<pre>{html.escape(narrative)}</pre></section>")

    return _TEMPLATE.format(
        color=color, gate=gate, risk=risk, model=html.escape(str(score.get("model"))),
        repo=html.escape(str(pr.get("repo") or "")), number=pr.get("number") or "",
        sha=html.escape(str(pr.get("revision_sha") or "")[:8]),
        blast=features.get("blast_radius_symbols", 0),
        bfiles=features.get("blast_radius_files", 0),
        depth=features.get("max_impact_distance", 0),
        unresolved=features.get("unresolved_risk_count", 0),
        checkpoints=features.get("checkpoint_count", 0),
        tpass=features.get("tests_passed", 0), tfail=features.get("tests_failed", 0),
        cov=int(round(float(features.get("impacted_symbol_test_coverage", 0)) * 100)),
        churn=features.get("churn_total", 0), cfiles=features.get("files_changed", 0),
        drivers=drivers, risks=risk_items, partial=partial_banner, ai=ai_panel,
        nodes=json.dumps(nodes), edges=json.dumps(edges),
        dash=risk * 2.51, gaugecolor=color,
    )


_TEMPLATE = """<!doctype html><html lang=en><head><meta charset=utf-8>
<meta name=viewport content="width=device-width,initial-scale=1">
<title>Release Gate</title>
<script src="https://unpkg.com/vis-network/standalone/umd/vis-network.min.js"></script>
<style>
:root{{--bg:#0b1020;--card:#151b2e;--muted:#8b98b8;--line:#24304d}}
*{{box-sizing:border-box}}body{{margin:0;font:15px/1.5 system-ui,Segoe UI,Roboto,sans-serif;
background:radial-gradient(1200px 600px at 20% -10%,#1b2545,#0b1020);color:#e6ecff}}
.wrap{{max-width:1160px;margin:0 auto;padding:28px}}
.top{{display:flex;align-items:center;gap:20px;flex-wrap:wrap}}
h1{{font-size:22px;margin:0}}.sub{{color:var(--muted);font-size:13px}}
.badge{{padding:6px 14px;border-radius:999px;font-weight:700;color:#0b1020}}
.grid{{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px;margin-top:22px}}
.card{{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:18px;
box-shadow:0 10px 30px rgba(0,0,0,.25)}}
.card h3{{margin:0 0 12px;font-size:15px}}.muted{{color:var(--muted)}}
.metric{{font-size:30px;font-weight:800}}.metric small{{font-size:13px;color:var(--muted);font-weight:500}}
.gauge{{position:relative;width:150px;height:150px}}
.gauge svg{{transform:rotate(-90deg)}}
.gauge .val{{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center}}
.gauge .val b{{font-size:34px}}.gauge .val span{{color:var(--muted);font-size:12px}}
.bar{{display:grid;grid-template-columns:1fr 120px 44px;align-items:center;gap:10px;margin:8px 0;font-size:13px}}
.track{{height:8px;background:#0e1526;border-radius:6px;overflow:hidden}}.fill{{height:100%}}
#net{{height:340px;border-radius:12px;background:#0e1526;border:1px solid var(--line)}}
ul{{margin:0;padding-left:18px}}li{{margin:4px 0}}
.banner{{background:#3a2a12;border:1px solid #7c5b1e;color:#ffd479;padding:10px 14px;border-radius:12px;margin-top:16px;font-size:13px}}
.ai pre{{white-space:pre-wrap;background:#0e1526;border-radius:10px;padding:14px;margin:0;font:14px/1.6 ui-monospace,monospace;color:#c7f9cc}}
.chip{{background:#0e1526;border:1px solid var(--line);color:var(--muted);font-size:11px;padding:2px 8px;border-radius:999px;margin-left:6px}}
.foot{{color:var(--muted);font-size:12px;margin-top:22px;text-align:center}}
.two{{display:grid;grid-template-columns:1.3fr 1fr;gap:16px}}@media(max-width:820px){{.two{{grid-template-columns:1fr}}}}
</style></head><body><div class=wrap>
<div class=top>
  <div class=gauge><svg width=150 height=150>
    <circle cx=75 cy=75 r=64 fill=none stroke="#1c2540" stroke-width=14></circle>
    <circle cx=75 cy=75 r=64 fill=none stroke="{gaugecolor}" stroke-width=14
      stroke-linecap=round stroke-dasharray="{dash} 402"></circle></svg>
    <div class=val><b>{risk}</b><span>risk / 100</span></div></div>
  <div style=flex:1>
    <h1>Release Gate <span class=badge style="background:{color}">{gate}</span></h1>
    <div class=sub>{repo} · PR #{number} · <code>{sha}</code> · model <code>{model}</code></div>
    <div class=sub>Entire Checkpoints (why) + Entire Graph (what's affected), scored on Databricks.</div>
  </div>
</div>
{partial}
<div class=grid>
  <div class=card><h3>Entire Graph — blast radius</h3>
    <div class=metric>{blast}<small> symbols</small></div>
    <div class=muted>{bfiles} files · max depth {depth}</div></div>
  <div class=card><h3>Entire Checkpoints — risk</h3>
    <div class=metric>{unresolved}<small> unresolved</small></div>
    <div class=muted>across {checkpoints} checkpoints</div></div>
  <div class=card><h3>Tests</h3>
    <div class=metric>{tpass}<small> passed</small></div>
    <div class=muted>{tfail} failed · impacted coverage {cov}%</div></div>
  <div class=card><h3>Churn</h3>
    <div class=metric>{churn}<small> lines</small></div>
    <div class=muted>across {cfiles} files</div></div>
</div>
<div class=two style=margin-top:16px>
  <div class=card><h3>Impact network (Entire Graph)</h3><div id=net></div></div>
  <div>
    <div class=card><h3>Top risk drivers</h3>{drivers}</div>
    <div class=card style=margin-top:16px><h3>Open questions from checkpoints</h3><ul>{risks}</ul></div>
  </div>
</div>
{ai}
<div class=foot>Powered by <b>Entire</b> (Checkpoints + Graph) and <b>Databricks</b> (Delta · MLflow · Model Serving · Foundation Models)</div>
</div>
<script>
const nodes=new vis.DataSet({nodes});const edges=new vis.DataSet({edges});
new vis.Network(document.getElementById('net'),{{nodes,edges}},{{
 physics:{{stabilization:true,barnesHut:{{springLength:120}}}},
 nodes:{{shape:'dot',borderWidth:0}},interaction:{{hover:true}}}});
</script></body></html>"""
