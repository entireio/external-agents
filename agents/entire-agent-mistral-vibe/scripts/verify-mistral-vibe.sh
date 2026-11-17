#!/usr/bin/env bash
# Captures real Mistral Vibe hook payloads and session files so AGENT.md can be
# checked against ground truth.
#
# Isolation: a live run uses a private VIBE_HOME inside the probe directory, so
# sessions, logs and trust state land there and the user's own hooks, plugins and
# config are not loaded. The only thing taken from the real setup is
# MISTRAL_API_KEY (from the environment, else from the real ~/.vibe/.env), which a
# live run sends to the Mistral API. The key is never printed or written to disk.
#
#   verify-mistral-vibe.sh                 static checks only (no API call)
#   verify-mistral-vibe.sh --run           headless run with a fixed prompt
#   verify-mistral-vibe.sh --manual-live   you drive vibe by hand, then press Enter
#   --clean                                delete the probe directory on success
#
# The probe directory is kept by default; it holds tool inputs and outputs.
set -euo pipefail

AGENT_NAME="Mistral Vibe"
AGENT_SLUG="mistral-vibe"
AGENT_BIN="${VIBE_BIN:-vibe}"
REAL_VIBE_HOME="${VIBE_HOME:-$HOME/.vibe}"
PROBE_PARENT="${PROBE_PARENT:-${TMPDIR:-/tmp}/${AGENT_SLUG}-probe}"
EVENTS=(session_start pre_agent pre_tool post_tool post_agent notification session_end)
# The reply marker is assembled by the model, so it never appears in the prompt.
REPLY_MARKER="ZEBRA4412"
PROMPT="Create a file named probe-entire.txt containing the text PROBE-FILE-7391. Then reply with exactly one word: the word formed by joining 'ZEB' and 'RA4412' with nothing between them."

MODE="static"
CLEAN=0
for arg in "$@"; do
  case "$arg" in
    --run) MODE="run" ;;
    --manual-live) MODE="manual" ;;
    --clean) CLEAN=1 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

FAILED=0
verdict() {
  printf '%-5s %s\n' "$1" "$2"
  if [ "$1" = "FAIL" ]; then FAILED=1; fi
}

echo "== Static checks: ${AGENT_NAME} =="
if ! command -v "$AGENT_BIN" >/dev/null 2>&1; then
  verdict FAIL "binary '${AGENT_BIN}' not on PATH"
  exit 1
fi
verdict PASS "binary: $(command -v "$AGENT_BIN")"
verdict PASS "version: $("$AGENT_BIN" --version 2>&1 | head -1)"
HELP="$("$AGENT_BIN" --help 2>&1 || true)"
for kw in '--resume' '--continue' '--output-dir' '--trust'; do
  if grep -q -- "$kw" <<<"$HELP"; then verdict PASS "help mentions $kw"; else verdict WARN "help lacks $kw"; fi
done

[ "$MODE" = "static" ] && exit 0

mkdir -p "$PROBE_PARENT"
PROBE_DIR="$(mktemp -d "$PROBE_PARENT/run-XXXXXXXX")"
chmod 700 "$PROBE_DIR"
case "$PROBE_DIR" in
  *[\"\\\$\`]*) echo "probe path has characters unsafe for TOML/shell: $PROBE_DIR" >&2; exit 2 ;;
esac
REPO="$PROBE_DIR/repo"
CAPTURES="$PROBE_DIR/captures"
PRIVATE_HOME="$PROBE_DIR/vibe-home"
mkdir -p "$REPO/.vibe" "$CAPTURES" "$PRIVATE_HOME"
git -C "$REPO" init -q
echo "probe dir: $PROBE_DIR"

# Records each payload with receive/finish times so ordering and overlap show up.
cat > "$PROBE_DIR/capture.py" <<'PY'
import json, os, sys, time
event, out_dir = sys.argv[1], sys.argv[2]
start = time.time_ns()
raw = sys.stdin.read()
try:
    payload, valid = json.loads(raw), True
except ValueError:
    payload, valid = raw, False
record = {
    "event": event, "recv_ns": start, "pid": os.getpid(), "valid_json": valid,
    "env": {k: v for k, v in os.environ.items() if k.startswith(("VIBE_", "PLUGIN_"))},
    "payload": payload,
}
record["done_ns"] = time.time_ns()
with open(os.path.join(out_dir, f"{start}-{event}-{os.getpid()}.json"), "w") as fh:
    json.dump(record, fh, indent=2)
PY

{
  for ev in "${EVENTS[@]}"; do
    printf '[[hooks]]\nname = "probe-%s"\ntype = "%s"\n' "$ev" "$ev"
    printf "command = 'python3 -I \"%s/capture.py\" %s \"%s\"'\n\n" "$PROBE_DIR" "$ev" "$CAPTURES"
  done
} > "$REPO/.vibe/hooks.toml"

# Key source: MISTRAL_API_KEY from the environment if set; with KEY_SOURCE=envfile,
# the real ~/.vibe/.env; otherwise nothing is injected and Vibe resolves the key
# itself (for example from the OS keyring, which may show an approval prompt).
if [ -z "${MISTRAL_API_KEY:-}" ] && [ "${KEY_SOURCE:-}" = "envfile" ] && [ -f "$REAL_VIBE_HOME/.env" ]; then
  MISTRAL_API_KEY="$(sed -n 's/^MISTRAL_API_KEY=//p' "$REAL_VIBE_HOME/.env" | head -1 | tr -d "\"'")"
