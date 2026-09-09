"""Generate the launcher for this agent's local build."""

from pathlib import Path
import shlex


def main():
    root = Path.cwd()
    template = """#!/usr/bin/env bash
set -euo pipefail
ROOT=__AGENT_ROOT__
if [[ "${1:-}" == "__e2e_run_fixture" ]]; then
  exec "$ROOT/.venv-crewai/bin/python" "$ROOT/e2e_crewai_fixture.py"
fi
exec "$ROOT/.venv/bin/python" "$ROOT/protocol_wrapper.py" crewai "$@"
"""
    launcher = root / 'entire-agent-crewai'
    launcher.write_text(template.replace("__AGENT_ROOT__", shlex.quote(str(root))), encoding="utf-8")
    launcher.chmod(0o755)


if __name__ == "__main__":
    main()
