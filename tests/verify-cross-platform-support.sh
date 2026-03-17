#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_file() {
  local path="$1"
  if [[ ! -f "$repo_root/$path" ]]; then
    echo "missing file: $path" >&2
    exit 1
  fi
}

require_contains() {
  local path="$1"
  local pattern="$2"
  if ! grep -Fq "$pattern" "$repo_root/$path"; then
    echo "missing pattern in $path: $pattern" >&2
    exit 1
  fi
}

require_file ".codex/INSTALL.md"
require_file ".opencode/INSTALL.md"
require_file ".opencode/plugins/entire-external-agent.js"
require_file ".cursor-plugin/plugin.json"

require_contains ".codex/INSTALL.md" ".claude/skills"
require_contains ".opencode/INSTALL.md" ".claude/skills"
require_contains ".opencode/INSTALL.md" "entire-external-agent.js"
require_contains ".opencode/plugins/entire-external-agent.js" "entire-external-agent"
require_contains ".cursor-plugin/plugin.json" "\"skills\": \"./.claude/skills/\""
require_contains ".cursor-plugin/plugin.json" "\"commands\": \"./.claude/plugins/entire-external-agent/commands/\""
require_contains "README.md" "## Installation"
require_contains "README.md" "### Codex"
require_contains "README.md" "### OpenCode"
require_contains "README.md" "### Cursor"

echo "cross-platform support surface: PASS"
