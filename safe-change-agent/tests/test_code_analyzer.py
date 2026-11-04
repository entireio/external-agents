"""Tests for dependency-free code analysis."""

import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from services.code_analyzer import CodeAnalyzer  # noqa: E402


class CodeAnalyzerTests(unittest.TestCase):
    def test_python_files_returns_python_files_in_order(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "b.py").touch()
            (root / "a.py").touch()
            (root / "note.txt").touch()

            self.assertEqual([path.name for path in CodeAnalyzer(root).python_files()], ["a.py", "b.py"])
