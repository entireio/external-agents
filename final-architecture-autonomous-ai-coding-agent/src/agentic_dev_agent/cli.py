from __future__ import annotations

import argparse
import json
from dataclasses import asdict
from pathlib import Path

from .workflow import run_workflow


def main() -> None:
    parser = argparse.ArgumentParser(description="Run the autonomous AI coding agent workflow.")
    parser.add_argument("--repo", required=True, help="Path to a new project directory or existing repository.")
    parser.add_argument("--request", required=True, help="Natural language development request.")
    parser.add_argument("--apply", action="store_true", help="Write generated artifacts into the repository.")
    parser.add_argument("--dry-run", action="store_true", help="Propose changes without writing files.")
    parser.add_argument("--max-debug-loops", type=int, default=2, help="Maximum debug iterations after failed validation.")
    args = parser.parse_args()

    repo = Path(args.repo)
    repo.mkdir(parents=True, exist_ok=True)
    state = run_workflow(
        args.request,
        repo,
        apply_changes=args.apply and not args.dry_run,
        max_debug_loops=args.max_debug_loops,
    )

    print(
        json.dumps(
            {
                "request": state.request,
                "repo_path": str(state.repo_path),
                "apply_changes": state.apply_changes,
                "provider": state.provider_name,
                "analysis": asdict(state.analysis) if state.analysis else None,
                "conversation_history_loaded": len(state.conversation_history),
                "plan": asdict(state.plan) if state.plan else None,
                "artifacts": [asdict(a) for a in state.artifacts],
                "test_results": [asdict(r) for r in state.test_results],
                "review": asdict(state.review) if state.review else None,
                "deployment_url": state.deployment_url,
            },
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
