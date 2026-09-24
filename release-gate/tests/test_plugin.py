import json

from release_gate import plugin


def test_plugin_info(capsys):
    rc = plugin.main(["info"])
    assert rc == 0
    out = json.loads(capsys.readouterr().out)
    assert out["name"] == "release-gate"
    assert out["kind"] == "cli-plugin"
    assert "score" in out["commands"]


def test_plugin_version(capsys):
    rc = plugin.main(["version"])
    assert rc == 0
    assert "release-gate" in capsys.readouterr().out
