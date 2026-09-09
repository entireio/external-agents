"""Run the generated launcher from a checkout with shell metacharacters."""

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class LauncherTests(unittest.TestCase):
    def test_checkout_path_is_literal(self):
        agent = Path(__file__).parent.name.removeprefix("entire-agent-")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "odd \" ' \\ $(touch injected) `touch injected2` $HOME"
            root.mkdir()
            root = root.resolve()
            subprocess.run([sys.executable, str(Path(__file__).with_name("build_launcher.py"))],
                           cwd=root, check=True)
            for env in (".venv", ".venv-crewai"):
                bindir = root / env / "bin"
                bindir.mkdir(parents=True)
                (bindir / "python").symlink_to(sys.executable)
            stub = "import json, sys; print(json.dumps(sys.argv))\n"
            (root / "protocol_wrapper.py").write_text(stub)
            fixture = root / f"e2e_{agent}_fixture.py"
            fixture.write_text(stub)
            launcher = root / f"entire-agent-{agent}"
            cases = [(["info", "argument with spaces"],
                      [str(root / "protocol_wrapper.py"), agent, "info", "argument with spaces"])]
            if agent == "langgraph":
                cases.append((["__e2e_run_prompt", "a prompt"], [str(fixture), "a prompt"]))
            else:
                cases.append((["__e2e_run_fixture"], [str(fixture)]))
            for args, expected in cases:
                with self.subTest(args=args):
                    result = subprocess.run([str(launcher), *args], cwd=temp,
                                            capture_output=True, text=True, check=True)
                    self.assertEqual(json.loads(result.stdout), expected)
            self.assertFalse((Path(temp) / "injected").exists())
            self.assertFalse((Path(temp) / "injected2").exists())


if __name__ == "__main__":
    unittest.main()
