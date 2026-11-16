#!/usr/bin/env python3
"""
entire-agent-vibe — Agent Integration for Mistral Vibe Code and Entire.

Implements Entire's Agent Integration Protocol (protocol_version 1) for the
Mistral Vibe coding agent (VS Code extension / Vibe CLI). Entire discovers
this binary as the external agent "vibe" when it is on your PATH under the
name `entire-agent-vibe`.

Vibe session store (~ = %USERPROFILE% on Windows):
    ~/.vibe/logs/session/unified/<session-uuid>/journal/*.jsonl
        Recovery journal: core_input, projection_delta, action_intent,
        action_result, callback_* and receipt_* records (JSONL, one JSON
        object per line). The journal is a rolling window — Vibe compacts
        and deletes older chunks, so transcripts are captured at turn end.
    ~/.vibe/logs/session/unified/<session-uuid>/meta.json
        Session metadata (title, model, start/end time, working directory).

Optional diagnostics: set the environment variable VIBE_AGENT_DEBUG=1 to
record every subcommand invocation and result in ~/vibe-agent-debug.log.

Usage:  entire-agent-vibe <subcommand> [flags]
        Reads JSON on stdin where the protocol requires it, writes JSON
        to stdout.
"""
import sys
import os
import json
import glob
import base64
import time
import re
import subprocess

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
HOME = os.path.expanduser("~")
VIBE_DIR = os.path.join(HOME, ".vibe")
SESSIONS_ROOT = os.path.join(VIBE_DIR, "logs", "session")
UNIFIED_DIR = os.path.join(SESSIONS_ROOT, "unified")
INDEX_FILE = os.path.join(SESSIONS_ROOT, "unified.session-index.json")

# Where this integration persists its own AgentSession state.
# Lives inside the repo checkout, next to Entire's own data.
STATE_DIR = ".entire/vibe-agent-sessions"   # one JSON per session ID

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
def out(obj):
    """Print a JSON object to stdout and exit 0. When VIBE_AGENT_DEBUG=1 is
    set, also log the result to the debug log for troubleshooting."""
    try:
        _debug_log("result: " + json.dumps(obj)[:500])
    except Exception:
        pass
    sys.stdout.write(json.dumps(obj))
    sys.exit(0)


def read_stdin_json():
    raw = sys.stdin.read()
    if not raw.strip():
        return {}
    return json.loads(raw)


def arg_value(flag):
    """Return the value following a --flag in argv, or None."""
    argv = sys.argv[2:]
    for i, a in enumerate(argv):
        if a == flag and i + 1 < len(argv):
            return argv[i + 1]
    return None


