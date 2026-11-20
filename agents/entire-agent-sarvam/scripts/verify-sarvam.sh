#!/usr/bin/env bash
# verify-sarvam.sh — capture real sarvam-code lifecycle hook payloads.
#
# Probes the sarvam-code CLI non-destructively: it builds a probe project with
# capture hooks for all eight events, grants it directory trust (backing up and
# restoring the user config), and optionally drives a prompt. Because sarvam
# gates project hooks behind an interactive /hooks review, the script pauses
# for that one-time review when run with --manual-live.
#
# Usage:
#   scripts/verify-sarvam.sh --run-cmd 'prompt text'   # headless exec run
#   scripts/verify-sarvam.sh --manual-live             # you drive the TUI
#   scripts/verify-sarvam.sh --check                   # static checks only
set -euo pipefail

AGENT_NAME="Sarvam Code"
AGENT_SLUG="sarvam"
AGENT_BIN="sarvam-code"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
PROBE_DIR="${PROBE_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/sarvam-probe-XXXXXX")}"
CAPTURE_DIR="$PROBE_DIR/captures"
USER_CONFIG="$HOME/.sarvam/config.toml"
USER_CONFIG_BACKUP=""

MODE="check"
PROMPT=""
for arg in "$@"; do
  case "$arg" in
    --run-cmd) MODE="run"; shift; PROMPT="${1:-}"; ;;
    --manual-live) MODE="manual"; ;;
    --check) MODE="check"; ;;
    *) ;;
  esac
done

events=(SessionStart UserPromptSubmit PreToolUse PostToolUse PermissionRequest PreCompact PostCompact Stop)

log() { printf '\033[1;34m[verify-sarvam]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[verify-sarvam]\033[0m %s\n' "$*" >&2; }

# --- Static checks ----------------------------------------------------------
log "static checks"
STATIC_FAIL=0
if command -v "$AGENT_BIN" >/dev/null 2>&1; then
  log "binary present: $(command -v "$AGENT_BIN")"
else
  fail "binary $AGENT_BIN not found on PATH"
  STATIC_FAIL=1
fi
if "$AGENT_BIN" --version >/dev/null 2>&1; then
  log "version: $("$AGENT_BIN" --version 2>&1 | head -1)"
else
  fail "$AGENT_BIN --version failed"
  STATIC_FAIL=1
fi
[ -d "$HOME/.sarvam" ] && log "config dir: $HOME/.sarvam" || { fail "no ~/.sarvam"; STATIC_FAIL=1; }

if [ "$MODE" = "check" ]; then
  [ "$STATIC_FAIL" = "0" ] && log "PASS: static checks" || fail "FAIL: static checks"
  exit "$STATIC_FAIL"
fi

# --- Hook wiring (non-destructive) -----------------------------------------
log "probe dir: $PROBE_DIR"
mkdir -p "$CAPTURE_DIR"

write_probe_config() {
  cat > "$PROBE_DIR/.sarvam/config.toml" <<'EOF'
[[hooks.SessionStart]]
[[hooks.SessionStart.hooks]]
type = "command"
command = "sh -c 'cat > CAPTURES/SessionStart.json'"

[[hooks.UserPromptSubmit]]
[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = "sh -c 'cat > CAPTURES/UserPromptSubmit.json'"

[[hooks.PreToolUse]]
matcher = ".*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "sh -c 'cat > CAPTURES/PreToolUse.json'"

[[hooks.PostToolUse]]
matcher = ".*"
[[hooks.PostToolUse.hooks]]
type = "command"
command = "sh -c 'cat > CAPTURES/PostToolUse.json'"

[[hooks.PermissionRequest]]
[[hooks.PermissionRequest.hooks]]
type = "command"
command = "sh -c 'cat > CAPTURES/PermissionRequest.json'"

[[hooks.PreCompact]]
[[hooks.PreCompact.hooks]]
type = "command"
command = "sh -c 'cat > CAPTURES/PreCompact.json'"

[[hooks.PostCompact]]
[[hooks.PostCompact.hooks]]
type = "command"
command = "sh -c 'cat > CAPTURES/PostCompact.json'"

[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "sh -c 'cat > CAPTURES/Stop.json'"
EOF
  # Expand the capture dir placeholder.
  sed -i.bak "s|CAPTURES|$CAPTURE_DIR|g" "$PROBE_DIR/.sarvam/config.toml"
  rm -f "$PROBE_DIR/.sarvam/config.toml.bak"
}

backup_user_config() {
  if [ -f "$USER_CONFIG" ]; then
    USER_CONFIG_BACKUP="$(mktemp)"
    cp "$USER_CONFIG" "$USER_CONFIG_BACKUP"
  fi
}

restore_user_config() {
  if [ -n "$USER_CONFIG_BACKUP" ]; then
    cp "$USER_CONFIG_BACKUP" "$USER_CONFIG"
    rm -f "$USER_CONFIG_BACKUP"
    USER_CONFIG_BACKUP=""
  fi
}

grant_trust() {
  # Trust the probe dir so the project layer (hooks) loads. Mirrors what the
  # interactive first-run prompt records. Backed up; restored on cleanup.
  backup_user_config
  local canonical
  canonical="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$PROBE_DIR")"
  mkdir -p "$HOME/.sarvam"
  touch "$USER_CONFIG"
  if ! grep -qF "[projects.\"$canonical\"]" "$USER_CONFIG"; then
    printf '\n[projects."%s"]\ntrust_level = "trusted"\n' "$canonical" >> "$USER_CONFIG"
    log "granted directory trust for $canonical (user config backed up)"
  fi
}

cleanup() {
  restore_user_config
  log "cleanup done (probe dir kept at $PROBE_DIR; remove manually if unwanted)"
}
trap cleanup EXIT

write_probe_config
grant_trust

# --- Run modes --------------------------------------------------------------
case "$MODE" in
  run)
    log "headless run: $AGENT_BIN exec (note: exec mode does not fire hooks; kept to document that)"
    "$AGENT_BIN" exec --skip-git-repo-check -C "$PROBE_DIR" "${PROMPT:-Reply with the word ready}" || true
    ;;
  manual)
    log "manual-live: start the TUI yourself in another terminal:"
    log "  cd $PROBE_DIR && $AGENT_BIN"
    log "1) If asked, trust the directory."
    log "2) Run /hooks and review + enable every hook (t then space, per hook)."
    log "3) Send a prompt that uses a tool, e.g.: create hello.txt with hello"
    log "4) Optionally /compact to capture compaction events."
    log "Press Enter here when you are done."
    read -r _
    ;;
esac

# --- Capture collection ------------------------------------------------------
PASS=0; WARN=0; FAIL=0
for ev in "${events[@]}"; do
  f="$CAPTURE_DIR/$ev.json"
  if [ -s "$f" ]; then
    log "PASS  $ev: $(head -c 200 "$f")"
    PASS=$((PASS+1))
  else
    log "WARN  $ev: no payload captured"
    WARN=$((WARN+1))
  fi
done

log "verdict: $PASS captured, $WARN missing, session artifacts under $CAPTURE_DIR"
if [ "$PASS" -gt 0 ]; then
  log "copy captures into the module for fixtures:"
  log "  cp $CAPTURE_DIR/*.json $PROJECT_DIR/testdata/hook-payloads/"
fi
exit 0
