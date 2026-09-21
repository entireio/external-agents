"""Generate the launcher for this agent's local build."""

from pathlib import Path
import shlex


def main():
    root = Path.cwd()
    template = """#!/usr/bin/env bash
set -euo pipefail
ROOT=__AGENT_ROOT__
PYTHON="$ROOT/.venv/bin/python"

if [[ "${1:-}" == "__e2e_run_prompt" ]]; then
  shift
  exec "$PYTHON" "$ROOT/e2e_langgraph_fixture.py" "$@"
fi

exec "$PYTHON" "$ROOT/protocol_wrapper.py" langgraph "$@"
"""
    launcher = root / 'entire-agent-langgraph'
    launcher.write_text(template.replace("__AGENT_ROOT__", shlex.quote(str(root))), encoding="utf-8")
    launcher.chmod(0o755)


if __name__ == "__main__":
    main()
