import os

from release_gate.dashboard import render_dashboard
from release_gate.events import events_to_bundle, parse_events
from release_gate.features import build_gold_features
from release_gate.scoring import score_features

_SEED = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "seed_data")


def test_render_dashboard_is_self_contained_html():
    with open(os.path.join(_SEED, "track-3-agent-session.jsonl"), "r", encoding="utf-8") as fh:
        bundle = events_to_bundle(parse_events(fh), pr_number=5, pr_repo="o/r")
    features = build_gold_features_wrapper(bundle)
    score = score_features(features)
    html = render_dashboard(bundle, features, score, narrative="Verdict: ok\nWhy: x\nCheck: y")
    assert "<!doctype html>" in html.lower()
    assert "Release Gate" in html and "Databricks" in html
    assert "vis-network" in html  # impact graph library
    assert "AI review" in html    # narrative panel rendered


def build_gold_features_wrapper(bundle):
    from release_gate.silver import to_silver
    return build_gold_features(to_silver(bundle))
