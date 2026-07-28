#!/usr/bin/env bash
set -euo pipefail

# Verifies a local kilo install meets the integration's expectations.
# Useful as a pre-flight before running lifecycle tests.

if ! command -v kilo >/dev/null 2>&1; then
  echo "kilo binary not on PATH" >&2
  exit 1
fi

kilo --version
kilo session list --format json --max-count 1 >/dev/null || {
  echo "kilo session list failed — check Kilo install" >&2
  exit 1
}

echo "kilo install looks good"
