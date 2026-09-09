#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd "$script_dir/.." && pwd)"
repo_root="$(cd "$project_dir/../.." && pwd)"
adapter_bin="$project_dir/entire-agent-hermes"
fixtures="$project_dir/testdata/compliance.json"
runner="${EXTERNAL_AGENTS_TESTS_BIN:-external-agents-tests}"

command -v "$runner" >/dev/null 2>&1 || {
  echo "external-agents-tests is not available: $runner" >&2
  exit 1
}

if [[ ! -x "$adapter_bin" ]]; then
  (cd "$project_dir" && go build -o "$adapter_bin" ./cmd/entire-agent-hermes)
fi

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/hermes-compliance.XXXXXX")"
hermes_home="$work_dir/hermes-home"
output="$work_dir/compliance.log"
cleanup() { rm -rf -- "$work_dir"; }
trap cleanup EXIT
mkdir -p "$hermes_home"

set +e
(
  cd "$work_dir"
  HERMES_HOME="$hermes_home" "$runner" verify "$adapter_bin" --fixtures "$fixtures"
) 2>&1 | tee "$output"
status=${PIPESTATUS[0]}
set -e

if [[ "$status" -ne 0 ]]; then
  exit "$status"
fi
if ! grep -q -- 'TestWriteAndReadSession' "$output" ||
   ! grep -q -- 'TestResolveSessionFile' "$output" ||
   ! grep -q -- 'tests/mandatory' "$output"; then
  echo "compliance runner did not execute the shared mandatory suite" >&2
  exit 1
fi

echo "PASS shared mandatory compliance suite executed from $repo_root"
