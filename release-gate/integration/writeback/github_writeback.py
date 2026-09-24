"""Post (or update) the Release Gate comment on a GitHub PR.

Idempotent: finds an existing Release Gate comment by its hidden marker and
updates it instead of posting duplicates. Supports ``--dry-run`` so the slice is
demonstrable without a token. The token is read only from the environment
(never a CLI arg, never logged).
"""
from __future__ import annotations

import argparse
import json
import os
import sys

_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
if _ROOT not in sys.path:
    sys.path.insert(0, _ROOT)

from release_gate.bundle import load_bundle  # noqa: E402
from release_gate.features import build_gold_features  # noqa: E402
from release_gate.scoring import score_features  # noqa: E402
from release_gate.silver import to_silver  # noqa: E402
from release_gate.writeback import MARKER, render_comment  # noqa: E402

_API = "https://api.github.com"


def _find_existing_comment(session, repo: str, pr: int, token: str):
    import requests  # local import so dry-run needs no dependency

    url = f"{_API}/repos/{repo}/issues/{pr}/comments"
    r = session.get(url, timeout=30)
    r.raise_for_status()
    for c in r.json():
        if MARKER in (c.get("body") or ""):
            return c.get("id")
    return None


def post_comment(repo: str, pr: int, body: str, dry_run: bool) -> dict:
    if dry_run:
        print(body)
        return {"dry_run": True, "posted": False}

    token = os.environ.get("GITHUB_TOKEN")
    if not token:
        raise SystemExit("GITHUB_TOKEN not set; use --dry-run to preview.")

    import requests

    session = requests.Session()
    session.headers.update({
        "Authorization": f"Bearer {token}",
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    })
    existing = _find_existing_comment(session, repo, pr, token)
    if existing:
        url = f"{_API}/repos/{repo}/issues/comments/{existing}"
        r = session.patch(url, data=json.dumps({"body": body}), timeout=30)
    else:
        url = f"{_API}/repos/{repo}/issues/{pr}/comments"
        r = session.post(url, data=json.dumps({"body": body}), timeout=30)
    r.raise_for_status()
    return {"dry_run": False, "posted": True, "url": r.json().get("html_url")}


def main(argv=None) -> int:
    p = argparse.ArgumentParser(description="Post the Release Gate PR comment.")
    p.add_argument("--bundle", required=True, help="Path to evidence bundle JSON.")
    p.add_argument("--repo", default=None, help="owner/name (default: from bundle).")
    p.add_argument("--pr", type=int, default=None, help="PR number (default: from bundle).")
    p.add_argument("--dry-run", action="store_true")
    args = p.parse_args(argv)

    bundle = load_bundle(args.bundle)
    features = build_gold_features(to_silver(bundle))
    score = score_features(features)
    body = render_comment(bundle, features, score)

    repo = args.repo or bundle["pr"]["repo"]
    pr = args.pr if args.pr is not None else bundle["pr"]["number"]
    result = post_comment(repo, pr, body, args.dry_run)
    sys.stderr.write(f"[github_writeback] {result}\n")
    return 0


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass
    raise SystemExit(main())
