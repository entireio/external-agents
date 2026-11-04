from __future__ import annotations

import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Protocol

from .state import AgentState, ModelRole


def _load_dotenv(path: Path = Path(".env")) -> None:
    if not path.exists():
        return
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, value = stripped.split("=", 1)
        cleaned = value.strip().strip('"').strip("'")
        if cleaned:
            os.environ.setdefault(key.strip(), cleaned)


_load_dotenv()


class LLMProvider(Protocol):
    name: str

    def complete(self, model: str, system: str, prompt: str) -> str:
        ...


class LocalDeterministicProvider:
    """Offline provider for demos and tests."""

    name = "local-deterministic"

    def complete(self, model: str, system: str, prompt: str) -> str:
        return f"[{model}] {system}\n{prompt[:1200]}"


class OpenAICompatibleProvider:
    def __init__(self) -> None:
        from openai import OpenAI  # type: ignore

        base_url = os.getenv("OPENAI_BASE_URL")
        if base_url and not base_url.startswith(("http://", "https://")):
            base_url = None
            os.environ.pop("OPENAI_BASE_URL", None)
        self.client = OpenAI(
            api_key=os.getenv("OPENAI_API_KEY"),
            base_url=base_url,
        )
        self.name = "openai-compatible"

    def complete(self, model: str, system: str, prompt: str) -> str:
        try:
            response = self._chat_completion(model, system, prompt)
        except Exception as exc:
            raise RuntimeError(f"OpenAI API call failed for model '{model}': {_clean_error(exc)}") from exc
        return response.choices[0].message.content or ""

    def _chat_completion(self, model: str, system: str, prompt: str):
        messages = [
            {"role": "system", "content": system},
            {"role": "user", "content": prompt},
        ]
        try:
            return self.client.chat.completions.create(
                model=model,
                messages=messages,
                response_format={"type": "json_object"},
            )
        except Exception:
            return self.client.chat.completions.create(model=model, messages=messages)


def make_provider() -> LLMProvider:
    if os.getenv("OPENAI_API_KEY"):
        try:
            return OpenAICompatibleProvider()
        except ImportError as exc:
            raise RuntimeError(
                "OPENAI_API_KEY is set, but the OpenAI Python package is not installed. "
                "Install it with: pip install -e \".[llm]\""
            ) from exc
    return LocalDeterministicProvider()


@dataclass
class ModelRouter:
    coding_model: str = field(default_factory=lambda: _model_name("CODING_MODEL", "coding-local"))
    reasoning_model: str = field(default_factory=lambda: _model_name("REASONING_MODEL", "reasoning-local"))
    fast_model: str = field(default_factory=lambda: _model_name("FAST_MODEL", "fast-local"))
    debugger_model: str = field(default_factory=lambda: _model_name("DEBUGGER_MODEL", "debugger-local"))

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


def _model_name(env_key: str, local_default: str) -> str:
    configured = os.getenv(env_key)
    if configured:
        return configured
    if os.getenv("OPENAI_API_KEY"):
        return os.getenv("OPENAI_MODEL", "gpt-4o-mini")
    return local_default


def _clean_error(exc: Exception) -> str:
    text = str(exc)
    api_key = os.getenv("OPENAI_API_KEY")
    if api_key:
        text = text.replace(api_key, "<OPENAI_API_KEY>")
    return text
