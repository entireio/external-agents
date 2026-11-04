"""Basic, dependency-free code-structure inspection service."""

from pathlib import Path


class CodeAnalyzer:
    """Find source files so later agents can reason about change scope."""

    def __init__(self, root: Path | str = ".") -> None:
        self.root = Path(root)

    def python_files(self) -> list[Path]:
        """Return Python files below the configured root in stable order."""
        return sorted(path for path in self.root.rglob("*.py") if "__pycache__" not in path.parts)
