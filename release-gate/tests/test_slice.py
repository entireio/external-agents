import os

from scripts.run_local_slice import run

_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def test_slice_end_to_end():
    result = run(os.path.join(_ROOT, "seed_data", "sample_bundle.json"))
    assert "score" in result and "comment" in result
    score = result["score"]
    assert 0.0 <= score["risk_score"] <= 1.0
    assert score["gate"] in {"PASS", "REVIEW", "BLOCK"}
    # The rendered comment carries the Entire evidence narrative.
    assert "Entire Graph" in result["comment"]
    assert "Entire Checkpoints" in result["comment"]
    assert "Release Gate" in result["comment"]


def test_lake_layers_written():
    run(os.path.join(_ROOT, "seed_data", "sample_bundle.json"))
    lake = os.path.join(_ROOT, "._lake")
    for layer in ("bronze", "silver", "gold"):
        assert os.path.isdir(os.path.join(lake, layer))
