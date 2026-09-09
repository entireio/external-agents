"""Regression tests for the wrapper's session sidecar behavior."""

import base64
import json
import os
import stat
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class SessionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "session.jsonl"
        self.original = b'{"type":"user","content":"first"}\n'
        self.path.write_bytes(self.original)
        self.agent = Path(__file__).parent.name.removeprefix("entire-agent-")

    def command(self, command, payload):
        result = subprocess.run(
            [sys.executable, str(Path(__file__).with_name("protocol_wrapper.py")),
             self.agent, command],
            input=json.dumps(payload), text=True, capture_output=True, check=True,
            env=dict(os.environ, ENTIRE_REPO_ROOT=self.temp.name),
        )
        return json.loads(result.stdout) if result.stdout else None

    def restore(self):
        self.command("write-session", {
            "session_id": "test-session", "session_ref": str(self.path),
            "agent_name": self.agent, "repo_path": self.temp.name,
            "start_time": "2026-09-09T12:00:00Z",
            "native_data": base64.b64encode(self.original).decode("ascii"),
        })

    def read(self):
        return self.command("read-session", {
            "session_id": "test-session", "session_ref": str(self.path),
        })

    @unittest.skipUnless(os.name == "posix", "POSIX file permissions")
    def test_restore_creates_private_files(self):
        self.path.unlink()
        old_umask = os.umask(0)
        try:
            self.restore()
        finally:
            os.umask(old_umask)
        for path in (self.path, self.path.with_suffix(".jsonl.session.json")):
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)

    @unittest.skipUnless(os.name == "posix", "POSIX file permissions")
    def test_restore_makes_existing_files_private(self):
        self.restore()
        paths = (self.path, self.path.with_suffix(".jsonl.session.json"))
        for path in paths:
            path.chmod(0o644)
        self.restore()
        for path in paths:
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)

    def test_sidecar_does_not_duplicate_transcript(self):
        self.restore()
        metadata = json.loads(self.path.with_suffix(".jsonl.session.json").read_text())
        self.assertNotIn("native_data", metadata)
        self.assertEqual(base64.b64decode(self.read()["native_data"]), self.original)

    def test_install_replaces_marker_symlink_without_overwriting_target(self):
        marker = Path(self.temp.name) / ".entire" / f"{self.agent}-adapter-hooks-installed.json"
        marker.parent.mkdir()
        marker.symlink_to(self.path)
        self.command("install-hooks", {})
        self.assertEqual(self.path.read_bytes(), self.original)
        self.assertFalse(marker.is_symlink())
        self.assertEqual(json.loads(marker.read_text())["agent"], self.agent)

    def test_null_file_lists_are_initialized_after_restore(self):
        self.restore()
        session = self.read()
        fields = ("modified_files", "new_files", "deleted_files")
        for field in fields:
            session[field] = None
        self.command("write-session", session)
        restored = self.read()
        for field in fields:
            self.assertEqual(restored[field], [])

    def test_nonempty_file_lists_survive_restore(self):
        self.restore()
        session = self.read()
        files = {"modified_files": ["changed.py"], "new_files": ["new.py"],
                 "deleted_files": ["removed.py"]}
        session.update(files)
        self.command("write-session", session)
        restored = self.read()
        for field, expected in files.items():
            self.assertEqual(restored[field], expected)

    def test_restore_requires_nonempty_string_session_ref(self):
        for fields in ({}, {"session_ref": None}, {"session_ref": ""},
                       {"session_ref": 0}, {"session_ref": False},
                       {"session_ref": ["session.jsonl"]}, {"session_ref": {"path": "session.jsonl"}}):
            with self.subTest(fields=fields):
                with self.assertRaises(subprocess.CalledProcessError) as error:
                    self.command("write-session", {
                        "native_data": base64.b64encode(self.original).decode("ascii"),
                        **fields,
                    })
                self.assertIn("session_ref", error.exception.stderr)
                self.assertEqual(self.path.read_bytes(), self.original)

    def test_invalid_native_data_preserves_existing_session(self):
        self.restore()
        sidecar = self.path.with_suffix(".jsonl.session.json")
        saved_metadata = sidecar.read_bytes()
        for invalid in ("YWJj!!!!", "abc", "é", "YWJj\n", [97, 98, 99], {"text": "abc"}, 123, False):
            with self.subTest(native_data=invalid):
                with self.assertRaises(subprocess.CalledProcessError) as error:
                    self.command("write-session", {
                        "session_ref": str(self.path), "native_data": invalid,
                    })
                self.assertIn("native_data", error.exception.stderr)
                self.assertEqual(self.path.read_bytes(), self.original)
                self.assertEqual(sidecar.read_bytes(), saved_metadata)

    def test_valid_base64_preserves_arbitrary_bytes(self):
        data = bytes(range(256))
        self.command("write-session", {
            "session_ref": str(self.path),
            "native_data": base64.b64encode(data).decode("ascii"),
        })
        self.assertEqual(self.path.read_bytes(), data)
        self.assertEqual(base64.b64decode(self.read()["native_data"]), data)

    def test_absent_native_data_preserves_existing_session(self):
        self.restore()
        sidecar = self.path.with_suffix(".jsonl.session.json")
        saved_metadata = sidecar.read_bytes()
        for native in ({}, {"native_data": None}):
            with self.subTest(native=native):
                self.command("write-session", {"session_ref": str(self.path), **native})
                self.assertEqual(self.path.read_bytes(), self.original)
                self.assertEqual(sidecar.read_bytes(), saved_metadata)

    def test_absent_native_data_does_not_create_transcript(self):
        self.path.unlink()
        self.command("write-session", {"session_ref": str(self.path), "native_data": None})
        self.assertFalse(self.path.exists())

    def test_explicit_empty_native_data_restores_empty_transcript(self):
        self.command("write-session", {"session_ref": str(self.path), "native_data": ""})
        self.assertEqual(self.path.read_bytes(), b"")

    def test_read_after_append_round_trips_current_transcript(self):
        self.restore()
        current = self.original + b'{"type":"assistant","content":"later"}\n'
        self.path.write_bytes(current)
        session = self.read()
        self.assertEqual(base64.b64decode(session["native_data"]), current)
        self.assertEqual(session["session_id"], "test-session")
        self.assertEqual(session["start_time"], "2026-09-09T12:00:00Z")
        self.command("write-session", session)
        self.assertEqual(self.path.read_bytes(), current)

    def test_read_missing_transcript_does_not_resurrect_sidecar_bytes(self):
        self.restore()
        self.path.unlink()
        session = self.read()
        self.assertIsNone(session["native_data"])
        self.command("write-session", session)
        self.assertFalse(self.path.exists())


if __name__ == "__main__":
    unittest.main()
