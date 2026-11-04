"""Thin integration boundary for the Entire CLI."""


class EntireService:
    """Describes the Entire operations the workflow will use."""

    def checkpoint_command(self) -> list[str]:
        return ["entire", "checkpoint", "list"]