def load_index():
    """Load the Vibe session index, or return an empty structure."""
    try:
        with open(INDEX_FILE, "r", encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return {"entries": []}


def index_entry(session_id):
    for e in load_index().get("entries", []):
        if e.get("session_id") == session_id:
            return e
    return None


def journal_dir(session_id):
    return os.path.join(UNIFIED_DIR, session_id, "journal")


def journal_chunks(session_id):
    """All journal chunk files for a session, oldest first."""
    return sorted(glob.glob(os.path.join(journal_dir(session_id), "*.jsonl")))


def full_transcript_bytes(session_id):
    """Concatenate all journal chunks into the raw transcript."""
    data = b""
    for chunk in journal_chunks(session_id):
        with open(chunk, "rb") as f:
            data += f.read()
    return data


def newest_active_session(repo_path):
    """
    Find the Vibe session whose cwd matches this repo, most recently updated.
    Used to bind the integration to the session you're actually running.
    """
    entries = load_index().get("entries", [])
    repo = os.path.normcase(os.path.abspath(repo_path))
    best, best_ts = None, -1
    for e in entries:
        meta = e.get("metadata") or {}
        cwd = meta.get("cwd") or ""
        sess = e.get("session") or {}
        if cwd and os.path.normcase(os.path.abspath(cwd)) == repo:
            ts = sess.get("updatedAt", 0)
            if ts > best_ts:
                best, best_ts = e.get("session_id"), ts
    return best

# ---------------------------------------------------------------------------
# Required commands
# ---------------------------------------------------------------------------
def cmd_info():
    out({
        "protocol_version": 1,
        "name": "vibe",
        "type": "MistralVibe",
        "description": "Mistral Vibe Code (VS Code extension / CLI) agent integration",
        "protected_dirs": [".vibe"],
        "hook_names": ["post_agent"],
        "capabilities": {
            "hooks": True,
            "transcript_analyzer": True,
            "transcript_preparer": True,     # prepare-transcript: journal -> readable transcript
            "token_calculator": True,      # Vibe's index tracks tokenUsage
            "compact_transcript": True,    # compact-transcript: -> Entire compact transcript.jsonl
            "text_generator": False,
            "hook_response_writer": False,
            "subagent_aware_extractor": False,
        },
    })


def cmd_detect():
    # Present when the Vibe agent has been used on this machine.
    # The session index file is written lazily by Vibe (it may not exist
    # yet), so presence is decided by the Vibe data directory itself.
    present = os.path.isdir(VIBE_DIR)
    out({"present": bool(present)})


def cmd_get_session_id():
    payload = read_stdin_json()
    # If the hook input carries a session id, honour it. Otherwise fall back
    # to the most recent session whose cwd matches the current repository.
    sid = payload.get("session_id")
    if not sid:
        repo = payload.get("repo_path") or os.getcwd()
        sid = newest_active_session(repo)
    out({"session_id": sid or ""})


def cmd_get_session_dir():
    repo_path = arg_value("--repo-path") or os.getcwd()
    _ = repo_path
    out({"session_dir": os.path.join(".entire", "vibe-sessions")})


def cmd_resolve_session_file():
    session_id = arg_value("--session-id")
    chunks = journal_chunks(session_id or "")
    if chunks:
        # The newest chunk is the active journal file.
        out({"session_file": chunks[-1]})
    out({"session_file": os.path.join(UNIFIED_DIR, session_id or "", "journal")})


def _session_state_path(session_id):
    sid = (session_id or "unknown").replace("/", "_").replace("\\", "_")
    return os.path.join(STATE_DIR, sid + ".json")


def _remember_repo_dir(session_id, repo_dir):
    """Persist the turn's repo cwd (from the hook payload) so later commands
    (extract-modified-files) can run git in the right worktree even though
    the journal itself lives under the user's HOME directory."""
    try:
        os.makedirs(STATE_DIR, exist_ok=True)
        with open(_session_state_path(session_id) + ".cwd", "w", encoding="utf-8") as f:
            f.write((repo_dir or "").strip())
    except OSError:
        pass


def _recall_repo_dir(session_id):
    try:
        with open(_session_state_path(session_id) + ".cwd", "r", encoding="utf-8") as f:
            d = f.read().strip()
        return d or None
    except OSError:
        return None


def _now_rfc3339():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def cmd_read_session():
    # stdin: HookInput -> return the stored AgentSession for THAT session id,
    # or build a fresh one from the hook input. AgentSession schema:
    #   session_id, agent_name, repo_path, session_ref, start_time,
    #   native_data, modified_files, new_files, deleted_files
    payload = read_stdin_json() or {}
    sid = payload.get("session_id")
    session = None
    if sid:
        path = _session_state_path(sid)
        if os.path.isfile(path):
            try:
                with open(path, "r", encoding="utf-8") as f:
                    session = json.load(f)
            except ValueError:
                session = None
    if session is None:
        session = {
            "session_id": sid,
            "agent_name": "vibe",
            "repo_path": payload.get("cwd"),
            "session_ref": payload.get("transcript_path"),
            "start_time": payload.get("timestamp") or _now_rfc3339(),
            "native_data": None,
            "modified_files": [],
            "new_files": [],
            "deleted_files": [],
            "transcript_position": 0,
        }
    else:
        # Keep session_ref pointing at the newest journal chunk.
        if payload.get("transcript_path"):
            session["session_ref"] = payload.get("transcript_path")
    out(session)


def cmd_write_session():
    session = read_stdin_json() or {}
    sid = session.get("session_id") or "unknown"
    os.makedirs(STATE_DIR, exist_ok=True)
    with open(_session_state_path(sid), "w", encoding="utf-8") as f:
        json.dump(session, f)
    sys.exit(0)


def _journal_chunks(session_ref):
    """Ordered list of journal chunk paths up to and including session_ref."""
    ref_dir = os.path.dirname(session_ref)
    ref_name = os.path.basename(session_ref)
    chunks = []
    if os.path.isdir(ref_dir):
        for chunk in sorted(glob.glob(os.path.join(ref_dir, "*.jsonl"))):
            chunks.append(chunk)
            if os.path.basename(chunk) == ref_name:
                break
    return chunks


def cmd_read_transcript():
    session_ref = arg_value("--session-ref") or ""
    chunks = _resolve_chunks(session_ref)
    # Prefer the prepared (readable) transcript if one exists.
    prepared = (chunks[-1] + ".prepared.md") if chunks else ""
    if prepared and os.path.isfile(prepared):
        with open(prepared, "rb") as f:
            sys.stdout.buffer.write(f.read())
        sys.exit(0)
    # Serve the whole journal (all chunks up to the ref chunk / the session dir).
    data = b""
    for chunk in chunks:
        try:
            with open(chunk, "rb") as f:
                data += f.read()
        except OSError:
            pass
    sys.stdout.buffer.write(data)
    sys.exit(0)


def cmd_chunk_transcript():
    max_size = int(arg_value("--max-size") or 1024 * 1024)
    raw = sys.stdin.buffer.read()
    chunks = [base64.b64encode(raw[i:i + max_size]).decode("ascii")
              for i in range(0, len(raw), max_size)] or [""]
    out({"chunks": chunks})


def cmd_reassemble_transcript():
    payload = read_stdin_json()
    data = b"".join(base64.b64decode(c) for c in payload.get("chunks", []))
    sys.stdout.buffer.write(data)
    sys.exit(0)


def cmd_format_resume_command():
    session_id = arg_value("--session-id") or ""
    out({"command": f"vibe --resume {session_id}"})

# ---------------------------------------------------------------------------
# transcript_analyzer capability
# ---------------------------------------------------------------------------
def cmd_get_transcript_position():
    path = arg_value("--path") or ""
    # Byte size of the current journal = the resume position.
    # The ref may be a session directory -> total size of all chunks.
    chunks = _resolve_chunks(path) if not os.path.isfile(path) else [path]
    pos = 0
    for chunk in chunks:
        try:
            pos += os.path.getsize(chunk)
        except OSError:
            pass
    out({"position": pos})


def _resolve_chunks(session_ref):
    """Ordered journal chunk paths for a session-ref.

    The Entire CLI passes `<session-dir>/<session-id>` — a DIRECTORY — as the
    session-ref (see get-session-dir + session id). Journal chunks live at
    `<session-dir>/journal/*.jsonl`. A ref may also be a chunk file directly.
    Returns [] when nothing is found.
    """
    if not session_ref:
        return []
    if os.path.isfile(session_ref):
        return _journal_chunks(session_ref)
    if os.path.isdir(session_ref):
        for pattern in (
            os.path.join(session_ref, "journal", "*.jsonl"),
            os.path.join(session_ref, "*.jsonl"),
        ):
            chunks = sorted(glob.glob(pattern))
            if chunks:
                return chunks
    return []


def _session_id_from_ref(session_ref):
    """Session ID from a chunk path OR a session directory ref."""
    if not session_ref:
        return None
    if os.path.isdir(session_ref):
        return os.path.basename(session_ref.rstrip("/\\"))
    return os.path.basename(os.path.dirname(session_ref))


def _parse_journal_lines(path, offset=0):
    """Return parsed JSON records from a journal chunk — or from ALL chunks
    when the ref is a session directory (offset applies per chunk)."""
    events = []
    paths = [path] if (os.path.isfile(path) or not os.path.isdir(path)) else _resolve_chunks(path)
    for p in paths:
        try:
            with open(p, "r", encoding="utf-8", errors="replace") as f:
                f.seek(offset)
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        events.append(json.loads(line))
                    except ValueError:
                        continue
        except OSError:
            pass
    return events


def cmd_extract_prompts():
    # --session-ref <path> --offset <n>
    # Journal format: one JSON record per line, top-level "type".
    # User prompts: type == "core_input" and payload.method == "turn/start",
    # text at payload.params.message[] (list of {"type": "text", "text": "..."}).
    session_ref = arg_value("--session-ref") or ""
    offset = int(arg_value("--offset") or 0)
    prompts = []

    for rec in _parse_journal_lines(session_ref, offset):
        # turn/start records may carry different top-level "type" values
        # (core_input / action_intent / command_reserved) — match on the
        # payload shape instead of filtering by record type.
        payload = rec.get("payload") or {}
        if not isinstance(payload, dict) or payload.get("method") != "turn/start":
            continue
        params = payload.get("params") or {}
        if params.get("injected"):
            continue  # synthetic/injected messages are not user prompts
        for part in params.get("message") or []:
            if isinstance(part, dict) and part.get("type") == "text":
                text = (part.get("text") or "").strip()
                if text:
                    prompts.append(text)

    # Fallback: the index preview (first prompt) if the journal had none.
    if not prompts:
        sid = _session_id_from_ref(session_ref)
        entry = index_entry(sid) if sid else None
        if entry:
            preview = (entry.get("session") or {}).get("preview")
            if preview:
                prompts.append(preview)

    out({"prompts": prompts})


def _git_changed_files(base_dir=None):
    """Paths that Git considers changed (staged/unstaged/untracked) right now.
    base_dir: directory to run git in (journal dir), falls back to cwd.
    Returns None when Git is unavailable."""
    try:
        cmd = ["git"]
        if base_dir:
            cmd += ["-C", base_dir]
        cmd += ["status", "--porcelain", "--untracked-files=all", "--no-renames"]
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=10)
    except Exception:
        return None
    if proc.returncode != 0:
        return None  # e.g. not a git repository from this directory
    files = []
    for line in (proc.stdout or "").splitlines():
        if len(line) >= 4:
            files.append(line[3:].strip().strip('"'))
    return files


