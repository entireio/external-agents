"""Run the full Release Gate slice locally, with no Databricks or GitHub calls.

This mirrors the medallion flow (Bronze -> Silver -> Gold -> score -> writeback)
using local JSON files under ``._lake/`` so the whole pipeline is verifiable
from a clean checkout without spending Databricks Free Edition quota.

Usage:
    python scripts/run_local_slice.py [path/to/bundle.json]
"""
from __future__ import annotations

import json
import os
import sys

# Emoji/unicode in the rendered comment must not crash on a cp1252 console.
try:
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
except (AttributeError, ValueError):
    pass

# Make the repo root importable when run as a script.
_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if _ROOT not in sys.path:
    sys.path.insert(0, _ROOT)

from release_gate.bundle import load_bundle, pr_key, validate_bundle  # noqa: E402
from release_gate.features import build_gold_features  # noqa: E402
from release_gate.scoring import score_features  # noqa: E402
from release_gate.silver import to_silver  # noqa: E402
from release_gate.writeback import render_comment  # noqa: E402

_LAKE = os.path.join(_ROOT, "._lake")


def _write(layer: str, name: str, payload) -> str:
    path = os.path.join(_LAKE, layer)
    os.makedirs(path, exist_ok=True)
    dest = os.path.join(path, name)
    with open(dest, "w", encoding="utf-8") as fh:
        json.dump(payload, fh, indent=2)
    return dest


def run(bundle_path: str) -> dict:
    bundle = load_bundle(bundle_path)

    errors = validate_bundle(bundle)
    if errors:
        raise SystemExit(
            "Evidence bundle failed validation:\n  - " + "\n  - ".join(errors)
        )

    key = pr_key(bundle).replace(":", "_").replace("/", "_")

    # Bronze: raw, append-only.
    _write("bronze", f"{key}.json", bundle)

    # Silver: typed rows.
    silver = to_silver(bundle)
    _write("silver", f"{key}.json", silver)

    # Gold: one feature row + score row.
    features = build_gold_features(silver)
    _write("gold", f"{key}.features.json", features)

    score = score_features(features)
    _write("gold", f"{key}.score.json", score)

    # Writeback: render the PR comment (printed, not posted).
    comment = render_comment(bundle, features, score)

    print("=" * 70)
    print(f"Release Gate slice complete for PR key {pr_key(bundle)}")
    print(f"  risk_score={score['risk_score']}  gate={score['gate']}  "
          f"model={score['model']}")
    if score["evidence_gaps"]:
        print(f"  evidence_gaps={score['evidence_gaps']}")
    print(f"  lake written under: {_LAKE}")
    print("-" * 70)
    print(comment)
    print("=" * 70)

    return {"features": features, "score": score, "comment": comment}


if __name__ == "__main__":
    path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(
        _ROOT, "seed_data", "sample_bundle.json"
    )
    run(path)
