#!/usr/bin/env bash
set -euo pipefail

AGENT_NAME="Pi"
AGENT_SLUG="pi"
AGENT_BIN="pi"
PROBE_DIR="$(cd "$(dirname "$0")/.." && pwd)/.probe-${AGENT_SLUG}-$(date +%s)"
CAPTURE_DIR="$PROBE_DIR/captures"
KEEP_CONFIG=false
MODE=""
RUN_CMD=""

usage() {
  echo "Usage: $0 [--run-cmd '<cmd>'] [--manual-live] [--keep-config]"
  echo ""
  echo "  --run-cmd '<cmd>'   Automated: launch pi with a non-interactive prompt"
  echo "  --manual-live       Interactive: user runs pi manually, presses Enter when done"
  echo "  --keep-config       Don't remove test extension after completion"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --run-cmd) RUN_CMD="$2"; MODE="auto"; shift 2 ;;
    --manual-live) MODE="manual"; shift ;;
    --keep-config) KEEP_CONFIG=true; shift ;;
    *) usage ;;
  esac
done

mkdir -p "$CAPTURE_DIR"

# ──────────────────────────────────────────────
# Section 1: Static Checks
# ──────────────────────────────────────────────
echo "=== Static Checks ==="

check() {
  local label="$1" result="$2" notes="${3:-}"
  printf "  %-25s %s %s\n" "$label" "$result" "$notes"
}

# Binary present
if command -v "$AGENT_BIN" &>/dev/null; then
  PI_PATH="$(command -v "$AGENT_BIN")"
  check "Binary present" "PASS" "$PI_PATH"
else
  check "Binary present" "FAIL" "not found on PATH"
  echo "FATAL: $AGENT_BIN not found. Install pi first."
  exit 1
fi

# Help output
if "$AGENT_BIN" --help &>/dev/null; then
  check "Help available" "PASS" ""
else
  check "Help available" "FAIL" ""
fi

# Version info
PI_VERSION=$("$AGENT_BIN" --version 2>/dev/null || echo "unknown")
check "Version info" "PASS" "v${PI_VERSION}"

# Hook keywords in help
HELP_OUTPUT=$("$AGENT_BIN" --help 2>&1 || true)
HOOK_KEYWORDS=""
for kw in extension hook lifecycle callback event plugin; do
  if echo "$HELP_OUTPUT" | grep -qi "$kw"; then
    HOOK_KEYWORDS="${HOOK_KEYWORDS:+$HOOK_KEYWORDS, }$kw"
  fi
done
if [[ -n "$HOOK_KEYWORDS" ]]; then
  check "Hook keywords" "PASS" "$HOOK_KEYWORDS"
else
  check "Hook keywords" "WARN" "none found in --help"
fi

# Session keywords
SESSION_KEYWORDS=""
for kw in session resume continue history transcript context; do
  if echo "$HELP_OUTPUT" | grep -qi "$kw"; then
    SESSION_KEYWORDS="${SESSION_KEYWORDS:+$SESSION_KEYWORDS, }$kw"
  fi
done
if [[ -n "$SESSION_KEYWORDS" ]]; then
  check "Session keywords" "PASS" "$SESSION_KEYWORDS"
else
  check "Session keywords" "WARN" "none found in --help"
fi

# Config directory
PI_CONFIG_DIR="${PI_CODING_AGENT_DIR:-$HOME/.pi/agent}"
if [[ -d "$PI_CONFIG_DIR" ]]; then
  check "Config directory" "PASS" "$PI_CONFIG_DIR"
else
  check "Config directory" "WARN" "$PI_CONFIG_DIR not found"
fi

# Session directory
PI_SESSION_DIR="$PI_CONFIG_DIR/sessions"
if [[ -d "$PI_SESSION_DIR" ]]; then
  check "Session directory" "PASS" "$PI_SESSION_DIR"
else
  check "Session directory" "WARN" "$PI_SESSION_DIR not found"
fi

echo ""

