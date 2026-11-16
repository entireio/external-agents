"""Stores engineering-memory records in-process during early development."""

from models.schemas import CheckpointEvidence


class MemoryService:
    """A replaceable in-memory store for checkpoint evidence."""

    def __init__(self) -> None:
        self.records: list[CheckpointEvidence] = []

    def add(self, evidence: CheckpointEvidence) -> None:
        self.records.append(evidence)
