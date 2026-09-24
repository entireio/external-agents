"""Typed workflow records for the Safe Change Agent."""

from dataclasses import dataclass, field


@dataclass
class CheckpointEvidence:
    """The context preserved alongside an implementation checkpoint."""

    summary: str
    tests_run: list[str] = field(default_factory=list)