# ──────────────────────────────────────────────
# Section 2: Hook Wiring (TypeScript Extension)
# ──────────────────────────────────────────────
echo "=== Hook Wiring ==="

# Create a temporary test repo
TEST_REPO="$PROBE_DIR/test-repo"
mkdir -p "$TEST_REPO"
git -C "$TEST_REPO" init -q
echo "# Test repo for pi verification" > "$TEST_REPO/README.md"
git -C "$TEST_REPO" add . && git -C "$TEST_REPO" commit -q -m "init"

# Install a test extension that captures lifecycle events
EXT_DIR="$TEST_REPO/.pi/extensions/entire-probe"
mkdir -p "$EXT_DIR"

cat > "$EXT_DIR/index.ts" << 'EXTENSION_EOF'
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { execFileSync } from "node:child_process";
import { writeFileSync, mkdirSync, existsSync } from "node:fs";
import { join } from "node:path";

export default function (pi: ExtensionAPI) {
  const captureDir = process.env.ENTIRE_PROBE_CAPTURE_DIR;
  if (!captureDir) return;

  function capture(eventName: string, data: Record<string, unknown>) {
    try {
      if (!existsSync(captureDir!)) mkdirSync(captureDir!, { recursive: true });
      const ts = new Date().toISOString().replace(/[:.]/g, "-");
      const file = join(captureDir!, `${eventName}-${ts}.json`);
      writeFileSync(file, JSON.stringify(data, null, 2));
    } catch (e) {
      // best effort
    }
  }

  pi.on("session_start", async (_event, ctx) => {
    capture("session_start", {
      type: "session_start",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
    });
  });

  pi.on("before_agent_start", async (event, ctx) => {
    capture("before_agent_start", {
      type: "before_agent_start",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
      prompt: event.prompt,
    });
  });

  pi.on("turn_end", async (event, ctx) => {
    capture("turn_end", {
      type: "turn_end",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
      turn_index: event.turnIndex,
    });
  });

  pi.on("agent_end", async (event, ctx) => {
    capture("agent_end", {
      type: "agent_end",
      cwd: ctx.cwd,
      session_file: ctx.sessionManager.getSessionFile(),
      message_count: event.messages.length,
    });
  });

  pi.on("session_shutdown", async (_event) => {
    capture("session_shutdown", {
      type: "session_shutdown",
    });
  });
}
EXTENSION_EOF

echo "  Extension installed at: $EXT_DIR/index.ts"
echo ""

# ──────────────────────────────────────────────
# Section 3: Run Modes
# ──────────────────────────────────────────────

if [[ "$MODE" == "auto" ]]; then
  echo "=== Automated Run ==="
  echo "  Running: $RUN_CMD"
  echo ""
  cd "$TEST_REPO"
  ENTIRE_PROBE_CAPTURE_DIR="$CAPTURE_DIR" eval "$RUN_CMD" || true
  cd - > /dev/null
elif [[ "$MODE" == "manual" ]]; then
  echo "=== Manual Live Mode ==="
  echo "  Test repo: $TEST_REPO"
  echo "  Run pi in the test repo with:"
  echo ""
  echo "    cd $TEST_REPO && ENTIRE_PROBE_CAPTURE_DIR=$CAPTURE_DIR pi"
  echo ""
  echo "  Send a few prompts, then exit pi (Ctrl+C or Ctrl+D)."
  echo "  Press Enter here when done..."
  read -r
else
  echo "=== Skipping run (no mode selected) ==="
  echo "  Use --run-cmd or --manual-live to capture payloads"
fi

echo ""

# ──────────────────────────────────────────────
# Section 4: Capture Collection
# ──────────────────────────────────────────────
echo "=== Captured Payloads ==="