def cmd_extract_modified_files():
    # --path <path> --offset <n>  ->  {"files": [...], "current_position": n}
    # Heuristic journal extraction, but ONLY report files that Git itself
    # confirms as changed. This kills false positives (files the agent only
    # read or mentioned) and the RAG/Docs vs RAG/docs casing duplicates.
    path = arg_value("--path") or ""
    offset = int(arg_value("--offset") or 0)
    files = []
    for ev in _parse_journal_lines(path, offset):
        # Heuristics for tool-call entries that name touched files.
        fp = ev.get("file_path") or ev.get("path")
        ti = ev.get("tool_input")
        if isinstance(ti, dict):
            fp = fp or ti.get("path") or ti.get("file_path")
        if fp:
            files.append(fp)

    # Fail closed: if Git cannot confirm changes, report NOTHING rather than
    # passing heuristic candidates through (those caused nano_llm false
    # positives on entire.com when the extractor ran outside the repo).
    # The repo dir comes from the hook payload (remembered by parse-hook);
    # the journal path itself lives under HOME, NOT inside the repo.
    changed = _git_changed_files(_recall_repo_dir(_session_id_from_ref(path)))
    if changed is None:
        files = []
    elif files:
        # Map heuristic files to Git's canonical paths (matched by basename).
        by_basename = {}
        for c in changed:
            by_basename.setdefault(os.path.basename(c).lower(), c)
        matched, seen = [], set()
        for f in files:
            key = os.path.basename(f).lower()
            gpath = by_basename.get(key)
            if gpath and gpath not in seen:
                seen.add(gpath)
                matched.append(gpath)
        files = matched

    chunks = _resolve_chunks(path) if not os.path.isfile(path) else [path]
    pos = 0
    for chunk in chunks:
        try:
            pos += os.path.getsize(chunk)
        except OSError:
            pass
    if not chunks:
        pos = offset
    out({"files": sorted(set(files)), "current_position": pos})


