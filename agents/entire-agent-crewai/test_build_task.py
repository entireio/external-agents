"""Exercise mise's actual inline build task under Ubuntu's default shell."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import tomllib
import unittest


@unittest.skipUnless(shutil.which("dash"), "requires the POSIX dash shell")
class BuildTaskTests(unittest.TestCase):
    def test_runtime_opt_in_and_stale_environment_recreation(self):
        task = tomllib.loads(Path(__file__).with_name("mise.toml").read_text())["tasks"]["build"]["run"]
        # Substitute only Python/package installation. The task's shell logic
        # runs unchanged, without network access or real dependency installs.
        fake_python = '''#!/bin/sh
printf '%s\\n' "$*" >> calls.log
case "$1" in
  -c) exit 1 ;; # Simulate a pre-existing Python 3.14 environment.
esac
if [ "$1" = "-m" ] && [ "$2" = "venv" ]; then
  for target do :; done
  mkdir -p "$target/bin"
  cp "$0" "$target/bin/python"
fi
'''
        for enabled in ("", "1"):
            with self.subTest(CREWAI_E2E=enabled), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                bindir = root / "bin"
                bindir.mkdir()
                python = bindir / "python3"
                python.write_text(fake_python)
                python.chmod(0o755)
                old_python = root / ".venv" / "bin" / "python"
                old_python.parent.mkdir(parents=True)
                shutil.copy2(python, old_python)
                env = dict(os.environ, CREWAI_E2E=enabled, PATH=str(bindir) + os.pathsep + os.environ["PATH"])
                result = subprocess.run([shutil.which("dash"), "-ec", task], cwd=root,
                                        env=env, capture_output=True, text=True, check=True)
                self.assertEqual(result.stderr, "")
                calls = (root / "calls.log").read_text().splitlines()
                self.assertIn("-m venv --clear .venv", calls)
                self.assertEqual((root / ".venv-crewai" / "bin" / "python").exists(), enabled == "1")
                if enabled:
                    self.assertIn("-m pip install entire-adapter[crewai]==0.2.2 crewai==1.15.20", calls)


if __name__ == "__main__":
    unittest.main()
