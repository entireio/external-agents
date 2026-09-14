from __future__ import annotations

import subprocess
from pathlib import Path

from .state import ProjectContext, RepoFile


LANG_BY_EXT = {
    ".py": "python",
    ".js": "javascript",
    ".jsx": "javascript",
    ".ts": "typescript",
    ".tsx": "typescript",
    ".java": "java",
    ".go": "go",
    ".rs": "rust",
    ".md": "markdown",
    ".json": "json",
    ".toml": "toml",
    ".yaml": "yaml",
    ".yml": "yaml",
}

IGNORED_DIRS = {
    ".git",
    ".pytest_cache",
    ".ruff_cache",
    ".mypy_cache",
    "node_modules",
    "dist",
    "build",
    "outputs",
    "work",
    "__pycache__",
    ".entire",
}


class RepositoryScanner:
    def scan(self, repo_path: Path) -> ProjectContext:
        repo_path = repo_path.resolve()
        files: list[RepoFile] = []
        docs: dict[str, str] = {}
        package_managers: list[str] = []
        test_commands: list[str] = []

        for path in repo_path.rglob("*"):
            relative_parts = path.relative_to(repo_path).parts
            ignored = any(
                part in IGNORED_DIRS or part.startswith(".venv") or part.endswith(".egg-info")
                for part in relative_parts
            )
            if not path.is_file() or ignored:
                continue
            rel = path.relative_to(repo_path).as_posix()
            language = LANG_BY_EXT.get(path.suffix.lower(), "unknown")
            files.append(RepoFile(rel, language, path.stat().st_size))
            if path.name.lower() in {"readme.md", "package.json", "requirements.txt", "pyproject.toml"}:
                docs[rel] = _read_text(path, limit=6000)

        if (repo_path / "package.json").exists():
            package_managers.append("npm")
            test_commands.append("npm test")
        if (repo_path / "pyproject.toml").exists() or (repo_path / "requirements.txt").exists():
            package_managers.append("python")
            test_commands.append("python -m pytest")

        return ProjectContext(
            repo_path=repo_path,
            files=sorted(files, key=lambda f: f.path),
            docs=docs,
            package_managers=package_managers,
            test_commands=test_commands,
            git_summary=_git_summary(repo_path),
        )


def _read_text(path: Path, limit: int) -> str:
    try:
        return path.read_text(encoding="utf-8", errors="replace")[:limit]
    except OSError:
        return ""


def _git_summary(repo_path: Path) -> dict[str, str]:
    if not (repo_path / ".git").exists():
        return {"status": "not_a_git_repo"}
    commands = {
        "branch": ["git", "branch", "--show-current"],
        "status": ["git", "status", "--short"],
        "last_commit": ["git", "log", "-1", "--pretty=%h %s"],
    }
    result: dict[str, str] = {}
    for key, command in commands.items():
        completed = subprocess.run(command, cwd=repo_path, text=True, capture_output=True, timeout=10)
        result[key] = completed.stdout.strip()
    return result
