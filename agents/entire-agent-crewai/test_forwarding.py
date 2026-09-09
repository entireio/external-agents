"""Ensure cancellation targets the adapter and buffered input survives exec."""

import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


class ForwardingTests(unittest.TestCase):
    def test_forwarding_keeps_process_identity_and_input(self):
        agent = Path(__file__).parent.name.removeprefix("entire-agent-")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            wrapper = root / "protocol_wrapper.py"
            shutil.copyfile(Path(__file__).with_name("protocol_wrapper.py"), wrapper)
            underlying = root / ".venv" / "bin" / f"entire-agent-{agent}"
            underlying.parent.mkdir(parents=True)
            underlying.write_text(
                f"#!{sys.executable}\n"
                "import json, os, sys\n"
                "print(json.dumps({'pid': os.getpid(), 'args': sys.argv[1:], 'input': sys.stdin.read()}))\n"
                "sys.exit(7)\n"
            )
            underlying.chmod(0o755)
            # The fallback path buffers stdin. Use more than a pipe's capacity
            # to catch replay implementations that block before exec.
            payload = {"session_ref": str(root / "session.jsonl"), "user_prompt": "x" * 200000}
            for args in (["parse-hook", "--hook", "turn-start"], ["read-session"]):
                with self.subTest(args=args), subprocess.Popen(
                    [sys.executable, str(wrapper), agent, *args],
                    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                ) as process:
                    try:
                        stdout, stderr = process.communicate(json.dumps(payload), timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.communicate()
                        self.fail("forwarding stalled while replaying input")
                    self.assertEqual(process.returncode, 7, stderr)
                    result = json.loads(stdout)
                    self.assertEqual(result["pid"], process.pid, "the adapter must replace the wrapper process")
                    self.assertEqual(result["args"], args)
                    self.assertEqual(json.loads(result["input"]), payload)


if __name__ == "__main__":
    unittest.main()