def cmd_extract_summary():
    # --session-ref <path>
    session_ref = arg_value("--session-ref") or ""
    entry = index_entry(_session_id_from_ref(session_ref))
    if entry:
        title = (entry.get("session") or {}).get("title")
        if title:
            out({"summary": title, "has_summary": True})
    out({"summary": "", "has_summary": False})

# ---------------------------------------------------------------------------
# token_calculator capability
# ---------------------------------------------------------------------------
def cmd_calculate_tokens():
    # --offset <n>  -> token usage from the Vibe session index.
    # stdin: transcript bytes — either raw journal chunks ('"sessionId":"..."')
    # or the prepared Markdown transcript ('# Mistral Vibe session <uuid>').
    raw = sys.stdin.buffer.read().decode("utf-8", errors="replace") or ""
    session_id = None
    match = re.search(r'"sessionId":"([0-9a-fA-F-]{8,64})"', raw)
    if not match:
        # Prepared transcript header written by prepare-transcript.
        match = re.search(r'Vibe session ([0-9a-fA-F]{8}-[0-9a-fA-F-]{27,})', raw)
    if match:
        session_id = match.group(1)
    entry = index_entry(session_id) if session_id else None
    usage = ((entry or {}).get("session") or {}).get("tokenUsage", {})
    if not usage:
        # Fallback: scan the raw journal bytes for the last tokenUsage
        # object (present in Vibe's end-of-turn records). Keeps token
        # counting alive even when the index lookup misses.
        if session_id:
            jraw = full_transcript_bytes(session_id).decode("utf-8", errors="replace")
            last = None
            for m in re.finditer(r'"tokenUsage":\s*\{[^{}]*\}', jraw):
                last = m.group(0)
            if last:
                usage = {}
                for k in ("inputTokens", "outputTokens", "totalTokens"):
                    mm = re.search(r'"%s":\s*([0-9]+)' % k, last)
                    if mm:
                        usage[k] = int(mm.group(1))
    # Vibe's tokenUsage is CUMULATIVE for the whole session, but the CLI sums
    # per-turn values. Report only the per-turn DELTA (cumulative now minus
    # cumulative at the previous turn), persisted per session.
    in_tok = int(usage.get("inputTokens", 0))
    out_tok = int(usage.get("outputTokens", 0))
    tot_tok = int(usage.get("totalTokens", 0))
    last_file = _session_state_path(session_id) + ".last-tokens" if session_id else ""
    last = {"i": 0, "o": 0, "t": 0}
    if last_file:
        try:
            with open(last_file, "r", encoding="utf-8") as f:
                parts = (f.read().strip() or "0 0 0").split()
                last = {"i": int(parts[0]), "o": int(parts[1]), "t": int(parts[2])}
        except (OSError, ValueError, IndexError):
            last = {"i": 0, "o": 0, "t": 0}
    try:
        os.makedirs(STATE_DIR, exist_ok=True)
        with open(last_file, "w", encoding="utf-8") as f:
            f.write("%d %d %d" % (in_tok, out_tok, tot_tok))
    except OSError:
        pass
    out({
        "input_tokens": max(0, in_tok - last["i"]),
        "output_tokens": max(0, out_tok - last["o"]),
        "total_tokens": max(0, tot_tok - last["t"]),
    })

# ---------------------------------------------------------------------------
# Hook capability — bridges Vibe's native hooks.toml into Entire.
#
# Vibe (CLI and VS Code extension) declares hooks in ./.vibe/hooks.toml:
#   [[hooks]] name/type/match/command ... with types pre_tool, post_tool,
#   post_agent. Each hook receives a UTF-8 JSON payload on stdin containing
#   session_id, parent_session_id, transcript_path, cwd, hook_event_name.
#
# Our installed hook commands call `entire hooks vibe <hookName>`, which
# pipes that raw payload to this integration's `parse-hook`.
# ---------------------------------------------------------------------------

