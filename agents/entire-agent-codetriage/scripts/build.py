#!/usr/bin/env python3
"""Mark the protocol launcher executable for lifecycle discovery."""

from __future__ import annotations

import stat
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LAUNCHER = ROOT / "entire-agent-codetriage"
CMD = ROOT / "entire-agent-codetriage.cmd"


def main() -> int:
    if not LAUNCHER.is_file():
        print(f"missing launcher: {LAUNCHER}", file=sys.stderr)
        return 1
    mode = LAUNCHER.stat().st_mode
    LAUNCHER.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    print(f"built {LAUNCHER.name}")
    if CMD.is_file():
        print(f"windows launcher {CMD.name}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
