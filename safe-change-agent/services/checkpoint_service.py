"""Coordinates checkpoint-ready engineering evidence."""

from models.schemas import CheckpointEvidence


class CheckpointService:
    """Creates evidence records to be sent to Entire in a later integration."""

    def prepare(self, summary: str, tests_run: list[str]) -> CheckpointEvidence:
        return CheckpointEvidence(summary=summary, tests_run=tests_run)