HOOK_MARKER = "# managed by entire-agent-vibe"
HOOK_TOML = os.path.join(".vibe", "hooks.toml")

HOOK_BLOCK = """{marker}
[[hooks]]
name = "entire-turn-end"
type = "post_agent"
command = "entire hooks vibe post_agent"
description = "Send the turn-end event to the Entire CLI."
{marker}
""".format(marker=HOOK_MARKER)


def _read_hook_toml():
    if os.path.isfile(HOOK_TOML):
        with open(HOOK_TOML, "r", encoding="utf-8") as f:
            return f.read()
    return ""


def _write_hook_toml(text):
    os.makedirs(os.path.dirname(HOOK_TOML), exist_ok=True)
    with open(HOOK_TOML, "w", encoding="utf-8") as f:
        f.write(text)


def cmd_install_hooks():
    force = "--force" in sys.argv[2:]
    current = _read_hook_toml()
    if HOOK_MARKER in current and not force:
        out({"hooks_installed": 0})  # idempotent, already present
    _write_hook_toml(current.rstrip("\n") + ("\n\n" if current.strip() else "") + HOOK_BLOCK)
    out({"hooks_installed": 1})


def cmd_uninstall_hooks():
    current = _read_hook_toml()
    # Remove our marked block (from marker line up to the next marker).
    lines = current.splitlines(keepends=True)
    cleaned, inside = [], False
    for ln in lines:
        if ln.strip() == HOOK_MARKER:
            inside = True
            continue
        if inside:
            # block ends when the next hook table or comment begins
            if ln.strip().startswith("#") or ln.strip().startswith("[[hooks]]"):
                inside = False
                cleaned.append(ln) if ln.strip() else None
            continue
        cleaned.append(ln)
    _write_hook_toml("".join(cleaned))
    sys.exit(0)


def cmd_are_hooks_installed():
    out({"installed": HOOK_MARKER in _read_hook_toml()})


