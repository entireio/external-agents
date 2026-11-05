#!/usr/bin/env bash
# Drive the real Entire CLI with the exact payload shape the DSH Host plugin
# emits, using a synthetic DSH v4 session. No model or live DSH prompt needed.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: verify-dsh.sh [--entire <path>] [--keep]

Builds entire-agent-dsh (if needed), creates a throwaway git repository and a
synthetic DSH session, then drives `entire hooks dsh` through a full turn.

  --entire <path>  Entire CLI binary (default: entire from PATH)
  --keep           Keep the temporary work directory and print its path
  -h, --help       Show this help
EOF
}

ENTIRE_BIN="${ENTIRE_BIN:-entire}"
KEEP=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --entire)
      [[ $# -ge 2 ]] || { echo "--entire requires a value" >&2; exit 2; }
      ENTIRE_BIN="$2"
      shift 2
      ;;
    --keep)
      KEEP=1
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

for command in git node jq; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "SKIP: missing required command: $command" >&2
    exit 0
  }
done
if ! command -v "$ENTIRE_BIN" >/dev/null 2>&1 && [[ ! -x "$ENTIRE_BIN" ]]; then
  echo "SKIP: Entire CLI not found: $ENTIRE_BIN" >&2
  exit 0
fi
ENTIRE_BIN="$(command -v "$ENTIRE_BIN" || printf '%s' "$ENTIRE_BIN")"

if [[ ! -x "$AGENT_DIR/entire-agent-dsh" ]]; then
  command -v go >/dev/null 2>&1 || {
    echo "SKIP: entire-agent-dsh is not built and go is not installed" >&2
    exit 0
  }
  echo "Building entire-agent-dsh..."
  (cd "$AGENT_DIR" && go build -o entire-agent-dsh ./cmd/entire-agent-dsh)
fi
export PATH="$AGENT_DIR:$PATH"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/entire-dsh-verify.XXXXXX")"
cleanup() {
  if [[ "$KEEP" -eq 1 ]]; then
    echo "Kept work directory: $WORK"
  else
    rm -rf "$WORK"
  fi
}
trap cleanup EXIT

REPO="$WORK/repo"
export DSH_HOME="$WORK/dsh-home"
SESSION_ID="session-verify-$$"
mkdir -p "$REPO" "$DSH_HOME"

cd "$REPO"
git init -q
git config user.name "Entire DSH Verify"
git config user.email "dsh-verify@example.com"
printf 'initial\n' > README.md
git add README.md
git commit -q -m "initial commit"
mkdir -p .entire
printf '{"external_agents": true, "commit_linking": "always"}\n' >.entire/settings.local.json

cat >"$WORK/mksession.mjs" <<'EOF'
import fs from 'node:fs';
import path from 'node:path';
import zlib from 'node:zlib';

const [out, cwd, id, eventsFile] = process.argv.slice(2);
const events = JSON.parse(fs.readFileSync(eventsFile, 'utf8'));
const header = { type: 'session', version: 4, id, createdAt: Date.now(), cwd, isSeeded: false, delegationDepth: 0 };
const lines = [header, ...events].map((value) => JSON.stringify(value) + '\n');
const frames = lines.map((line) => zlib.zstdCompressSync(Buffer.from(line)));
fs.mkdirSync(path.dirname(out), { recursive: true });
fs.writeFileSync(out, Buffer.concat(frames));
EOF

cat >"$WORK/mkevents.mjs" <<'EOF'
import fs from 'node:fs';

const [out, prompt, file, withAssistant] = process.argv.slice(2);
const base = [
  { type: 'turn/start', seq: 0, time: Date.now(), data: { turn: 1 } },
  {
    type: 'user/message',
    seq: 1,
    time: Date.now(),
    data: {
      id: 'u1',
      role: 'user',
      content: [{ type: 'text', text: prompt }],
      source: { kind: 'user' },
    },
  },
];
if (withAssistant !== '1') {
  fs.writeFileSync(out, JSON.stringify(base));
  process.exit(0);
}
const content = JSON.stringify({ file_path: file, content: 'hello from dsh\n' });
base.push(
  {
    type: 'assistant/message',
    seq: 2,
    time: Date.now(),
    data: {
      turn: 1,
      step: 1,
      message: {
        id: 'a1',
        role: 'assistant',
        content: [
          { type: 'text', text: `Creating ${file}.` },
          { type: 'tool-call', id: 'call_1', name: 'write', arguments: content },
        ],
        source: { kind: 'model', provider: 'test', model: 'test-model' },
      },
      usage: { inputTokens: 100, outputTokens: 20, cacheReadTokens: 5, cacheWriteTokens: 0 },
    },
  },
  { type: 'tool/call', seq: 3, time: Date.now(), data: { turn: 1, step: 1, callId: 'call_1', name: 'write', arguments: content } },
  {
    type: 'tool/result',
    seq: 4,
    time: Date.now(),
    data: {
      turn: 1,
      step: 1,
      message: {
        id: 'r1',
        role: 'tool',
        content: [{ type: 'text', text: `wrote ${file}` }],
        source: { kind: 'tool', callId: 'call_1' },
        toolCallId: 'call_1',
      },
    },
  },
  { type: 'turn/end', seq: 5, time: Date.now(), data: { turn: 1, reason: 'completed' } },
);
fs.writeFileSync(out, JSON.stringify(base));
EOF