fi
KEY_ENV=()
if [ -n "${MISTRAL_API_KEY:-}" ]; then
  KEY_ENV=("MISTRAL_API_KEY=$MISTRAL_API_KEY")
  echo "key source: MISTRAL_API_KEY"
else
  echo "key source: resolved by Vibe"
fi

RUN_RC=0
if [ "$MODE" = "run" ]; then
  echo "== Headless run (private VIBE_HOME) =="
  env VIBE_HOME="$PRIVATE_HOME" ${KEY_ENV[@]+"${KEY_ENV[@]}"} \
    "$AGENT_BIN" ${VIBE_MODEL:+--model "$VIBE_MODEL"} \
    --workdir "$REPO" --trust --auto-approve --max-turns 6 --output json \
    --output-dir "$PROBE_DIR/export" -p "$PROMPT" \
    > "$PROBE_DIR/stdout.json" 2> "$PROBE_DIR/stderr.txt" || RUN_RC=$?
  if [ "$RUN_RC" -eq 0 ]; then verdict PASS "vibe exit 0"; else verdict FAIL "vibe exit $RUN_RC (see $PROBE_DIR/stderr.txt)"; fi
else
  echo "Run this in another terminal, send a prompt or two, quit, then press Enter:"
  echo "  VIBE_HOME=\"$PRIVATE_HOME\" MISTRAL_API_KEY=... $AGENT_BIN --workdir \"$REPO\" --trust"
  read -r _
fi

echo "== Captures (in arrival order) =="
python3 -I - "$CAPTURES" "${EVENTS[@]}" <<'PY' || FAILED=1
import json, os, sys
cap, events = sys.argv[1], sys.argv[2:]
recs = [json.load(open(os.path.join(cap, f))) for f in sorted(os.listdir(cap))]
t0 = recs[0]["recv_ns"] if recs else 0
for r in recs:
    p = r["payload"] if r["valid_json"] else {}
    extra = {k: v for k, v in p.items() if k in ("source", "reason", "agent_name", "tool_name", "notification_type", "tool_status", "parent_session_id")}
    print(f'+{(r["recv_ns"]-t0)/1e6:9.1f}ms {r["event"]:<14} valid={r["valid_json"]} keys={sorted(p)} {extra}')
bad = [r for r in recs if not r["valid_json"]]
missing = [e for e in events if e not in {r["event"] for r in recs}]
for e in events:
    print(("WARN " if e in missing else "PASS ") + e + (": no payload" if e in missing else ""))
sys.exit(1 if bad else 0)
PY

echo "== Files written under the private VIBE_HOME =="
( cd "$PRIVATE_HOME" && find . -type f | sort | sed -n '1,80p' )

echo "== Where the assistant reply ($REPLY_MARKER) is stored =="
grep -rl "$REPLY_MARKER" "$PRIVATE_HOME" "$PROBE_DIR/export" "$PROBE_DIR/stdout.json" "$CAPTURES" 2>/dev/null \
  | sed "s#$PROBE_DIR/##" || verdict WARN "reply marker not found anywhere"

if [ "$FAILED" -eq 0 ] && [ "$CLEAN" -eq 1 ]; then
  rm -rf "$PROBE_DIR"
  echo "removed $PROBE_DIR"
else
  echo "kept: $PROBE_DIR"
fi
exit "$FAILED"