def cmd_compact_transcript():
    # --session-ref <path> — the CLI passes a temp file with the redacted
    # transcript (our prepared Markdown). That temp file carries the FULL
    # session history, but it can miss the most recent turn's final
    # assistant message (Vibe flushes it to the journal only after the
    # post_agent hook fires). The live journal always has that message, but
    # it is a rolling window — Vibe deletes older chunks — so it can lack
    # early turns. Strategy: parse BOTH sources and MERGE. The Markdown
    # lines provide the complete history, the journal lines provide the
    # complete recent tail. Fallback: treat the ref as a raw journal file.
    # The merged result is converted into Entire's compact transcript JSONL
    # (one line per message, base64-encoded in the response):
    #   {"v":1,"agent":"vibe","cli_version":"...","type":"user",
    #    "content":[{"text":"..."}]}
    #   {"v":1,...,"type":"assistant","content":[{"type":"text","text":"..."},
    #    {"type":"tool_use","name":"...","input":{...},
    #     "result":{"output":"...","status":"success"}}]}
    # Reasoning sections are dropped (mirroring the Amp reference, which omits
    # thinking blocks). On empty output we exit non-zero so the CLI falls back
    # to its built-in compactor.
    path = arg_value("--session-ref") or ""
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            md = f.read()
    except OSError as exc:
        sys.stderr.write("compact-transcript: %s\n" % exc)
        sys.exit(1)

    cli_version = (os.environ.get("ENTIRE_CLI_VERSION") or "").strip() or "unknown"

    def user_line(text):
        return {"v": 1, "agent": "vibe", "cli_version": cli_version,
                "type": "user", "content": [{"text": text}]}

    def new_assistant_line():
        return {"v": 1, "agent": "vibe", "cli_version": cli_version,
                "type": "assistant", "content": []}

    def new_converter():
        # An independent sink for compact lines with its own tracking of
        # the "current" assistant line (used to attach tool blocks).
        state = {"lines": [], "last": None}

        def ensure_assistant():
            if state["last"] is None:
                state["last"] = new_assistant_line()
                state["lines"].append(state["last"])
            return state["last"]

        def add_entries(chunks):
            # Convert replayed journal entries into compact transcript lines.
            for entry in _replay_conversation(chunks):
                if not isinstance(entry, dict):
                    continue
                etype = entry.get("type")
                if etype == "message":
                    texts = []
                    for part in entry.get("content") or []:
                        if isinstance(part, dict) and (part.get("text") or "").strip():
                            texts.append(part["text"].strip())
                    if not texts:
                        continue
                    if str(entry.get("role") or "").lower() == "user":
                        state["lines"].append(user_line("\n\n".join(texts)))
                        state["last"] = None
                    else:
                        state["last"] = new_assistant_line()
                        state["last"]["content"].append(
                            {"type": "text", "text": "\n\n".join(texts)})
                        state["lines"].append(state["last"])
                elif etype == "effect":
                    detail = entry.get("detail") or {}
                    state_out = entry.get("state") or {}
                    command = (detail.get("input") or {}).get("command") or ""
                    structured = ((state_out.get("output") or {}).get("structured_content")) or {}
                    output = state_out.get("outputText") or structured.get("stdout") or ""
                    if not (command or output):
                        continue
                    block = {"type": "tool_use",
                             "name": str(entry.get("title") or detail.get("toolName") or "tool"),
                             "input": {"command": command}}
                    if output:
                        status = "error" if str(state_out.get("status") or "") == "failed" else "success"
                        block["result"] = {"output": output, "status": status}
                    ensure_assistant()["content"].append(block)

        return state, ensure_assistant, add_entries

    md_state, md_ensure, md_add = new_converter()
    j_state, j_ensure, j_add = new_converter()

    # --- Source 1: the live journal (complete recent turns) ---
    # At checkpoint time the journal always contains the final assistant
    # message of the latest turn. Safety: the CLI redacts the
    # compact-transcript output again before persisting, and scopes
    # checkpoints by leading-identical lines, so a merged full-session
    # transcript is attributed correctly even when the journal already
    # contains later turns.
    m = re.search(r"Mistral Vibe session ([0-9a-fA-F-]{32,})", md)
    if m:
        sid = m.group(1)
        candidates = [os.path.join(UNIFIED_DIR, sid)]
        if os.path.isdir(path):
            candidates.append(path)     # manual runs may pass the session dir
        for candidate in candidates:
            if os.path.isdir(candidate):
                j_add(_resolve_chunks(candidate))
                break

    # --- Source 2: the prepared Markdown sections (full history) ---
    sections = []
    current = None
    for ln in md.splitlines():
        if ln.startswith("## "):
            if current:
                sections.append(current)
            current = [ln[3:].strip(), []]
        elif current is not None:
            current[1].append(ln)
    if current:
        sections.append(current)

    for heading, body in sections:
        text = "\n".join(body).strip()
        if heading == "User":
            if text:
                md_state["lines"].append(user_line(text))
                md_state["last"] = None
        elif heading == "Assistant":
            if text:
                md_state["last"] = new_assistant_line()
                md_state["last"]["content"].append({"type": "text", "text": text})
                md_state["lines"].append(md_state["last"])
        elif heading.startswith("Tool"):
            name = heading.split(":", 1)[1].strip() if ":" in heading else "tool"
            blocks = []
            in_fence = False
            fence_text = []
            for ln in body:
                if ln.strip().startswith("```"):
                    if in_fence:
                        blocks.append("\n".join(fence_text).strip())
                        fence_text = []
                    in_fence = not in_fence
                elif in_fence:
                    fence_text.append(ln)
            command = blocks[0] if len(blocks) > 0 else ""
            output = blocks[1] if len(blocks) > 1 else ""
            if command or output:
                block = {"type": "tool_use", "name": name or "tool",
                         "input": {"command": command}}
                if output:
                    block["result"] = {"output": output, "status": "success"}
                md_ensure()["content"].append(block)
        # Reasoning and unknown sections are intentionally skipped.

    # --- Last resort: the ref may be a raw journal file itself ---
    if not md_state["lines"] and not j_state["lines"] and os.path.isfile(path):
        j_add([path])

    # --- Merge: full history from Markdown + complete tail from journal ---
    def line_sig(l):
        # Content signature used to align the two sources: line type plus
        # text content. Tool blocks are ignored because the same turn can
        # carry different tool sets depending on when it was captured.
        texts = []
        for c in l.get("content") or []:
            if isinstance(c, dict) and c.get("text"):
                texts.append(c["text"])
        return json.dumps([l.get("type"), texts], sort_keys=True)

    lines_md = md_state["lines"]
    lines_j = j_state["lines"]
    if lines_md and lines_j:
        j_sigs = [line_sig(l) for l in lines_j]
        last = line_sig(lines_md[-1])
        if last in j_sigs:
            # The journal continues where the Markdown ends: keep the full
            # history and append the journal's newer lines (e.g. the final
            # assistant message the temp file was missing).
            idx = len(j_sigs) - 1 - j_sigs[::-1].index(last)
            lines = lines_md + lines_j[idx + 1:]
        else:
            # The Markdown's last line is absent from the journal — e.g. the
            # temp file merged a turn into one section where the journal has
            # it split into more entries. Align on the newest Markdown line
            # that also exists in the journal and take the journal's more
            # complete tail from there.
            merged = None
            for p in range(len(lines_md) - 1, -1, -1):
                sp = line_sig(lines_md[p])
                if sp in j_sigs:
                    q = len(j_sigs) - 1 - j_sigs[::-1].index(sp)
                    merged = lines_md[:p] + lines_j[q:]
                    break
            # No overlap at all (disjoint captures): concatenate both.
            lines = merged if merged is not None else lines_md + lines_j
    else:
        lines = lines_j or lines_md

    if not lines:
        sys.stderr.write("compact-transcript: no conversation content found\n")
        sys.exit(1)

    payload = "".join(json.dumps(l) + "\n" for l in lines)
    out({"transcript": base64.b64encode(payload.encode("utf-8")).decode("ascii")})
    out({"transcript": base64.b64encode(payload.encode("utf-8")).decode("ascii")})


