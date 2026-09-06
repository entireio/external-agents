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
from release_gate.features import build_gold_features
from release_gate.scoring import score_features
from release_gate.silver import to_silver
from release_gate.writeback import render_comment

PLUGIN_NAME = "release-gate"


def _info() -> dict:
    return {
        "protocol_version": 1,
        "name": PLUGIN_NAME,
        "kind": "cli-plugin",
        "version": __version__,
        "description": "PR release-risk gate from Entire Checkpoints + Entire Graph.",
        "commands": ["info", "version", "collect", "score"],
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
    sp_score = sub.add_parser("score")
    _add_common(sp_score)
    sp_score.add_argument("--out", default=None, help="Write the bundle to this path.")
    sp_score.add_argument("--load-databricks", action="store_true",
                          help="Also load the medallion tables live (needs a profile).")
    sp_score.add_argument("--warehouse", default=os.environ.get("RG_WAREHOUSE_ID", ""))
    sp_score.add_argument("--profile", default=os.environ.get("DATABRICKS_CONFIG_PROFILE", "release-gate"))

    args = parser.parse_args(argv)

    if args.cmd == "info":
        print(json.dumps(_info(), indent=2))
        return 0
    if args.cmd == "version":
        print(f"entire release-gate {__version__}")
        return 0

    bundle = _bundle_from(args)

    if args.cmd == "collect":
        print(json.dumps(bundle, indent=2))
        return 0

    # score
    features = build_gold_features(to_silver(bundle))
    score = score_features(features)
    comment = render_comment(bundle, features, score)

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
