#!/usr/bin/env bash
# verify-zcode.sh — capture real ZCode hook payloads and verify session storage.
#
# ZCode is an Electron desktop app, so this script cannot launch a session
# itself. Use --manual-live: it wires hook captures into the USER config
# (~/.zcode/cli/config.json — the only executed config source; workspace
# configs are ignored), then you interact with ZCode normally, press Enter
# here, and it pretty-prints every captured payload and restores the config.
set -euo pipefail

AGENT_NAME="ZCode"
AGENT_SLUG="zcode"
CONFIG="$HOME/.zcode/cli/config.json"
PROBE_DIR="$(cd "$(dirname "$0")" && pwd)/.probe-${AGENT_SLUG}-$(date +%s)"
CAPTURES="$PROBE_DIR/captures"
MARKER="entire-agent-zcode-probe"
ENTIRE_BIN="${E2E_ENTIRE_BIN:-$(command -v entire || true)}"

mkdir -p "$CAPTURES"

on_exit() { restore_config; }
trap on_exit EXIT

backup_config() {
  if [[ -f "$CONFIG" ]]; then
    cp "$CONFIG" "$PROBE_DIR/config.json.bak"
  fi
}

restore_config() {
  if [[ -f "$PROBE_DIR/config.json.bak" ]]; then
    cp "$PROBE_DIR/config.json.bak" "$CONFIG"
    echo "[cleanup] restored original config"
  fi
}

write_hook() { # <EventName>
  cat > "$CAPTURES/hook-$1.json.dump.sh" <<EOF
#!/usr/bin/env bash
cat > "$CAPTURES/$1-\$(date +%s%N).json"
EOF
  chmod +x "$CAPTURES/hook-$1.json.dump.sh"
}

install_hooks() {
  backup_config
  mkdir -p "$(dirname "$CONFIG")"
  for ev in SessionStart UserPromptSubmit PreToolUse PostToolUse Stop; do
    write_hook "$ev"
  done
  python3 - "$CONFIG" "$CAPTURES" <<'EOF'
import json, os, sys
config_path, captures = sys.argv[1], sys.argv[2]
config = {}
if os.path.exists(config_path):
    with open(config_path) as f:
        config = json.load(f)
hooks = config.setdefault("hooks", {})
hooks["enabled"] = True
events = hooks.setdefault("events", {})
for ev in ("SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop"):
    groups = events.setdefault(ev, [])
    if not any("entire-agent-zcode-probe" in json.dumps(g) for g in groups):
        groups.append({
            "hooks": [{
                "type": "process",
                "command": "/usr/bin/env",
                "args": ["bash", os.path.join(captures, f"hook-{ev}.json.dump.sh")],
                "statusMessage": "entire-agent-zcode probe: " + ev,
                "entire-agent-zcode-probe": True,
            }]
        })
with open(config_path, "w") as f:
    json.dump(config, f, indent=2)
print("[hooks] capture hooks installed (marker: entire-agent-zcode-probe)")
EOF
}

static_checks() {
  echo "== Static checks =="
  if command -v "$AGENT_SLUG" >/dev/null 2>&1; then
    echo "PASS binary present: $(command -v $AGENT_SLUG)"
  else
    echo "FAIL binary not on PATH"
  fi
  if [[ -d "$HOME/.zcode/cli/db" ]]; then
    echo "PASS db dir: $HOME/.zcode/cli/db"
    if command -v sqlite3 >/dev/null 2>&1; then
      sqlite3 "$HOME/.zcode/cli/db/db.sqlite" \
        "select id, title, datetime(time_created/1000,'unixepoch') from session order by time_created desc limit 5;"
    fi
  else
    echo "WARN no ~/.zcode/cli/db — has a session ever run?"
  fi
  if compgen -G "$HOME/.zcode/cli/rollout/model-io-sess_*.jsonl" >/dev/null; then
    echo "PASS rollout logs present"
  else
    echo "WARN no rollout/model-io logs"
  fi
}

collect() {
  echo "== Captured payloads =="
  local found=0
  for f in "$CAPTURES"/*.json; do
    [[ -e "$f" ]] || continue
    found=1
    echo "--- $f"
    python3 -m json.tool "$f" 2>/dev/null || cat "$f"
  done
  if [[ $found -eq 0 ]]; then
    echo "WARN no payloads captured"
  fi
}

verdict() {
  echo "== Verdict =="
  for ev in SessionStart UserPromptSubmit PreToolUse PostToolUse Stop; do
    if compgen -G "$CAPTURES/$ev-"*.json >/dev/null; then
      echo "PASS $ev payload captured"
    else
      echo "WARN $ev: no payload"
    fi
  done
}

case "${1:-}" in
  --manual-live)
    static_checks
    install_hooks
    echo
    echo "[live] Now open ZCode, start a NEW session, submit a prompt that"
    echo "       reads a file, and wait for the response to finish."
    read -r -p "Press Enter when done (config will be restored)... "
    collect
    verdict
    ;;
  *)
    static_checks
    echo
    echo "Run with --manual-live to capture hook payloads from a real ZCode session."
    ;;
esac
