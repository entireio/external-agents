#!/usr/bin/env bash
set -euo pipefail

AGENT_NAME="Hermes Agent"
AGENT_SLUG="hermes"
AGENT_BIN="hermes"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd "$script_dir/.." && pwd)"
adapter_bin="$project_dir/entire-agent-hermes"
entire_bin="${ENTIRE_BIN:-entire}"
run_cmd=""
manual_live=0
keep_home=0
requested_home=""

usage() {
  cat <<'EOF'
Usage: verify-hermes.sh [--run-cmd '<command>' | --manual-live] [options]

Options:
  --run-cmd CMD      Run a non-interactive Hermes command in the disposable repo.
  --manual-live      Launch `hermes --cli` in the disposable repo.
  --hermes-home DIR  Use this explicit disposable HERMES_HOME.
  --keep-home        Keep verification artifacts after exit.
  -h, --help         Show this help.

The script refuses every HERMES_HOME beneath the invoking user's ~/.hermes.
It never reads or copies config, credentials, state.db, memory, or sessions
from the real Hermes profile.
EOF
}

while (($#)); do
  case "$1" in
    --run-cmd)
      [[ $# -ge 2 ]] || { echo "--run-cmd requires a command" >&2; exit 2; }
      run_cmd="$2"
      shift 2
      ;;
    --manual-live)
      manual_live=1
      shift
      ;;
    --hermes-home)
      [[ $# -ge 2 ]] || { echo "--hermes-home requires a directory" >&2; exit 2; }
      requested_home="$2"
      shift 2
      ;;
    --keep-home)
      keep_home=1
      shift
      ;;
    -h|--help)
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

if [[ -n "$run_cmd" && "$manual_live" -eq 1 ]]; then
  echo "choose either --run-cmd or --manual-live" >&2
  exit 2
fi

command -v "$AGENT_BIN" >/dev/null 2>&1 || {
  echo "FAIL binary present: $AGENT_BIN not found" >&2
  exit 1
}
hermes_bin="$(command -v "$AGENT_BIN")"

probe_root="$(mktemp -d "${TMPDIR:-/tmp}/entire-hermes-verify.XXXXXX")"
if [[ -n "$requested_home" ]]; then
  mkdir -p "$requested_home"
  hermes_home="$(cd "$requested_home" && pwd -P)"
else
  hermes_home="$probe_root/hermes-home"
  mkdir -p "$hermes_home"
fi

real_profile_root="$(cd "${HOME}/.hermes" 2>/dev/null && pwd -P || printf '%s' "${HOME}/.hermes")"
case "$hermes_home/" in
  "$real_profile_root/"*)
    echo "FAIL refusing real Hermes profile path: $hermes_home" >&2
    exit 1
    ;;
esac

repo_dir="$probe_root/repo"
mkdir -p "$repo_dir" "$probe_root/user-home"

cleanup() {
  if [[ "$keep_home" -eq 1 ]]; then
    echo "Artifacts kept at: $probe_root"
  else
    rm -rf -- "$probe_root"
  fi
}
trap cleanup EXIT

echo "== Static checks =="
echo "PASS binary present: $hermes_bin"
version_output="$(HERMES_HOME="$hermes_home" HOME="$probe_root/user-home" "$hermes_bin" --version 2>&1)"
echo "PASS version: $(printf '%s' "$version_output" | head -n 1)"
help_output="$(HERMES_HOME="$hermes_home" HOME="$probe_root/user-home" "$hermes_bin" --help 2>&1)"
echo "PASS help available"
if grep -Eqi 'hook|plugin|extension|callback|event' <<<"$help_output"; then
  echo "PASS hook/plugin keywords"
else
  echo "WARN hook/plugin keywords absent from top-level help"
fi
if grep -Eqi 'session|resume|continue|history|transcript|context' <<<"$help_output"; then
  echo "PASS session keywords"
else
  echo "WARN session keywords absent from top-level help"
fi

if [[ ! -x "$adapter_bin" ]]; then
  if [[ ! -f "$project_dir/go.mod" || ! -f "$project_dir/cmd/entire-agent-hermes/main.go" ]]; then
    echo "WARN adapter scaffold is not present yet; hook wiring deferred"
    echo "Summary: static Hermes checks passed; live lifecycle remains unverified."
    exit 0
  fi
  echo "Building disposable adapter binary..."
  (cd "$project_dir" && go build -o "$adapter_bin" ./cmd/entire-agent-hermes)
fi

if ! command -v "$entire_bin" >/dev/null 2>&1; then
  echo "WARN Entire binary unavailable: $entire_bin"
  echo "Summary: static Hermes checks passed; hook wiring was not run."
  exit 0
fi

git -C "$repo_dir" init -q
mkdir -p "$repo_dir/.entire"
printf '{"external_agents":true}\n' > "$repo_dir/.entire/settings.json"

echo "== Hook wiring =="
(
  cd "$repo_dir"
  PATH="$project_dir:${PATH}" \
    HERMES_HOME="$hermes_home" \
    HOME="$probe_root/user-home" \
    "$entire_bin" enable --agent hermes --telemetry=false
)

installed_json="$(cd "$repo_dir" && HERMES_HOME="$hermes_home" ENTIRE_REPO_ROOT="$repo_dir" "$adapter_bin" are-hooks-installed)"
if grep -q '"installed":true' <<<"$installed_json"; then
  echo "PASS observer plugin installed in explicit HERMES_HOME"
else
  echo "FAIL observer plugin status: $installed_json" >&2
  exit 1
