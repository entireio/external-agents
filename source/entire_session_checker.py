"""Check whether Entire reports an active session for the current repository."""

from __future__ import annotations

import shutil
import subprocess


def check_entire_session() -> tuple[bool | None, str]:
    """Return the session state and the output from ``entire status``.

    The state is ``True`` for an active session, ``False`` when Entire reports
    no active session, and ``None`` when the Entire CLI is unavailable or its
    status cannot be determined.
    """
    if shutil.which("entire") is None:
        return None, "Entire CLI is not installed or is not available on PATH."

    result = subprocess.run(
        ["entire", "status"],
        capture_output=True,
        text=True,
        check=False,
    )
    output = (result.stdout + result.stderr).strip()
    normalized = output.lower()

    if result.returncode != 0:
        return None, output or "Unable to determine Entire session status."
    if "no active session" in normalized or "no session" in normalized:
        return False, output
    if "active" in normalized and "session" in normalized:
        return True, output

    return None, output or "Entire returned no session information."


def main() -> None:
    active, details = check_entire_session()

    if active is True:
        print("An Entire session is active.")
    elif active is False:
        print("No Entire session is active.")
    else:
        print("Could not determine whether an Entire session is active.")

    if details:
        print(f"\nDetails:\n{details}")


if __name__ == "__main__":
    main()
