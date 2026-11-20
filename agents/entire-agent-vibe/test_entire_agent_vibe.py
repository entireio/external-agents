"""Unit and protocol smoke tests for entire-agent-vibe.

Runs the agent script as a subprocess with HOME pointed at a temporary
directory, so the suite needs neither a Vibe installation nor LLM
credentials. Synthetic journals exercise the projection-delta replay and
the Markdown/journal merge in compact-transcript.
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "entire_agent_vibe.py")
SESSION_ID = "11111111-2222-3333-4444-555555555555"


def user_msg(text, eid="e1"):
    return {"id": eid, "type": "message", "role": "user",
            "content": [{"type": "text", "text": text}]}


def assistant_msg(text, eid="e2"):
    return {"id": eid, "type": "message", "role": "assistant",
            "content": [{"type": "text", "text": text}]}


class AgentTestCase(unittest.TestCase):
    def setUp(self):
        self.home = tempfile.mkdtemp(prefix="vibe-home-")
        self.cwd = tempfile.mkdtemp(prefix="vibe-cwd-")

    def tearDown(self):
        shutil.rmtree(self.home, ignore_errors=True)
        shutil.rmtree(self.cwd, ignore_errors=True)

    def run_agent(self, args, stdin=b"", home=None, cwd=None):
        env = dict(os.environ)
        env["HOME"] = home or self.home
        env.pop("VIBE_AGENT_DEBUG", None)
        return subprocess.run(
            [sys.executable, SCRIPT] + list(args),
            input=stdin, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=env, cwd=cwd or self.cwd, timeout=30)

    def write_journal(self, entries, session_id=SESSION_ID, home=None):
        """Write a synthetic journal with one projection_delta per entry.
        Returns the session directory (the ref the CLI passes)."""
        jdir = os.path.join(home or self.home, ".vibe", "logs", "session",
                            "unified", session_id, "journal")
        os.makedirs(jdir)
        chunk = os.path.join(jdir, "000001.jsonl")
        with open(chunk, "w", encoding="utf-8") as f:
            for entry in entries:
                rec = {"type": "projection_delta",
                       "payload": {"delta": [{"op": "append_entry",
                                              "entry": entry}]}}
                f.write(json.dumps(rec) + "\n")
        return os.path.dirname(jdir)


class TestInfoAndDetect(AgentTestCase):
    def test_info_reports_protocol_capabilities(self):
        proc = self.run_agent(["info"])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        data = json.loads(proc.stdout)
        self.assertEqual(data["protocol_version"], 1)
        self.assertEqual(data["name"], "vibe")
        self.assertEqual(data["hook_names"], ["post_agent"])
        caps = data["capabilities"]
        for key in ("hooks", "transcript_analyzer", "transcript_preparer",
                    "token_calculator", "compact_transcript"):
            self.assertTrue(caps[key], key)

    def test_detect_reflects_vibe_dir_presence(self):
        proc = self.run_agent(["detect"])
        self.assertEqual(proc.returncode, 0)
        self.assertFalse(json.loads(proc.stdout)["present"])
        os.makedirs(os.path.join(self.home, ".vibe"))
        proc = self.run_agent(["detect"])
        self.assertTrue(json.loads(proc.stdout)["present"])


class TestSessionBinding(AgentTestCase):
    def test_get_session_id_resolves_repo_from_index(self):
        repo = tempfile.mkdtemp(prefix="vibe-repo-")
        self.addCleanup(shutil.rmtree, repo, ignore_errors=True)
        idx_dir = os.path.join(self.home, ".vibe", "logs", "session")
        os.makedirs(idx_dir)
        with open(os.path.join(idx_dir, "unified.session-index.json"),
                  "w", encoding="utf-8") as f:
            json.dump({"entries": [{
                "session_id": SESSION_ID,
                "metadata": {"cwd": repo},
                "session": {"updatedAt": 1700000000}}]}, f)
        proc = self.run_agent(["get-session-id"],
                              stdin=json.dumps({"repo_path": repo}).encode())
        self.assertEqual(json.loads(proc.stdout)["session_id"], SESSION_ID)
        # An explicit session id in the hook input always wins.
        proc = self.run_agent(["get-session-id"],
                              stdin=json.dumps({"session_id": "explicit"}).encode())
        self.assertEqual(json.loads(proc.stdout)["session_id"], "explicit")


class TestHooks(AgentTestCase):
    def test_install_and_uninstall_hooks(self):
        hooks_toml = os.path.join(self.cwd, ".vibe", "hooks.toml")
        proc = self.run_agent(["install-hooks"], stdin=b"{}")
        self.assertEqual(json.loads(proc.stdout)["hooks_installed"], 1)
        self.assertTrue(os.path.isfile(hooks_toml))
        with open(hooks_toml, encoding="utf-8") as f:
            self.assertIn("entire hooks vibe post_agent", f.read())
        self.assertTrue(json.loads(
            self.run_agent(["are-hooks-installed"]).stdout)["installed"])
        # Idempotent: a second install does not duplicate the block.
        proc = self.run_agent(["install-hooks"], stdin=b"{}")
        self.assertEqual(json.loads(proc.stdout)["hooks_installed"], 0)
        self.assertEqual(self.run_agent(["uninstall-hooks"]).returncode, 0)
        self.assertFalse(json.loads(
            self.run_agent(["are-hooks-installed"]).stdout)["installed"])


class TestParseHook(AgentTestCase):
    def test_parse_hook_maps_post_agent_to_turn_end(self):
        payload = {"hook_event_name": "post_agent",
                   "session_id": "s1", "parent_session_id": None,
                   "transcript_path": "/tmp/x", "cwd": "/tmp/repo"}
        proc = self.run_agent(["parse-hook", "--hook", "post_agent"],
                              stdin=json.dumps(payload).encode())
        event = json.loads(proc.stdout)
        self.assertEqual(event["type"], 3)  # TurnEnd
        self.assertEqual(event["session_id"], "s1")
        self.assertEqual(event["metadata"]["source"], "vibe")
        # Other Vibe hook types are not lifecycle-significant.
        proc = self.run_agent(["parse-hook", "--hook", "pre_tool"],
                              stdin=json.dumps({"hook_event_name":
                                               "pre_tool"}).encode())
        self.assertEqual(proc.returncode, 0)
        self.assertIsNone(json.loads(proc.stdout))


class TestTranscripts(AgentTestCase):
    def test_prepare_transcript_writes_readable_markdown(self):
        session_dir = self.write_journal(
            [user_msg("Please add a test.", eid="u1"),
             assistant_msg("Added test_entropy.py.", eid="a1")])
        proc = self.run_agent(["prepare-transcript", "--session-ref", session_dir])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        prepared = os.path.join(session_dir, "journal",
                                "000001.jsonl.prepared.md")
        self.assertTrue(os.path.isfile(prepared))
        with open(prepared, encoding="utf-8") as f:
            md = f.read()
        self.assertIn("# Mistral Vibe session " + SESSION_ID, md)
        self.assertIn("## User", md)
        self.assertIn("Please add a test.", md)
        self.assertIn("## Assistant", md)
        self.assertIn("Added test_entropy.py.", md)

    def test_compact_transcript_merges_markdown_with_journal(self):
        # The journal holds the full turn INCLUDING the final assistant
        # message; the CLI's Markdown temp file is missing that tail.
        self.write_journal(
            [user_msg("Ship it.", eid="u1"),
             assistant_msg("Shipping now.", eid="a1"),
             assistant_msg("Done: v1.2.3 tagged.", eid="a2")])
        md = ("# Mistral Vibe session " + SESSION_ID + "\n\n"
              "## User\n\nShip it.\n\n"
              "## Assistant\n\nShipping now.\n")
        ref = os.path.join(self.cwd, "transcript.md")
        with open(ref, "w", encoding="utf-8") as f:
            f.write(md)
        proc = self.run_agent(["compact-transcript", "--session-ref", ref])
        self.assertEqual(proc.returncode, 0, proc.stderr)
        import base64
        payload = json.loads(proc.stdout)["transcript"]
        lines = [json.loads(l) for l in
                 base64.b64decode(payload).decode("utf-8").splitlines()]
        texts = []
        for line in lines:
            for part in line["content"]:
                if part.get("text"):
                    texts.append(part["text"])
        self.assertIn("Ship it.", texts)
        self.assertIn("Shipping now.", texts)
        # The merged compact transcript must contain the journal's newer
        # tail that the Markdown source was missing.
        self.assertEqual(lines[-1]["type"], "assistant")
        self.assertEqual(texts[-1], "Done: v1.2.3 tagged.")


if __name__ == "__main__":
    unittest.main()