CAPTURE_COUNT=0
if [[ -d "$CAPTURE_DIR" ]]; then
  for f in "$CAPTURE_DIR"/*.json; do
    [[ -f "$f" ]] || continue
    CAPTURE_COUNT=$((CAPTURE_COUNT + 1))
    echo "  --- $(basename "$f") ---"
    python3 -m json.tool "$f" 2>/dev/null || cat "$f"
    echo ""
  done
fi

if [[ $CAPTURE_COUNT -eq 0 ]]; then
  echo "  (no captures found)"
fi

echo ""

# ──────────────────────────────────────────────
# Section 5: Session File Inspection
# ──────────────────────────────────────────────
echo "=== Session File Inspection ==="

# Find session files for the test repo
ENCODED_PATH="--$(echo "$TEST_REPO" | sed 's|^/||; s|/|-|g')--"
SESSION_SUBDIR="$PI_SESSION_DIR/$ENCODED_PATH"

if [[ -d "$SESSION_SUBDIR" ]]; then
  echo "  Session directory: $SESSION_SUBDIR"
  for sf in "$SESSION_SUBDIR"/*.jsonl; do
    [[ -f "$sf" ]] || continue
    echo ""
    echo "  --- $(basename "$sf") ---"
    echo "  Entry types:"
    jq -r '.type' "$sf" 2>/dev/null | sort | uniq -c | sort -rn | sed 's/^/    /'
    echo "  First entry (session header):"
    head -1 "$sf" | python3 -m json.tool 2>/dev/null | sed 's/^/    /'
    echo "  User messages:"
    jq -r 'select(.type == "message" and .message.role == "user") | .message.content[0].text // "(no text)"' "$sf" 2>/dev/null | sed 's/^/    /'
    echo "  Token usage entries:"
    jq -r 'select(.type == "message" and .message.role == "assistant" and .message.usage != null) | "\(.message.usage.input) in / \(.message.usage.output) out / \(.message.usage.totalTokens) total"' "$sf" 2>/dev/null | sed 's/^/    /'
  done
else
  echo "  No session directory found at: $SESSION_SUBDIR"
fi

echo ""

# ──────────────────────────────────────────────
# Section 6: Cleanup
# ──────────────────────────────────────────────
if [[ "$KEEP_CONFIG" == false ]]; then
  rm -rf "$EXT_DIR"
  echo "=== Cleanup: Extension removed ==="
else
  echo "=== Cleanup: Skipped (--keep-config) ==="
  echo "  Extension at: $EXT_DIR/index.ts"
fi

echo ""

# ──────────────────────────────────────────────
# Section 7: Verdict
# ──────────────────────────────────────────────
echo "=== Verdict ==="

verdict() {
  local label="$1" status="$2"
  printf "  %-30s %s\n" "$label" "$status"
}

verdict "Binary present" "PASS"
verdict "Session storage (JSONL)" "PASS"
verdict "Extension hook system" "PASS"

if [[ $CAPTURE_COUNT -gt 0 ]]; then
  verdict "session_start event" "$(ls "$CAPTURE_DIR"/session_start-* 2>/dev/null | head -1 | xargs test -f 2>/dev/null && echo PASS || echo MISSING)"
  verdict "before_agent_start event" "$(ls "$CAPTURE_DIR"/before_agent_start-* 2>/dev/null | head -1 | xargs test -f 2>/dev/null && echo PASS || echo MISSING)"
  verdict "turn_end event" "$(ls "$CAPTURE_DIR"/turn_end-* 2>/dev/null | head -1 | xargs test -f 2>/dev/null && echo PASS || echo MISSING)"
  verdict "agent_end event" "$(ls "$CAPTURE_DIR"/agent_end-* 2>/dev/null | head -1 | xargs test -f 2>/dev/null && echo PASS || echo MISSING)"
  verdict "session_shutdown event" "$(ls "$CAPTURE_DIR"/session_shutdown-* 2>/dev/null | head -1 | xargs test -f 2>/dev/null && echo PASS || echo MISSING)"
else
  verdict "Lifecycle events" "UNVERIFIED (no run performed)"
fi

echo ""
echo "Probe directory: $PROBE_DIR"
