"""Agent responsible for applying an approved change plan."""


class ImplementerAgent:
    """Owns code and test modifications after analysis is complete."""

    def role(self) -> str:
        return "Implement planned changes and update affected tests."
