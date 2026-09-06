"""Load and validate Release Gate evidence bundles.

Validation uses `jsonschema` when available and falls back to lightweight
structural checks so the pipeline still runs from a clean checkout with only
the standard library.
"""
from __future__ import annotations

import json
import os
from typing import Any

from . import SUPPORTED_SCHEMA_MAJOR

_SCHEMA_PATH = os.path.join(
    os.path.dirname(os.path.dirname(__file__)),
    "evidence_schemas",
    "evidence_bundle.schema.json",
)

_REQUIRED_TOP = (
    "schema_version",
    "bundle_id",
    "generated_at",
    "source",
    "pr",
    "graph_impact",
    "checkpoint_signals",
    "test_results",
)


def load_bundle(path: str) -> dict[str, Any]:
    with open(path, "r", encoding="utf-8") as fh:
        return json.load(fh)


def load_schema() -> dict[str, Any]:
    with open(_SCHEMA_PATH, "r", encoding="utf-8") as fh:
        return json.load(fh)


def validate_bundle(bundle: dict[str, Any]) -> list[str]:
    """Return a list of validation error strings (empty means valid)."""
    errors: list[str] = []

    # Schema-major compatibility: we accept any additive minor/patch bump.
    version = str(bundle.get("schema_version", ""))
    major = version.split(".")[0] if version else ""
    if not major.isdigit() or int(major) != SUPPORTED_SCHEMA_MAJOR:
        errors.append(
            f"schema_version {version!r} major must be {SUPPORTED_SCHEMA_MAJOR}"
        )

    try:
        import jsonschema  # type: ignore

        validator = jsonschema.Draft202012Validator(load_schema())
        for err in sorted(validator.iter_errors(bundle), key=lambda e: e.path):
            loc = "/".join(str(p) for p in err.path) or "<root>"
            errors.append(f"{loc}: {err.message}")
        return errors
    except ImportError:
        # Fallback: shallow required-key checks only.
        for key in _REQUIRED_TOP:
            if key not in bundle:
                errors.append(f"missing required top-level key: {key}")
        return errors


def pr_key(bundle: dict[str, Any]) -> str:
    """Idempotency key used for MERGE INTO across all Delta layers."""
    pr = bundle.get("pr", {})
    return f"{pr.get('number')}:{pr.get('revision_sha')}"
