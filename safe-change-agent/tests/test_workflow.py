"""Tests for the early workflow data contracts."""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from services.checkpoint_service import CheckpointService  # noqa: E402
from services.memory_service import MemoryService  # noqa: E402


class WorkflowTests(unittest.TestCase):
    def test_validation_evidence_can_be_saved_to_memory(self) -> None:
        evidence = CheckpointService().prepare("Validated parser update", ["python -m unittest"])
        memory = MemoryService()
        memory.add(evidence)

        self.assertEqual(memory.records, [evidence])
