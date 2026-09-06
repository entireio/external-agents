import os

from release_gate.events import events_to_bundle, parse_events
from release_gate.features import build_gold_features
from release_gate.scoring import score_features
from release_gate.silver import to_silver

_SEED = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "seed_data")


def _parse(name):
    with open(os.path.join(_SEED, name), "r", encoding="utf-8") as fh:
        return parse_events(fh)


def test_original_format_v1():
    p = _parse("events_v1.jsonl")
    assert p["formats"] == [1]
    assert p["events"]["pr"]["number"] == 7
    assert len(p["events"]["graph"]) == 2
    assert p["events"]["checkpoint"][0]["unresolved_risks"] == ["Duplicate payment on retry"]
    assert p["complete"] is True and p["parse_errors"] == 0


def test_new_format_v2_and_unknown_events():
    p = _parse("events_v2.jsonl")
    assert p["formats"] == [2]
    assert p["events"]["pr"]["number"] == 8
    assert p["events"]["checkpoint"][0]["unresolved_risks"] == ["Idempotency not covered", "Currency rounding"]
    # Two unknown events (heartbeat + brand_new) must be counted, not crash.
    assert len(p["unknown"]) == 2
    assert p["complete"] is True


def test_incomplete_input_yields_partial_not_crash():
    p = _parse("events_incomplete.jsonl")
    # Missing PR + a truncated/corrupt final line.
    assert p["events"]["pr"] is None
    assert p["parse_errors"] >= 1
    assert p["complete"] is False
    bundle = events_to_bundle(p, pr_number=99, pr_repo="o/r")
    assert bundle["ingest"]["partial"] is True
    # Downstream still runs on the partial bundle (not discarded).
    score = score_features(build_gold_features(to_silver(bundle)))
    assert 0.0 <= score["risk_score"] <= 1.0


def test_both_formats_produce_scorable_bundles():
    for name in ("events_v1.jsonl", "events_v2.jsonl"):
        bundle = events_to_bundle(_parse(name), pr_number=1, pr_repo="o/r")
        score = score_features(build_gold_features(to_silver(bundle)))
        assert score["gate"] in {"PASS", "REVIEW", "BLOCK"}
    # v1 is fully recognized -> authoritative; v2 carries unknown event types ->
    # provisional, so we never present it as complete.
    assert events_to_bundle(_parse("events_v1.jsonl"))["ingest"]["partial"] is False
    assert events_to_bundle(_parse("events_v2.jsonl"))["ingest"]["partial"] is True


def test_unknown_and_corrupt_never_raise():
    # A stream of only unknown/garbage lines must not raise and must be partial.
    p = parse_events(['{"event": "x.y", "payload": {}}', "not-json", "{}", ""])
    bundle = events_to_bundle(p, pr_number=1, pr_repo="o/r")
    assert bundle["ingest"]["partial"] is True
    assert bundle["graph_impact"]["available"] is False
