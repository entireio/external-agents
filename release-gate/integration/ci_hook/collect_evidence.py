"""Release Gate evidence collector CLI (Track 3 Entire integration).

Thin wrapper around ``release_gate.collect`` so CI and the native
``entire release-gate`` plugin share identical logic.

    python integration/ci_hook/collect_evidence.py --repo . --base <sha> --head <sha> \
        --pr-number 1 --pr-repo owner/name --run-tests --out bundle.json
"""
from __future__ import annotations

import argparse
import json
import os
import sys

_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
if _ROOT not in sys.path:
    sys.path.insert(0, _ROOT)

from release_gate.collect import build_bundle  # noqa: E402


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Collect Release Gate evidence.")
    p.add_argument("--repo", default=".")
    p.add_argument("--base", default=None, help="Base SHA (defaults to head~1).")
    p.add_argument("--head", default="HEAD")
    p.add_argument("--pr-number", type=int, required=True)
    p.add_argument("--pr-repo", required=True, help="owner/name")
    p.add_argument("--author", default="")
    p.add_argument("--title", default="")
    p.add_argument("--run-tests", action="store_true")
    p.add_argument("--out", default=None, help="Write bundle here (default: stdout).")
    args = p.parse_args(argv)

    bundle = build_bundle(
        repo=args.repo, base=args.base, head=args.head, pr_number=args.pr_number,
        pr_repo=args.pr_repo, author=args.author, title=args.title,
        run_tests=args.run_tests,
    )
    text = json.dumps(bundle, indent=2)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as fh:
            fh.write(text)
        sys.stderr.write(f"[collect_evidence] wrote {args.out}\n")
    else:
        print(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