def cmd_parse_hook():
    # Called by the Entire CLI as:  parse-hook --hook <name>
    # stdin: the raw Vibe hook payload (JSON), fields:
    #   session_id, parent_session_id, transcript_path, cwd, hook_event_name
    payload = read_stdin_json()
    event_name = payload.get("hook_event_name") or arg_value("--hook") or ""
    session_id = payload.get("session_id") or ""

    # Map Vibe's native events onto Entire's lifecycle Event types:
    #   1 SessionStart, 2 TurnStart, 3 TurnEnd, 4 Compaction, 5 SessionEnd
    if event_name == "post_agent":
        event = {"type": 3}          # TurnEnd -> CLI stores a checkpoint
    else:
        out(None)                    # not lifecycle-significant

    event.update({
        "session_id": session_id,
        "previous_session_id": payload.get("parent_session_id"),
        "session_ref": payload.get("transcript_path"),
        "prompt": payload.get("prompt"),
        "model": payload.get("model"),
        "timestamp": payload.get("timestamp"),
        "metadata": {"cwd": payload.get("cwd"), "source": "vibe"},
    })
    # Remember the repo cwd for this session: extract-modified-files needs it
    # to run `git status` inside the worktree (its --path is a HOME journal).
    if payload.get("cwd"):
        _remember_repo_dir(session_id, payload["cwd"])
    out(event)

def _replay_conversation(chunks):
    """Replay projection_delta ops (append/replace/remove_entry) across journal
    chunks and return the ordered conversation entries — Vibe's canonical
    conversation state."""
    state_entries = []
    for chunk in chunks:
        for rec in _parse_journal_lines(chunk):
            payload = rec.get("payload") or {}
            if not isinstance(payload, dict):
                continue
            for op in payload.get("delta") or []:
                if not isinstance(op, dict):
                    continue
                kind = op.get("op")
                entry = op.get("entry") or {}
                eid = entry.get("id") if isinstance(entry, dict) else None
                if kind == "append_entry" and entry:
                    state_entries.append(entry)
                elif kind == "replace_entry" and eid:
                    for i, e in enumerate(state_entries):
                        if e.get("id") == eid:
                            state_entries[i] = entry
                            break
                elif kind == "remove_entry" and eid:
                    state_entries = [e for e in state_entries if e.get("id") != eid]
    return state_entries


def _journal_total_size(session_ref):
    # Total byte size of all journal chunk files for a session ref. Used by
    # the quiescence wait to detect that Vibe has finished flushing the
    # turn's final records to disk.
    total = 0
    for chunk in _resolve_chunks(session_ref):
        try:
            total += os.path.getsize(chunk)
        except OSError:
            pass
    return total


def _wait_for_journal_quiescence(session_ref, max_wait=4.0, poll=0.25,
                                 stable_for=0.5):
    # Vibe's post_agent hook fires BEFORE the turn's final assistant message
    # is appended to the journal (observed: hook at record N, final assistant
    # append_entry a few records later). Reading the journal immediately can
    # therefore miss the very response the user just saw. Poll the journal
    # size until it stops growing, so prepare-transcript captures the full
    # turn. On a static journal this costs roughly `stable_for` seconds once.
    deadline = time.time() + max_wait
    last_size = -1
    stable_since = None
    while time.time() < deadline:
        try:
            size = _journal_total_size(session_ref)
        except OSError:
            return
        if size == last_size:
            if stable_since is None:
                stable_since = time.time()
            if time.time() - stable_since >= stable_for:
                return
        else:
            last_size = size
            stable_since = None
        time.sleep(poll)


