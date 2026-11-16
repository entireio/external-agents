"""Agent responsible for validating a proposed implementation."""


class ValidatorAgent:
    """Owns test execution and evidence collection."""

    def role(self) -> str:
        return "Run validation and report the evidence needed for a checkpoint."
