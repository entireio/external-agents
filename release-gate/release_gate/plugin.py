"""Native Entire CLI plugin: ``entire release-gate``.

Discovered by the Entire CLI as an ``entire-release-gate`` executable on PATH,
so Release Gate is a first-class Entire command (like ``entire graph``), not an
external app calling ``entire``. It reuses the Entire Graph and Entire Checkpoint
data through the same CLI and turns it into a release-risk gate.

Subcommands:
    entire release-gate info                 # plugin metadata (JSON)
    entire release-gate version
    entire release-gate collect [flags]      # emit the evidence bundle (JSON)
    entire release-gate score   [flags]      # collect -> score -> print the gate
"""
from __future__ import annotations

import argparse
import json
import os
import sys

# The plugin runs inside the user's repo; make it importable for optional extras.
_CWD = os.getcwd()
if _CWD not in sys.path:
    sys.path.insert(0, _CWD)

from release_gate import __version__
from release_gate.collect import build_bundle
from release_gate.events import events_to_bundle, parse_events
from release_gate.features import build_gold_features
from release_gate.handoff import build_handoff, render_handoff
from release_gate.scoring import score_features
from release_gate.silver import to_silver
from release_gate.writeback import render_comment
from release_gate.dashboard import render_dashboard

PLUGIN_NAME = "release-gate"


def _info() -> dict:
    return {
        "protocol_version": 1,
        "name": PLUGIN_NAME,
        "kind": "cli-plugin",
        "version": __version__,
        "description": "PR release-risk gate from Entire Checkpoints + Entire Graph.",
        "commands": ["info", "version", "collect", "score", "handoff", "ingest", "dashboard"],
    }


def _add_common(sp: argparse.ArgumentParser) -> None:
    sp.add_argument("--repo", default=".")
    sp.add_argument("--base", default=None)
    sp.add_argument("--head", default="HEAD")
    sp.add_argument("--pr-number", type=int, default=0)
    sp.add_argument("--pr-repo", default="")
    sp.add_argument("--author", default="")
    sp.add_argument("--title", default="")
    sp.add_argument("--run-tests", action="store_true")


def _bundle_from(args) -> dict:
    return build_bundle(
        repo=args.repo, base=args.base, head=args.head, pr_number=args.pr_number,
        pr_repo=args.pr_repo, author=args.author, title=args.title,
        run_tests=args.run_tests,
    )


def _score_via_endpoint(features: dict, fallback: dict, profile: str) -> dict:
    """Score via the Databricks Model Serving endpoint; heuristic fallback on error."""
    try:
        from databricks.sdk import WorkspaceClient

        from release_gate.model import FEATURE_COLUMNS, to_vector

        import requests

        w = WorkspaceClient(profile=profile)
        headers = w.config.authenticate()  # {"Authorization": "Bearer ..."}
        url = f"{w.config.host.rstrip('/')}/serving-endpoints/release-gate-risk/invocations"
        record = dict(zip(FEATURE_COLUMNS, to_vector(features)))
        r = requests.post(url, headers={**headers, "Content-Type": "application/json"},
                          json={"dataframe_records": [record]}, timeout=30)
        r.raise_for_status()
        pred = r.json()["predictions"][0]
        raw = float(pred.get("risk_score", 0) if isinstance(pred, dict) else pred)
        # v2 serves a graded probability in [0,1]; v1 served a 0/1 class.
        score = raw if 0.0 < raw < 1.0 else (0.85 if raw >= 1.0 else 0.15)
        gate = "PASS" if score < 0.34 else "REVIEW" if score < 0.67 else "BLOCK"
        return {**fallback, "risk_score": round(score, 4), "gate": gate,
                "model": "served:release-gate-risk"}
    except Exception as exc:  # noqa: BLE001 - protect the demo
        sys.stderr.write(f"[release-gate] endpoint unreachable ({exc}); heuristic fallback\n")
        return fallback


def _risk_texts(bundle: dict) -> list:
    out: list = []
    for c in (bundle.get("checkpoint_signals", {}) or {}).get("checkpoints", []) or []:
        out += c.get("unresolved_risks", []) or []
    return out


def _ai_narrative(features: dict, score: dict, bundle: dict, profile: str, use_ai: bool):
    if not use_ai:
        return None
    from release_gate.ai import risk_narrative
    return risk_narrative(features, score, profile=profile, risk_texts=_risk_texts(bundle))


