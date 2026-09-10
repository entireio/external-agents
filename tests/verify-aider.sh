#!/usr/bin/env bash

set -euo pipefail

if ! command -v aider >/dev/null 2>&1; then
  echo "aider: NOT INSTALLED (static verification only)"
  exit 0
fi

version="$(aider --version 2>/dev/null || true)"
help_text="$(aider --help 2>&1 || true)"

if [[ -z "$version" ]]; then
  echo "aider version: NOT VERIFIED"
else
  printf 'aider version: %s\n' "$version"
fi

for flag in --message --message-file --yes --attribute-author --attribute-committer --attribute-co-authored-by --chat-history-file --restore-chat-history; do
  if grep -Fq -- "$flag" <<<"$help_text"; then
    printf '%s: PASS\n' "$flag"
  else
    printf '%s: NOT VERIFIED\n' "$flag"
  fi
done

if grep -Fq -- '.aider.chat.history.md' <<<"$help_text"; then
  echo 'default markdown chat history: PASS'
else
  echo 'default markdown chat history: NOT VERIFIED'
fi

echo 'No live Aider session was started; no files, commits, or working trees were modified.'
