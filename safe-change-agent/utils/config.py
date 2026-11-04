"""Configuration defaults for the Safe Change Agent."""

import os


def entire_command() -> str:
    """Return the configured Entire executable name."""
    return os.getenv("ENTIRE_COMMAND", "entire")
