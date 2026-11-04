from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Literal


ModelRole = Literal["coding", "reasoning", "fast", "debugger"]
TaskType = Literal["new_project", "existing_repo_change", "docs", "test_fix", "unknown"]


@dataclass
class RepoFile:
    path: str
    language: str
    size_bytes: int


@dataclass
class ProjectContext:
    repo_path: Path
    files: list[RepoFile] = field(default_factory=list)
    docs: dict[str, str] = field(default_factory=dict)
    package_managers: list[str] = field(default_factory=list)
    test_commands: list[str] = field(default_factory=list)
    git_summary: dict[str, Any] = field(default_factory=dict)


@dataclass
class TaskAnalysis:
    task_type: TaskType
    requirements: list[str]
    risk: Literal["low", "medium", "high"]
    notes: str


@dataclass
class Plan:
    steps: list[str]
    files_to_modify: list[str]
    test_strategy: list[str]
    acceptance_criteria: list[str]


@dataclass
class CodeArtifact:
    path: str
    content: str
    rationale: str


@dataclass
class CommandResult:
    command: str
    exit_code: int
    stdout: str
    stderr: str


@dataclass
class ReviewResult:
    approved: bool
    findings: list[str]
    summary: str


@dataclass
class AgentState:
    request: str
    repo_path: Path
    apply_changes: bool = False
    max_debug_loops: int = 2
    context: ProjectContext | None = None
    analysis: TaskAnalysis | None = None
    plan: Plan | None = None
    selected_models: dict[ModelRole, str] = field(default_factory=dict)
    artifacts: list[CodeArtifact] = field(default_factory=list)
    test_results: list[CommandResult] = field(default_factory=list)
    errors: list[str] = field(default_factory=list)
    review: ReviewResult | None = None
    deployment_url: str | None = None
