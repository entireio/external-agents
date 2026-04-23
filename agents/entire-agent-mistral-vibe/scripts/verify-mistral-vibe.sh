#!/usr/bin/env bash
set -euo pipefail

AGENT_NAME="Mistral Vibe"
AGENT_SLUG="mistral-vibe"
AGENT_BIN="vibe"
PROBE_DIR="${PROBE_DIR:-$(pwd)/.probe-${AGENT_SLUG}-$(date +%s)}"
PASS=0
WARN=0
FAIL=0

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC}: $1"; PASS=$((PASS + 1)); }
warn() { echo -e "${YELLOW}WARN${NC}: $1"; WARN=$((WARN + 1)); }
fail() { echo -e "${RED}FAIL${NC}: $1"; FAIL=$((FAIL + 1)); }

echo "=== ${AGENT_NAME} Verification Script ==="
echo "Probe directory: ${PROBE_DIR}"
echo ""

# ── Section 1: Static Checks ──────────────────────────────────────────

echo "── Static Checks ──"

# Binary present
if command -v "${AGENT_BIN}" &>/dev/null; then
    VIBE_PATH=$(command -v "${AGENT_BIN}")
    pass "Binary found at ${VIBE_PATH}"
else
    fail "Binary '${AGENT_BIN}' not found in PATH"
    echo "Install with: uv tool install mistral-vibe"
    exit 1
fi

# Version
if VERSION=$("${AGENT_BIN}" --version 2>&1); then
    pass "Version: ${VERSION}"
else
    warn "Could not get version"
fi

# Help output
if "${AGENT_BIN}" --help &>/dev/null; then
    pass "Help output available"
else
    warn "No help output"
fi

# Config directory
VIBE_HOME="${VIBE_HOME:-${HOME}/.vibe}"
if [ -d "${VIBE_HOME}" ]; then
    pass "Config directory exists at ${VIBE_HOME}"
else
    warn "Config directory not found at ${VIBE_HOME}"
fi

# Session log directory
SESSION_DIR="${VIBE_HOME}/logs/session"
if [ -d "${SESSION_DIR}" ]; then
    SESSION_COUNT=$(find "${SESSION_DIR}" -maxdepth 1 -name "session_*" -type d 2>/dev/null | wc -l | tr -d ' ')
    pass "Session log directory exists with ${SESSION_COUNT} sessions"
else
    warn "Session log directory not found at ${SESSION_DIR} (created on first use)"
fi

# API key
if [ -n "${MISTRAL_API_KEY:-}" ]; then
    pass "MISTRAL_API_KEY is set"
else
    fail "MISTRAL_API_KEY is not set — required for programmatic mode"
fi

# Programmatic mode check
HELP_OUTPUT=$("${AGENT_BIN}" --help 2>&1)
if echo "${HELP_OUTPUT}" | grep -q "\-p.*programmatic"; then
    pass "Programmatic mode (-p) available"
else
    warn "Programmatic mode flag not found in help"
fi

if echo "${HELP_OUTPUT}" | grep -q "\-\-resume"; then
    pass "Resume flag (--resume) available"
else
    warn "Resume flag not found in help"
fi

if echo "${HELP_OUTPUT}" | grep -q "\-\-output.*json"; then
    pass "JSON output mode available"
else
    warn "JSON output mode not found in help"
fi

echo ""

# ── Section 2: Session Storage Verification ────────────────────────────

echo "── Session Storage Verification ──"

if [ -d "${SESSION_DIR}" ]; then
    # Find most recent session
    LATEST=$(find "${SESSION_DIR}" -maxdepth 1 -name "session_*" -type d 2>/dev/null | sort -r | head -1)
    if [ -n "${LATEST}" ]; then
        pass "Latest session: $(basename "${LATEST}")"

        # Check meta.json
        if [ -f "${LATEST}/meta.json" ]; then
            pass "meta.json exists"
            # Extract session_id
            SESSION_ID=$(python3 -c "import json; print(json.load(open('${LATEST}/meta.json'))['session_id'])" 2>/dev/null || echo "")
            if [ -n "${SESSION_ID}" ]; then
                pass "Session ID from meta.json: ${SESSION_ID}"
            else
                warn "Could not extract session_id from meta.json"
            fi
        else
            warn "meta.json not found in latest session"
        fi

        # Check messages.jsonl
        if [ -f "${LATEST}/messages.jsonl" ]; then
            LINE_COUNT=$(wc -l < "${LATEST}/messages.jsonl" | tr -d ' ')
            pass "messages.jsonl exists with ${LINE_COUNT} lines"

            # Verify JSONL format
            if python3 -c "
import json
with open('${LATEST}/messages.jsonl') as f:
    for line in f:
        msg = json.loads(line)
        assert 'role' in msg, 'missing role'
print('valid')
" 2>/dev/null; then
                pass "messages.jsonl has valid JSONL format with role fields"
            else
                warn "messages.jsonl format validation failed"
            fi

            # Check for actual content (not placeholders)
            if python3 -c "
