"""Git-related operations used to gather change evidence."""

from pathlib import Path


class GitService:
    """Provides repository metadata without changing repository state."""

    def __init__(self, root: Path | str = ".") -> None:
        self.root = Path(root)