def cmd_prepare_transcript():
    # --session-ref <path>
    # Convert Vibe's recovery journal into a readable Markdown transcript so
    # the Entire dashboard can render the conversation. Writes a sidecar file
    # next to the journal chunk: <session-ref>.prepared.md
    # read-transcript prefers that sidecar once it exists.
    #
    # Observed record model (recovery_journal_record_version 1):
    #   projection_delta  payload.delta[] ops: append_entry / replace_entry /
    #                     remove_entry carry the canonical conversation
    #                     entries; set_envelope carries session state.
    #   core_input        payload.input.command (type "user_message": prompts)
    #   action_intent / action_result / callback_* / receipt_*: machinery
    # Entry shapes inside append/replace_entry:
    #   message   role user|assistant, content[{type:"text", text}]
    #   reasoning text
    #   effect    title, detail.input.command, state.outputText /
    #             state.output.structured_content.stdout
    #   notice    transient bookkeeping (removed again via remove_entry)
    session_ref = arg_value("--session-ref") or ""
    # Wait for the journal to settle first: the final assistant message of
    # the turn is flushed shortly AFTER the hook fires.
    _wait_for_journal_quiescence(session_ref)
    chunks = _resolve_chunks(session_ref)
    if not chunks:
        sys.stderr.write("prepare-transcript: no journal chunks found\n")
        sys.exit(1)

    session_id = _session_id_from_ref(session_ref) or "unknown"
    lines = ["# Mistral Vibe session " + session_id, ""]

    def add(role, text):
        text = str(text).strip()
        if not text:
            return
        lines.append("## " + role)
        lines.append("")
        lines.append(text)
        lines.append("")

    def render_entry(entry):
        """Return (heading, text) for a conversation entry, or None."""
        if not isinstance(entry, dict):
            return None
        etype = entry.get("type")
        if etype == "message":
            texts = []
            for part in entry.get("content") or []:
                if isinstance(part, dict):
                    t = (part.get("text") or "").strip()
                    if t:
                        texts.append(t)
            if not texts:
                return None
            role = str(entry.get("role") or "user").lower()
            return ("User" if role == "user" else "Assistant", "\n\n".join(texts))
        if etype == "reasoning":
            text = (entry.get("text") or "").strip()
            return ("Reasoning", text) if text else None
        if etype == "effect":
            detail = entry.get("detail") or {}
            state = entry.get("state") or {}
            title = str(entry.get("title") or detail.get("toolName") or "Tool")
            command = (detail.get("input") or {}).get("command") or ""
            output = state.get("outputText") or ""
            if not output:
                structured = ((state.get("output") or {}).get("structured_content")) or {}
                output = structured.get("stdout") or ""
            body = ""
            if command:
                body += "Command:\n```\n" + str(command).strip() + "\n```"
            if output:
                body += ("\n\n" if body else "") + "Output:\n```\n" + str(output).strip() + "\n```"
            return ("Tool: " + title, body) if body else None
        return None

    # 1) Replay the projection deltas: the canonical conversation state.
    state_entries = _replay_conversation(chunks)

    for entry in state_entries:
        parsed = render_entry(entry)
        if parsed:
            add(parsed[0], parsed[1])

    # 2) Fallbacks when the projection carried nothing (older chunks):
    #    core_input user_message prompts, then legacy turn/start records.
    if len(lines) <= 2:
        for chunk in chunks:
            for rec in _parse_journal_lines(chunk):
                payload = rec.get("payload") or {}
                if not isinstance(payload, dict):
                    continue
                command = (payload.get("input") or {}).get("command") or {}
                if isinstance(command, dict) and command.get("type") == "user_message":
                    for part in command.get("content") or []:
                        if isinstance(part, dict):
                            add("User", part.get("text") or "")
                params = payload.get("params") or {}
                if payload.get("method") == "turn/start" and not params.get("injected"):
                    for part in params.get("message") or []:
                        if isinstance(part, dict):
                            add("User", part.get("text") or "")

    if len(lines) <= 2:
        # No conversation records found: fall back to the index preview.
        entry = index_entry(session_id) or {}
        preview = (entry.get("session") or {}).get("preview")
        if preview:
            lines.extend(["## User", "", preview, ""])

    prepared = chunks[-1] + ".prepared.md"
    try:
        with open(prepared, "w", encoding="utf-8") as f:
            f.write("\n".join(lines) + "\n")
    except OSError as exc:
        sys.stderr.write("prepare-transcript: %s\n" % exc)
        sys.exit(1)
    sys.exit(0)


# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------
COMMANDS = {
    "info": cmd_info,
    "detect": cmd_detect,
    "get-session-id": cmd_get_session_id,
    "get-session-dir": cmd_get_session_dir,
    "resolve-session-file": cmd_resolve_session_file,
    "read-session": cmd_read_session,
    "write-session": cmd_write_session,
    "read-transcript": cmd_read_transcript,
    "prepare-transcript": cmd_prepare_transcript,
    "chunk-transcript": cmd_chunk_transcript,
    "reassemble-transcript": cmd_reassemble_transcript,
    "format-resume-command": cmd_format_resume_command,
    "get-transcript-position": cmd_get_transcript_position,
    "extract-prompts": cmd_extract_prompts,
    "extract-modified-files": cmd_extract_modified_files,
    "extract-summary": cmd_extract_summary,
    "compact-transcript": cmd_compact_transcript,
    "calculate-tokens": cmd_calculate_tokens,
    "parse-hook": cmd_parse_hook,
    "install-hooks": cmd_install_hooks,
    "uninstall-hooks": cmd_uninstall_hooks,
    "are-hooks-installed": cmd_are_hooks_installed,
}


def _debug_log(msg):
    # Optional diagnostics: records every subcommand invocation so
    # integration issues can be traced end to end. Disabled unless the
    # user explicitly opts in via VIBE_AGENT_DEBUG=1.
    if os.environ.get("VIBE_AGENT_DEBUG") != "1":
        return
    try:
        log = os.path.join(HOME, "vibe-agent-debug.log")
        with open(log, "a", encoding="utf-8") as f:
            f.write("%s %s\n" % (time.strftime("%Y-%m-%dT%H:%M:%S"), msg))
    except OSError:
        pass


def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else ""
    _debug_log(cmd + " " + " ".join(sys.argv[2:]))
    handler = COMMANDS.get(cmd)
    if handler is None:
        sys.stderr.write(f"unknown subcommand: {cmd}\n")
        sys.exit(2)
    handler()


if __name__ == "__main__":
    main()
