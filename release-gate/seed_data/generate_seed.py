"""Generate a small, clearly-synthetic labeled PR history for cold-start model
training (Phase 6). Deterministic (seeded) so the dataset is reproducible.

DATA PROVENANCE: 100% synthetic. No real PRs, users, or code are represented.
The generative process intentionally makes release risk correlate with the same
signals the heuristic uses (blast radius, unresolved checkpoint risks, test
failures, low impacted-symbol coverage) so a model can learn a sensible ranking.

Usage:
    python seed_data/generate_seed.py            # writes seed_data/pr_history.jsonl
"""
from __future__ import annotations

import json
import os
import random

N = 500
SEED = 42
_OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "pr_history.jsonl")


def _latent_risk(f: dict) -> float:
    """Underlying probability a PR causes a release incident."""
    z = (
        0.09 * f["blast_radius_symbols"]
        + 0.28 * f["unresolved_risk_count"]
        + 1.7 * f["test_failure_rate"]
        + 0.0016 * f["churn_total"]
        + 1.3 * (1.0 - f["impacted_symbol_test_coverage"])
        + 0.18 * f["max_impact_distance"]
        - 4.4
    )
    return 1.0 / (1.0 + pow(2.718281828, -z))


def _sample(rng: random.Random) -> dict:
    blast = rng.randint(0, 40)
    files = max(1, int(blast * rng.uniform(0.3, 0.7)) + rng.randint(0, 3))
    dist = rng.choice([0, 1, 1, 2, 2, 3])
    risks = rng.choice([0, 0, 0, 1, 1, 2, 3, 5])
    checkpoints = rng.randint(1, 4)
    additions = rng.randint(5, 600)
    deletions = rng.randint(0, 200)
    tests_total = rng.randint(0, 20)
    failed = rng.randint(0, max(0, tests_total // 3))
    fail_rate = (failed / tests_total) if tests_total else 0.0
    coverage = round(rng.uniform(0.0, 1.0), 3) if blast else 1.0
    f = {
        "blast_radius_symbols": blast,
        "blast_radius_files": files,
        "max_impact_distance": dist,
        "unresolved_risk_count": risks,
        "checkpoint_count": checkpoints,
        "rejected_option_count": rng.randint(0, 3),
        "assumption_count": rng.randint(0, 4),
        "additions": additions,
        "deletions": deletions,
        "churn_total": additions + deletions,
        "files_changed": files,
        "tests_total": tests_total,
        "tests_passed": tests_total - failed,
        "tests_failed": failed,
        "tests_skipped": 0,
        "test_failure_rate": round(fail_rate, 4),
        "impacted_symbol_test_coverage": coverage,
    }
    return f


def main() -> None:
    rng = random.Random(SEED)
    with open(_OUT, "w", encoding="utf-8") as fh:
        for i in range(N):
            f = _sample(rng)
            p = _latent_risk(f)
            # Threshold on latent risk with a controlled 12% label-flip rate:
            # realistic noise, but a signal a model can actually learn.
            label = 1 if p >= 0.5 else 0
            if rng.random() < 0.12:
                label = 1 - label
            f["label_incident"] = label
            f["synthetic"] = True
            f["pr_id"] = f"synthetic-{i:04d}"
            fh.write(json.dumps(f) + "\n")
    print(f"wrote {N} synthetic labeled rows to {_OUT}")


if __name__ == "__main__":
    main()
