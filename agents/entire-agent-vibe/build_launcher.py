"""Generate the launcher for this agent's local build."""

from pathlib import Path
import shlex


def main():
    root = Path.cwd()
    template = """#!/usr/bin/env bash
set -euo pipefail
ROOT=__AGENT_ROOT__
exec python3 "$ROOT/entire_agent_vibe.py" "$@"
"""
    launcher = root / 'entire-agent-vibe'
    launcher.write_text(template.replace("__AGENT_ROOT__", shlex.quote(str(root))), encoding="utf-8")
    launcher.chmod(0o755)


if __name__ == "__main__":
    main()
