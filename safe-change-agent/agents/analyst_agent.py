"""Agent responsible for investigating a requested change and its impact."""


class AnalystAgent:
    """Produces an evidence-based change plan."""

    def role(self) -> str:
        return "Analyze code structure, dependencies, and likely change impact."
