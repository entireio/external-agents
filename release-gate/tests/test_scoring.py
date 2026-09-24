from release_gate.scoring import GATE_PASS, GATE_REVIEW, score_features


def _features(**overrides):
    base = {
        "pr_number": 1,
        "revision_sha": "abc",
        "repo": "o/r",
        "blast_radius_symbols": 0,
        "max_impact_distance": 0,
        "unresolved_risk_count": 0,
        "churn_total": 0,
        "test_failure_rate": 0.0,
        "impacted_symbol_test_coverage": 1.0,
        "graph_available": True,
        "checkpoints_available": True,
        "tests_available": True,
    }
    base.update(overrides)
    return base


def test_low_risk_passes():
    s = score_features(_features())
    assert s["risk_score"] < GATE_PASS
    assert s["gate"] == "PASS"


def test_high_risk_blocks():
    s = score_features(_features(
        blast_radius_symbols=40,
        max_impact_distance=6,
        unresolved_risk_count=8,
        churn_total=800,
        test_failure_rate=1.0,
        impacted_symbol_test_coverage=0.0,
    ))
    assert s["risk_score"] >= GATE_REVIEW
    assert s["gate"] == "BLOCK"


def test_score_bounded_and_top_factors():
    s = score_features(_features(unresolved_risk_count=100, blast_radius_symbols=100))
    assert 0.0 <= s["risk_score"] <= 1.0
    assert len(s["top_factors"]) <= 3
    # Checkpoint risk should be the dominant driver here.
    assert s["top_factors"][0]["name"] == "unresolved_risk"


def test_evidence_gaps_reported():
    s = score_features(_features(graph_available=False, checkpoints_available=False))
    assert "graph" in s["evidence_gaps"]
    assert "checkpoints" in s["evidence_gaps"]
