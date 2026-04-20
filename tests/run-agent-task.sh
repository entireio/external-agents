#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
  echo "usage: $0 <build|test> [agent-dir]" >&2
  exit 1
}

task="${1:-}"
agent_dir_arg="${2:-}"

if [[ "$task" != "build" && "$task" != "test" ]]; then
  usage
fi

run_task() {
  local dir="$1"
  local name
  name="$(basename "$dir")"

  echo "Running $task for $name..."

  if [[ -f "$dir/mise.toml" ]] && grep -Eq "^\[tasks\.${task}\]$" "$dir/mise.toml"; then
    if (
      cd "$dir"
      mise trust -y "$dir/mise.toml"
      env GOCACHE=/tmp/go-build-cache mise run "$task"
    ); then
      if [[ "$task" == "build" && -f "$dir/$name" && -z "$agent_dir_arg" ]]; then
        mkdir -p "$repo_root/bin"
        cp "$dir/$name" "$repo_root/bin/$name"
      fi
      return
    fi
    if [[ ! -f "$dir/go.mod" ]]; then
      return 1
    fi
    echo "mise ${task} failed for $name, falling back to Go defaults..." >&2
  fi

  if [[ -f "$dir/go.mod" ]]; then
    case "$task" in
      build)
        if (
          cd "$dir"
          env GOCACHE=/tmp/go-build-cache go build -o "$name" "./cmd/$name"
        ); then
          :
        else
          return 1
        fi
        if [[ -z "$agent_dir_arg" ]]; then
          mkdir -p "$repo_root/bin"
          cp "$dir/$name" "$repo_root/bin/$name"
        fi
        ;;
      test)
        (
          cd "$dir"
          env GOCACHE=/tmp/go-build-cache go test ./...
        )
        ;;
    esac
    return
  fi

  echo "No $task runner found for $name. Add $dir/mise.toml with [tasks.$task] or a Go module fallback." >&2
  return 1
}

declare -a agent_dirs=()

if [[ -n "$agent_dir_arg" ]]; then
  if [[ "$agent_dir_arg" = /* ]]; then
    agent_dirs+=("${agent_dir_arg%/}")
  else
    agent_dirs+=("$repo_root/${agent_dir_arg%/}")
  fi
else
  while IFS= read -r dir; do
    agent_dirs+=("$dir")
  done < <(find "$repo_root/agents" -mindepth 1 -maxdepth 1 -type d -name 'entire-agent-*' | sort)
fi

if [[ "${#agent_dirs[@]}" -eq 0 ]]; then
  echo "No agent directories found." >&2
  exit 1
fi

failed=0
for dir in "${agent_dirs[@]}"; do
  if [[ ! -d "$dir" ]]; then
    echo "Agent directory not found: $dir" >&2
    failed=1
    continue
  fi
  if ! run_task "$dir"; then
    failed=1
  fi
done

exit "$failed"
