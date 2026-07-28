#!/usr/bin/env bash
set -euo pipefail

# Verification script for Goose hook/lifecycle research.
#
# Wires an Entire capture plugin into a throwaway probe workspace using
# Goose's PROJECT-scope plugin discovery (<workspace>/.agents/plugins/), so
# no global Goose config (~/.config/goose, ~/.agents) is ever modified.
#
# Usage:
#   verify-goose.sh                  # automated: goose run -t <prompt> in probe workspace
#   verify-goose.sh --run-cmd '<cmd>'  # automated with a custom command (runs in workspace)
#   verify-goose.sh --manual-live    # you drive goose manually in the workspace
#   verify-goose.sh --keep           # keep the probe dir after the run

AGENT_NAME="Goose"
AGENT_SLUG="goose"
AGENT_BIN="goose"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PROJECT_DIR=$(dirname "$SCRIPT_DIR")
PROBE_DIR="$PROJECT_DIR/.probe-${AGENT_SLUG}-$(date +%s)"
WORKSPACE="$PROBE_DIR/workspace"
CAPTURE_DIR="$PROBE_DIR/captures"
PLUGIN_DIR="$WORKSPACE/.agents/plugins/entire-verify"

MODE="auto"
RUN_CMD=""
KEEP="false"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --run-cmd) RUN_CMD="$2"; shift 2 ;;
    --manual-live) MODE="manual"; shift ;;
    --keep) KEEP="true"; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

pass=0; warn=0; fail=0
check() { # check <PASS|WARN|FAIL> <label> [detail]
  local status="$1" label="$2" detail="${3:-}"
  case "$status" in
    PASS) pass=$((pass+1)) ;;
    WARN) warn=$((warn+1)) ;;
    FAIL) fail=$((fail+1)) ;;
  esac
  printf '%-4s | %-28s | %s\n' "$status" "$label" "$detail"
}

echo "== 1. Static checks =="
if command -v "$AGENT_BIN" >/dev/null 2>&1; then
  check PASS "binary present" "$(command -v "$AGENT_BIN")"
else
  check FAIL "binary present" "goose not on PATH"
  exit 1
fi
VERSION=$("$AGENT_BIN" --version 2>&1 | tr -d '[:space:]' || true)
[[ -n "$VERSION" ]] && check PASS "version" "$VERSION" || check WARN "version" "no output"
HELP=$("$AGENT_BIN" --help 2>&1 || true)
grep -qi "session" <<<"$HELP" && check PASS "session keywords in help" "session" || check WARN "session keywords" "none"
grep -qi "run" <<<"$HELP" && check PASS "headless run command" "goose run" || check WARN "headless run" "not found"

DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/goose"
SESSIONS_DB="$DATA_DIR/sessions/sessions.db"
[[ -f "$SESSIONS_DB" ]] && check PASS "sessions.db present" "$SESSIONS_DB" || check WARN "sessions.db" "not found at $SESSIONS_DB (created on first session)"

echo
echo "== 2. Hook wiring (project-scope plugin, non-destructive) =="
mkdir -p "$PLUGIN_DIR/hooks" "$CAPTURE_DIR" "$WORKSPACE"

cat > "$PLUGIN_DIR/capture.sh" <<EOF
#!/bin/sh
# Dump hook stdin JSON to the capture dir, named by event. Always exit 0 so
# blocking hooks (Stop) allow the agent to proceed.
payload=\$(cat)
event=\$(printf '%s' "\$payload" | sed -n 's/.*"event"[[:space:]]*:[[:space:]]*"\\([^"]*\\)".*/\\1/p')
printf '%s' "\$payload" > "$CAPTURE_DIR/\${event:-unknown}-\$(date +%s%N).json"
exit 0
EOF
chmod +x "$PLUGIN_DIR/capture.sh"

cat > "$PLUGIN_DIR/hooks/hooks.json" <<'EOF'
{
  "hooks": {
    "SessionStart":   [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }],
    "SessionEnd":     [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }],
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }],
    "Stop":           [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }],
    "PreToolUse":     [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }],
    "PostToolUse":    [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }],
    "PostToolUseFailure": [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }],
    "SubagentStart":  [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }],
    "SubagentStop":   [{ "hooks": [{ "type": "command", "command": "${PLUGIN_ROOT}/capture.sh" }] }]
  }
}
EOF
check PASS "capture plugin written" "$PLUGIN_DIR"

(cd "$WORKSPACE" && git init -q .) || true
echo "# probe workspace" > "$WORKSPACE/README.md"

echo
echo "== 3. Run =="
DEFAULT_PROMPT='Create a file named verify.txt containing exactly ENTIRE_VERIFY_FILE, then reply with exactly: ENTIRE_VERIFY_OK'
if [[ "$MODE" == "manual" ]]; then
  echo "Run goose manually in: $WORKSPACE"
  echo "Suggested: (cd $WORKSPACE && goose run -t '$DEFAULT_PROMPT')"
  read -r -p "Press Enter when done..."
else
  if [[ -z "$RUN_CMD" ]]; then
    RUN_CMD="goose run -t \"$DEFAULT_PROMPT\""
  fi
  echo "+ (cd $WORKSPACE && $RUN_CMD)"
  (cd "$WORKSPACE" && eval "$RUN_CMD") || check WARN "agent run" "non-zero exit"
fi

echo
echo "== 4. Captures =="
shopt -s nullglob
captures=("$CAPTURE_DIR"/*.json)
if [[ ${#captures[@]} -eq 0 ]]; then
  check FAIL "hook captures" "no payloads captured"
else
  check PASS "hook captures" "${#captures[@]} payload(s)"
  for f in "${captures[@]}"; do
    echo "--- $f"
    python3 -m json.tool "$f" 2>/dev/null || cat "$f"
  done
fi

echo
echo "== 5. Verdict =="
for event in SessionStart UserPromptSubmit PreToolUse PostToolUse Stop SessionEnd; do
  if ls "$CAPTURE_DIR/$event"-*.json >/dev/null 2>&1; then
    check PASS "event: $event" "captured"
  else
    check WARN "event: $event" "not captured"
  fi
done
echo
echo "PASS=$pass WARN=$warn FAIL=$fail"
echo "Probe dir: $PROBE_DIR"
if [[ "$KEEP" != "true" ]]; then
  echo "(probe dir kept for analysis; delete manually or rerun with cleanup)"
fi
[[ $fail -eq 0 ]]