import json
with open('${LATEST}/messages.jsonl') as f:
    for line in f:
        msg = json.loads(line)
        if msg.get('role') == 'assistant' and msg.get('content'):
            if len(msg['content']) > 20:
                print('has_content')
                exit(0)
print('no_content')
exit(1)
" 2>/dev/null; then
                pass "Assistant messages contain actual content (not placeholders)"
            else
                warn "No substantial assistant content found"
            fi
        else
            warn "messages.jsonl not found in latest session"
        fi
    else
        warn "No sessions found in ${SESSION_DIR}"
    fi
else
    warn "Session directory not yet created — run vibe once to generate sessions"
fi

echo ""

# ── Section 3: Programmatic Mode Test ──────────────────────────────────

echo "── Programmatic Mode Test ──"

if [ "${1:-}" = "--run-cmd" ] && [ -n "${2:-}" ]; then
    echo "Running programmatic mode with custom command..."
    mkdir -p "${PROBE_DIR}/captures"

    PROMPT="${2}"
    WORKDIR="${3:-$(pwd)}"

    echo "Prompt: ${PROMPT}"
    echo "Workdir: ${WORKDIR}"

    # Create timestamp marker
    touch "${PROBE_DIR}/before-marker"

    # Run vibe in programmatic mode
    if OUTPUT=$("${AGENT_BIN}" -p "${PROMPT}" --output json --workdir "${WORKDIR}" 2>"${PROBE_DIR}/captures/stderr.txt"); then
        echo "${OUTPUT}" > "${PROBE_DIR}/captures/programmatic-output.json"
        pass "Programmatic mode completed successfully"

        # Check output format
        if python3 -c "import json; json.loads(open('${PROBE_DIR}/captures/programmatic-output.json').read())" 2>/dev/null; then
            pass "Output is valid JSON"
        else
            warn "Output is not valid JSON"
        fi
    else
        fail "Programmatic mode failed (exit code $?)"
        if [ -f "${PROBE_DIR}/captures/stderr.txt" ]; then
            echo "stderr: $(cat "${PROBE_DIR}/captures/stderr.txt")"
        fi
    fi

    # Find new session files
    if [ -d "${SESSION_DIR}" ]; then
        NEW_SESSIONS=$(find "${SESSION_DIR}" -maxdepth 1 -name "session_*" -type d -newer "${PROBE_DIR}/before-marker" 2>/dev/null)
        if [ -n "${NEW_SESSIONS}" ]; then
            pass "New session created: $(echo "${NEW_SESSIONS}" | head -1 | xargs basename)"
        else
            warn "No new session directory detected"
        fi
    fi

elif [ "${1:-}" = "--manual-live" ]; then
    echo "Manual mode: Run vibe interactively, then press Enter when done."
    echo "Suggested test commands:"
    echo "  1. vibe -p 'What is 2+2?' --output json"
    echo "  2. vibe -p 'Create a file called test-probe.txt with hello world' --output json"
    echo ""

    mkdir -p "${PROBE_DIR}/captures"
    touch "${PROBE_DIR}/before-marker"

    read -r -p "Press Enter when done testing... "

    # Find new session files
    if [ -d "${SESSION_DIR}" ]; then
        NEW_SESSIONS=$(find "${SESSION_DIR}" -maxdepth 1 -name "session_*" -type d -newer "${PROBE_DIR}/before-marker" 2>/dev/null)
        if [ -n "${NEW_SESSIONS}" ]; then
            for SESSION in ${NEW_SESSIONS}; do
                echo ""
                echo "── New session: $(basename "${SESSION}") ──"
                if [ -f "${SESSION}/meta.json" ]; then
                    cp "${SESSION}/meta.json" "${PROBE_DIR}/captures/$(basename "${SESSION}")-meta.json"
                    echo "meta.json saved to captures"
                fi
                if [ -f "${SESSION}/messages.jsonl" ]; then
                    cp "${SESSION}/messages.jsonl" "${PROBE_DIR}/captures/$(basename "${SESSION}")-messages.jsonl"
                    echo "messages.jsonl saved to captures"
                    echo "Lines: $(wc -l < "${SESSION}/messages.jsonl" | tr -d ' ')"
                fi
            done
            pass "Captured ${NEW_SESSIONS} session(s)"
        else
            warn "No new sessions detected"
        fi
    fi
else
    echo "Skipping programmatic test (use --run-cmd '<prompt>' or --manual-live)"
fi

echo ""

# ── Verdict ────────────────────────────────────────────────────────────

echo "══════════════════════════════════════════"
echo "Results: ${PASS} PASS, ${WARN} WARN, ${FAIL} FAIL"
echo ""
echo "Verdict: PARTIAL COMPATIBILITY"
echo "  - Session management: YES (meta.json + messages.jsonl)"
echo "  - Transcript analysis: YES (structured JSONL)"
echo "  - Lifecycle hooks: NO (not supported by Vibe)"
echo "  - Programmatic mode: YES (vibe -p)"
echo "══════════════════════════════════════════"

if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
exit 0