def main(argv: list[str] | None = None) -> int:
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

    parser = argparse.ArgumentParser(prog="entire release-gate")
    sub = parser.add_subparsers(dest="cmd", required=True)

    sub.add_parser("info")
    sub.add_parser("version")
    _add_common(sub.add_parser("collect"))
    sp_hand = sub.add_parser("handoff")
    sp_hand.add_argument("--repo", default=".")
    sp_hand.add_argument("--base", default=None)
    sp_hand.add_argument("--head", default="HEAD")
    sp_ing = sub.add_parser("ingest")
    sp_ing.add_argument("--events", required=True, help="Lifecycle-event / agent-transcript JSONL.")
    sp_ing.add_argument("--pr-number", type=int, default=0)
    sp_ing.add_argument("--pr-repo", default="")
    sp_ing.add_argument("--ai", action="store_true", help="Add an AI review (Databricks foundation model).")
    sp_ing.add_argument("--profile", default=os.environ.get("DATABRICKS_CONFIG_PROFILE", "release-gate"))
    sp_score = sub.add_parser("score")
    _add_common(sp_score)
    sp_score.add_argument("--out", default=None, help="Write the bundle to this path.")
    sp_score.add_argument("--ai", action="store_true", help="Add an AI review (Databricks foundation model).")
    sp_score.add_argument("--use-endpoint", action="store_true",
                          help="Score via the Databricks Model Serving endpoint (heuristic fallback).")
    sp_score.add_argument("--load-databricks", action="store_true",
                          help="Also load the medallion tables live (needs a profile).")
    sp_score.add_argument("--warehouse", default=os.environ.get("RG_WAREHOUSE_ID", ""))
    sp_score.add_argument("--profile", default=os.environ.get("DATABRICKS_CONFIG_PROFILE", "release-gate"))
    sp_dash = sub.add_parser("dashboard")
    _add_common(sp_dash)
    sp_dash.add_argument("--events", default=None, help="Render from a transcript JSONL instead of the repo.")
    sp_dash.add_argument("--out", default="release_gate_dashboard.html")
    sp_dash.add_argument("--ai", action="store_true", help="Include an AI review (Databricks foundation model).")
    sp_dash.add_argument("--use-endpoint", action="store_true")
    sp_dash.add_argument("--profile", default=os.environ.get("DATABRICKS_CONFIG_PROFILE", "release-gate"))

    args = parser.parse_args(argv)

    if args.cmd == "info":
        print(json.dumps(_info(), indent=2))
        return 0
    if args.cmd == "version":
        print(f"entire release-gate {__version__}")
        return 0

    if args.cmd == "handoff":
        print(render_handoff(build_handoff(args.repo, args.base, args.head)))
        return 0

    if args.cmd == "ingest":
        with open(args.events, "r", encoding="utf-8") as fh:
            parsed = parse_events(fh)
        bundle = events_to_bundle(parsed, pr_number=args.pr_number, pr_repo=args.pr_repo)
        features = build_gold_features(to_silver(bundle))
        score = score_features(features)
        comment = render_comment(bundle, features, score)
        narrative = _ai_narrative(features, score, bundle, args.profile, getattr(args, "ai", False))
        if narrative:
            comment += "\n\n### AI review (Databricks foundation model)\n" + narrative
        print(comment)
        partial = bundle["ingest"]["partial"]
        print(f"\n[release-gate] gate={score['gate']} risk={score['risk_score']} "
              f"partial={partial} formats={bundle['ingest']['formats']} "
              f"unknown={bundle['ingest']['unknown_events']}")
        return 0

    if args.cmd == "dashboard":
        if getattr(args, "events", None):
            with open(args.events, "r", encoding="utf-8") as fh:
                bundle = events_to_bundle(parse_events(fh), pr_number=args.pr_number, pr_repo=args.pr_repo)
        else:
            bundle = _bundle_from(args)
        features = build_gold_features(to_silver(bundle))
        score = score_features(features)
        if getattr(args, "use_endpoint", False):
            score = _score_via_endpoint(features, score, args.profile)
        narrative = _ai_narrative(features, score, bundle, args.profile, getattr(args, "ai", False))
        with open(args.out, "w", encoding="utf-8") as fh:
            fh.write(render_dashboard(bundle, features, score, narrative))
        print(f"[release-gate] dashboard written to {args.out} "
              f"(gate={score['gate']} risk={score['risk_score']})")
        return 0

    bundle = _bundle_from(args)

    if args.cmd == "collect":
        print(json.dumps(bundle, indent=2))
        return 0

    # score
    features = build_gold_features(to_silver(bundle))
    score = score_features(features)
    if getattr(args, "use_endpoint", False):
        score = _score_via_endpoint(features, score, args.profile if hasattr(args, "profile") else "release-gate")
    comment = render_comment(bundle, features, score)
    narrative = _ai_narrative(features, score, bundle, getattr(args, "profile", "release-gate"), getattr(args, "ai", False))
    if narrative:
        comment += "\n\n### AI review (Databricks foundation model)\n" + narrative

    if getattr(args, "out", None):
        with open(args.out, "w", encoding="utf-8") as fh:
            json.dump(bundle, fh, indent=2)

    if getattr(args, "load_databricks", False) and args.warehouse:
        try:
            import tempfile

            from scripts.databricks_live_load import run as _dbx_run  # type: ignore

            with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False,
                                             encoding="utf-8") as tf:
                json.dump(bundle, tf)
                path = tf.name
            _dbx_run(path, args.profile, args.warehouse)
        except Exception as exc:  # noqa: BLE001
            sys.stderr.write(f"[release-gate] databricks load skipped: {exc}\n")

    print(comment)
    print(f"\n[release-gate] gate={score['gate']} risk={score['risk_score']} "
          f"model={score['model']}")
    # Non-zero exit lets CI fail the check on a hard BLOCK.
    return 2 if score["gate"] == "BLOCK" else 0


if __name__ == "__main__":
    raise SystemExit(main())
