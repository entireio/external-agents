#!/usr/bin/env bash
set -euo pipefail

AGENT_NAME="Amp"
AGENT_SLUG="amp"
AGENT_BIN="amp"
PROBE_DIR="$(cd "$(dirname "$0")/.." && pwd)/.probe-${AGENT_SLUG}-$(date +%s)"
CAPTURE_DIR="$PROBE_DIR/captures"
MODE=""
RUN_CMD=""

usage() {
  echo "Usage: $0 [--run-cmd '<cmd>'] [--manual-live]"
  echo "  --run-cmd '<cmd>'   Automated: launch Amp with a non-interactive prompt"
  echo "  --manual-live       Interactive: run PLUGINS=all amp manually, then press Enter"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --run-cmd) RUN_CMD="$2"; MODE="auto"; shift 2 ;;
    --manual-live) MODE="manual"; shift ;;
    *) usage ;;
  esac
done

mkdir -p "$CAPTURE_DIR"

echo "=== Static Checks ==="
if command -v "$AGENT_BIN" >/dev/null 2>&1; then
  echo "  Binary present: PASS ($(command -v "$AGENT_BIN"))"
else
  echo "  Binary present: FAIL"
  exit 1
fi
echo "  Version: $($AGENT_BIN --version 2>/dev/null || true)"

TEST_REPO="$PROBE_DIR/test-repo"
mkdir -p "$TEST_REPO/.amp/plugins"
git -C "$TEST_REPO" init -q
git -C "$TEST_REPO" config user.name "Amp Probe"
git -C "$TEST_REPO" config user.email "amp-probe@example.invalid"
touch "$TEST_REPO/README.md"
git -C "$TEST_REPO" add README.md
git -C "$TEST_REPO" commit -q -m "init"

PLUGIN="$TEST_REPO/.amp/plugins/entire-probe.ts"
cat > "$PLUGIN" <<'PLUGIN_EOF'
import type { PluginAPI, ThreadMessage, ToolCallEvent } from "@ampcode/plugin";
import { writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";

export default function (amp: PluginAPI) {
  const captureDir = process.env.ENTIRE_PROBE_CAPTURE_DIR;
  const modified = new Map<string, Set<string>>();
  function capture(eventName: string, data: Record<string, unknown>) {
    if (!captureDir) return;
    mkdirSync(captureDir, { recursive: true });
    const ts = new Date().toISOString().replace(/[:.]/g, "-");
    writeFileSync(join(captureDir, `${eventName}-${ts}.json`), JSON.stringify(data, null, 2));
  }
  function filesFor(event: ToolCallEvent): Set<string> {
    let files = modified.get(event.thread.id);
    if (!files) {
      files = new Set<string>();
      modified.set(event.thread.id, files);
    }
    return files;
  }
  amp.on("tool.call", async (event) => {
    const files = amp.helpers.filesModifiedByToolCall(event);
    if (files) for (const file of files) filesFor(event).add(amp.helpers.filePathFromURI(file));
    capture("tool.call", { thread_id: event.thread.id, tool: event.tool, input: event.input });
    return { action: "allow" };
  });
  amp.on("agent.start", (event) => {
    capture("agent.start", { thread_id: event.thread.id, message_id: String(event.id), message: event.message, cwd: process.cwd() });
  });
  amp.on("agent.end", (event) => {
    capture("agent.end", {
      thread_id: event.thread.id,
      message_id: String(event.id),
      message: event.message,
      status: event.status,
      messages: event.messages as ThreadMessage[],
      modified_files: Array.from(modified.get(event.thread.id) ?? []).sort(),
      cwd: process.cwd(),
    });
  });
}
PLUGIN_EOF

echo "=== Hook Wiring ==="
echo "  Probe plugin: $PLUGIN"

if [[ "$MODE" == "auto" ]]; then
  echo "=== Automated Run ==="
  (cd "$TEST_REPO" && ENTIRE_PROBE_CAPTURE_DIR="$CAPTURE_DIR" PLUGINS=all eval "$RUN_CMD") || true
elif [[ "$MODE" == "manual" ]]; then
  echo "=== Manual Live Mode ==="
  echo "  Run: cd $TEST_REPO && ENTIRE_PROBE_CAPTURE_DIR=$CAPTURE_DIR PLUGINS=all amp"
  read -r
else
  echo "=== Skipping run ==="
  echo "  Use --run-cmd 'amp --dangerously-allow-all --no-notifications --no-ide -x ...' or --manual-live"
fi

echo "=== Captured Payloads ==="
count=0
for file in "$CAPTURE_DIR"/*.json; do
  [[ -f "$file" ]] || continue
  count=$((count + 1))
  echo "--- $(basename "$file") ---"
  python3 -m json.tool "$file" 2>/dev/null || true
done
[[ $count -gt 0 ]] || echo "  (no captures found)"

echo "Probe directory: $PROBE_DIR"