project_key() {
  node -e '
const cwd = process.argv[1];
let readable = "";
let separatorRun = false;
for (const ch of cwd) {
  if (ch === "/" || ch === "\\" || ch === ":") {
    if (!separatorRun) readable += "-";
    separatorRun = true;
  } else if (/^[A-Za-z0-9._-]$/.test(ch)) {
    readable += ch;
    separatorRun = false;
  } else {
    readable += "~" + ch.charCodeAt(0).toString(16).toUpperCase().padStart(4, "0");
    separatorRun = false;
  }
}
readable = readable.replace(/^-+/, "") || "root";
process.stdout.write("--" + readable.slice(0, 251) + "--");
' "$1"
}

PROJECT="$(project_key "$REPO")"
NATIVE="$DSH_HOME/sessions/$PROJECT/$SESSION_ID/session.v4.jsonl.zstd"
node "$WORK/mkevents.mjs" "$WORK/events1.json" "Create hello.txt" "hello.txt" 0
node "$WORK/mksession.mjs" "$NATIVE" "$REPO" "$SESSION_ID" "$WORK/events1.json"

"$ENTIRE_BIN" enable --agent dsh --telemetry=false -y >/dev/null
test -f .entire/dsh-hooks.json || {
  echo "FAIL: entire enable did not install .entire/dsh-hooks.json" >&2
  exit 1
}

printf '%s' "{\"hook_type\":\"session_start\",\"session_id\":\"$SESSION_ID\",\"timestamp\":\"2026-01-01T00:00:00Z\"}" |
  "$ENTIRE_BIN" hooks dsh session-start
printf '%s' "{\"hook_type\":\"turn_start\",\"session_id\":\"$SESSION_ID\",\"timestamp\":\"2026-01-01T00:00:01Z\",\"user_prompt\":\"Create hello.txt\",\"raw_data\":{\"dsh\":{\"turn\":1}}}" |
  "$ENTIRE_BIN" hooks dsh turn-start

printf 'hello from dsh\n' >hello.txt
node "$WORK/mkevents.mjs" "$WORK/events2.json" "Create hello.txt" "hello.txt" 1
node "$WORK/mksession.mjs" "$NATIVE" "$REPO" "$SESSION_ID" "$WORK/events2.json"
git add hello.txt
git commit -q -m "add hello.txt"
printf '%s' "{\"hook_type\":\"turn_end\",\"session_id\":\"$SESSION_ID\",\"timestamp\":\"2026-01-01T00:00:02Z\",\"raw_data\":{\"dsh\":{\"turn\":1,\"reason\":\"completed\"}}}" |
  "$ENTIRE_BIN" hooks dsh turn-end
printf '%s' "{\"hook_type\":\"session_end\",\"session_id\":\"$SESSION_ID\",\"timestamp\":\"2026-01-01T00:00:03Z\"}" |
  "$ENTIRE_BIN" hooks dsh session-end

status=0
if ! "$ENTIRE_BIN" session list --json | jq -e --arg id "$SESSION_ID" '.[] | select(.session_id == $id)' >/dev/null; then
  echo "FAIL: session $SESSION_ID is not listed by entire session list" >&2
  status=1
fi
if ! "$ENTIRE_BIN" checkpoint list | grep -q 'checkpoints'; then
  echo "FAIL: entire checkpoint list did not report a checkpoint" >&2
  status=1
fi
if ! git log -1 --format=%B | grep -q 'Entire-Checkpoint:'; then
  echo "FAIL: HEAD commit has no Entire-Checkpoint trailer" >&2
  status=1
fi

if [[ "$status" -ne 0 ]]; then
  exit "$status"
fi
echo "PASS: DSH session, checkpoint and commit trailer created"
