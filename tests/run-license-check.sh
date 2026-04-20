#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
decisions_file="$repo_root/doc/dependency_decisions.yml"

if [[ ! -f "$decisions_file" ]]; then
  echo "Missing LicenseFinder decisions file: $decisions_file" >&2
  exit 1
fi

declare -a module_dirs=()
while IFS= read -r dir; do
  module_dirs+=("$dir")
done < <(find "$repo_root" -name go.mod -not -path '*/vendor/*' -exec dirname {} \; | sort)

if [[ "${#module_dirs[@]}" -eq 0 ]]; then
  echo "No Go modules found for license checking." >&2
  exit 1
fi

failed=0
for dir in "${module_dirs[@]}"; do
  rel="${dir#$repo_root/}"
  echo "Checking licenses for ${rel}..."
  if ! (
    cd "$dir"
    env GOCACHE=/tmp/go-build-cache \
      license_finder action_items \
      --enabled-package-managers gomodules \
      --project-path . \
      --decisions-file "$decisions_file"
  ); then
    failed=1
  fi
done

exit "$failed"
