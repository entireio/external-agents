import os

from release_gate.events import events_to_bundle, parse_events
from release_gate.features import build_gold_features
from release_gate.scoring import score_features
from release_gate.silver import to_silver

_SEED = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "seed_data")

_RISK = "Should expiry or disabled state take precedence in user-facing errors?"


def _parse(name):
    with open(os.path.join(_SEED, name), "r", encoding="utf-8") as fh:
        return parse_events(fh)


def test_new_format_real_fixture():
    # The official Track-3 fixture: AcmeCode agent-session transcript.
    p = _parse("track-3-agent-session.jsonl")
    assert p["formats"] == ["new"]
    ev = p["events"]
    assert ev["meta"]["repo"] == "github.com/example/checkout-service"
    assert "apply_coupon.ts" in " ".join(c["file"] for c in ev["changes"])
    assert ev["checkpoints"][0]["risks"] == [_RISK]
    assert ev["tests"] == {"passed": 8, "failed": 0, "skipped": 0}  # final run wins
    assert p["complete"] is True and p["parse_errors"] == 0
    bundle = events_to_bundle(p, pr_number=5, pr_repo="example/checkout-service")
    assert bundle["ingest"]["partial"] is False
    assert bundle["checkpoint_signals"]["unresolved_risk_count"] == 1


def test_original_format():
    p = _parse("agent-session-original.jsonl")
    assert p["formats"] == ["original"]
    assert p["events"]["tests"] == {"passed": 8, "failed": 0, "skipped": 0}
    assert p["events"]["checkpoints"][0]["risks"] == [_RISK]
    assert p["complete"] is True
    assert events_to_bundle(p)["ingest"]["partial"] is False


def test_unknown_events_do_not_crash():
    p = _parse("agent-session-incomplete.jsonl")
    # Contains a brand-new unknown event type + a truncated final line.
    assert p["unknown"] and p["parse_errors"] >= 1
    assert p["complete"] is False


def test_incomplete_input_yields_partial_not_crash():
    p = _parse("agent-session-incomplete.jsonl")
    bundle = events_to_bundle(p, pr_number=99, pr_repo="o/r")
    assert bundle["ingest"]["partial"] is True
    # Still scorable on the partial evidence (not discarded).
    score = score_features(build_gold_features(to_silver(bundle)))
    assert 0.0 <= score["risk_score"] <= 1.0


def test_both_formats_produce_scorable_bundles():
    for name in ("track-3-agent-session.jsonl", "agent-session-original.jsonl"):
        bundle = events_to_bundle(_parse(name), pr_number=1, pr_repo="o/r")
        score = score_features(build_gold_features(to_silver(bundle)))
        assert score["gate"] in {"PASS", "REVIEW", "BLOCK"}
        assert bundle["ingest"]["partial"] is False


def test_garbage_never_raises():
    p = parse_events(['{"event": "x.y.z"}', "not-json", "{}", ""])
    bundle = events_to_bundle(p, pr_number=1, pr_repo="o/r")
    assert bundle["ingest"]["partial"] is True
    assert bundle["checkpoint_signals"]["available"] is False