fi

plugins_output="$(HERMES_HOME="$hermes_home" HOME="$probe_root/user-home" "$hermes_bin" plugins list 2>&1 || true)"
if grep -q 'entire-observer' <<<"$plugins_output"; then
  echo "PASS Hermes discovers entire-observer"
else
  echo "WARN Hermes plugin listing did not show entire-observer"
  printf '%s\n' "$plugins_output"
fi

echo "== Run mode =="
if [[ -n "$run_cmd" ]]; then
  (
    cd "$repo_dir"
    PATH="$project_dir:${PATH}" \
      HERMES_HOME="$hermes_home" \
      HOME="$probe_root/user-home" \
      bash -lc "$run_cmd"
  )
elif [[ "$manual_live" -eq 1 ]]; then
  echo "Starting an isolated interactive Hermes session. Exit it normally when done."
  (
    cd "$repo_dir"
    PATH="$project_dir:${PATH}" \
      HERMES_HOME="$hermes_home" \
      HOME="$probe_root/user-home" \
      "$hermes_bin" --cli --yolo --ignore-rules
  )
else
  echo "Running synthetic public-hook lifecycle fixture (no model or credentials)."
  VERIFY_REPO_DIR="$repo_dir" \
    HERMES_HOME="$hermes_home" \
    HOME="$probe_root/user-home" \
    python3 - <<'PY'
import importlib.util
import os
from pathlib import Path

home = Path(os.environ["HERMES_HOME"])
repo = Path(os.environ["VERIFY_REPO_DIR"])
plugin = home / "plugins" / "entire-observer" / "__init__.py"
spec = importlib.util.spec_from_file_location(
    "entire_observer_verification",
    plugin,
    submodule_search_locations=[str(plugin.parent)],
)
if spec is None or spec.loader is None:
    raise SystemExit("unable to load installed observer")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

callbacks = {}

class Context:
    def register_hook(self, name, callback):
        callbacks[name] = callback

module.register(Context())
expected = {
    "on_session_start", "pre_llm_call", "pre_tool_call", "post_tool_call",
    "post_llm_call", "on_session_end", "on_session_finalize",
}
if set(callbacks) != expected:
    raise SystemExit(f"unexpected hook set: {sorted(callbacks)}")

os.chdir(repo)
session = "fixture-session"
callbacks["on_session_start"](
    session_id=session,
    model="fixture-model",
    platform="platform-fixture-id",
)
callbacks["pre_llm_call"](
    session_id=session,
    user_message="Create hello.txt. password=hunter2",
    conversation_history=[{"role": "system", "content": "history-secret"}],
    sender_id="platform-fixture-id",
    model="fixture-model",
)
# Activation is invoked synchronously above, before this repository mutation.
callbacks["pre_tool_call"](
    session_id=session,
    tool_name="write_file",
    args={"path": "hello.txt", "content": "raw-tool-secret"},
)
(repo / "hello.txt").write_text("hello\n", encoding="utf-8")
callbacks["post_tool_call"](
    session_id=session,
    tool_name="write_file",
    args={"content": "raw-tool-secret"},
    result="raw-tool-result-secret",
    status="ok",
)
callbacks["post_llm_call"](
    session_id=session,
    assistant_response="Created hello.txt.",
    conversation_history=[{"role": "developer", "content": "history-secret"}],
    platform="platform-fixture-id",
    model="fixture-model",
)
callbacks["on_session_end"](session_id=session, completed=True)
callbacks["on_session_finalize"](session_id=session)
PY
fi

echo "== Capture collection =="
capture_root="$hermes_home/entire/transcripts"
captures=()
if [[ -d "$capture_root" ]]; then
  while IFS= read -r -d '' path; do
    captures+=("$path")
  done < <(find "$capture_root" -type f -name '*.jsonl' -print0 | sort -z)
fi

if [[ "${#captures[@]}" -eq 0 ]]; then
  echo "WARN no observer transcripts captured"
else
  for capture in "${captures[@]}"; do
    echo "CAPTURE $capture"
    if command -v jq >/dev/null 2>&1; then
      jq -c . < "$capture"
    else
      sed -n '1,240p' "$capture"
    fi
  done
  if grep -hEq 'hunter2|raw-tool-secret|raw-tool-result-secret|history-secret|platform-fixture-id' "${captures[@]}"; then
    echo "FAIL forbidden lifecycle data appeared in observer transcript" >&2
    exit 1
  fi
  echo "PASS forbidden prompts, IDs, arguments, and results were not captured"
  CAPTURE_ROOT="$capture_root" python3 - <<'PY'
import json
import os
from pathlib import Path

allowed = {"v", "type", "timestamp", "content", "model", "name", "status", "modified_files"}
for path in Path(os.environ["CAPTURE_ROOT"]).rglob("*.jsonl"):
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        value = json.loads(line)
        unknown = set(value) - allowed
        if unknown:
            raise SystemExit(f"{path}:{number}: forbidden fields {sorted(unknown)}")
print("PASS transcript entries use only the observer field allowlist")
PY
fi

echo "== Lifecycle verdict =="
for event_type in session_start user assistant tool turn_end session_end; do
  if [[ "${#captures[@]}" -gt 0 ]] && grep -hqs "\"type\":\"$event_type\"" "${captures[@]}"; then
    echo "PASS $event_type"
  else
    echo "WARN $event_type not captured"
  fi
done

echo "Summary: verification used only $hermes_home and $repo_dir; the real Hermes profile was untouched."
