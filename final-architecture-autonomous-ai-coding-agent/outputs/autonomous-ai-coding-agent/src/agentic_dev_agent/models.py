from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Protocol

from .state import AgentState, ModelRole


class LLMProvider(Protocol):
    def complete(self, model: str, system: str, prompt: str) -> str:
        ...


class LocalDeterministicProvider:
    """Offline provider for demos and tests."""

    def complete(self, model: str, system: str, prompt: str) -> str:
        return f"[{model}] {system}\n{prompt[:1200]}"


class OpenAICompatibleProvider:
    def __init__(self) -> None:
        from openai import OpenAI  # type: ignore

        self.client = OpenAI(
            api_key=os.getenv("OPENAI_API_KEY"),
            base_url=os.getenv("OPENAI_BASE_URL") or None,
        )

    def complete(self, model: str, system: str, prompt: str) -> str:
        response = self.client.chat.completions.create(
            model=model,
            messages=[
                {"role": "system", "content": system},
                {"role": "user", "content": prompt},
            ],
        )
        return response.choices[0].message.content or ""


def make_provider() -> LLMProvider:
    if os.getenv("OPENAI_API_KEY"):
        try:
            return OpenAICompatibleProvider()
        except ImportError:
            return LocalDeterministicProvider()
    return LocalDeterministicProvider()


@dataclass
class ModelRouter:
    coding_model: str = os.getenv("CODING_MODEL", "coding-local")
    reasoning_model: str = os.getenv("REASONING_MODEL", "reasoning-local")
    fast_model: str = os.getenv("FAST_MODEL", "fast-local")
    debugger_model: str = os.getenv("DEBUGGER_MODEL", "debugger-local")

    def route(self, state: AgentState) -> dict[ModelRole, str]:
        risk = state.analysis.risk if state.analysis else "medium"
        if risk == "high":
            reasoning = self.reasoning_model
        else:
            reasoning = self.fast_model
        return {
            "coding": self.coding_model,
            "reasoning": reasoning,
            "fast": self.fast_model,
            "debugger": self.debugger_model,
        }
