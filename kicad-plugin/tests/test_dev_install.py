import json
import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "scripts"))

import dev_install  # noqa: E402

PLUGIN = Path(__file__).resolve().parent.parent


def test_dev_copy_has_its_own_identity_and_runs_this_checkout(tmp_path):
    target = dev_install.install(tmp_path)
    m = json.loads((target / "plugin.json").read_text())
    release = json.loads((PLUGIN / "plugin.json").read_text())
    assert m["identifier"] == release["identifier"] + ".dev"
    assert m["name"].endswith("(dev)") and all(a["name"].endswith("(dev)") for a in m["actions"])
    # Every file the manifest names is really there, as a file, not a link.
    for a in m["actions"]:
        entry = target / a["entrypoint"]
        assert entry.is_file() and not entry.is_symlink()
        for icon in a["icons-light"] + a["icons-dark"]:
            assert (target / icon).is_file()
        assert str(PLUGIN) in entry.read_text()
    # The entry script imports the plugin from this checkout.
    out = subprocess.run(
        [sys.executable, "-c", f"import runpy,sys; sys.argv=['{target / 'analyze.py'}']; "
         f"exec(open('{target / 'analyze.py'}').read().split('if __name__')[0]); "
         "import trace_length_analyzer.app as a; print(a.PLUGIN_DIR); print(a.plugin_identifier())"],
        capture_output=True, text=True, cwd=tmp_path,
    )
    lines = out.stdout.split()
    assert lines[0] == str(PLUGIN), out.stderr
    assert lines[1] == release["identifier"] + ".dev"


def test_it_borrows_the_release_environment_instead_of_downloading_pyside_again(tmp_path):
    target = dev_install.install(tmp_path)
    release = json.loads((PLUGIN / "plugin.json").read_text())["identifier"]
    entry = (target / "analyze.py").read_text()
    assert str(dev_install.release_env(release)) in entry
    assert "site-packages" in entry
    # And its own requirements ask for nothing heavy: comments aside, only kipy.
    wants = [l.strip() for l in (target / "requirements.txt").read_text().splitlines()
             if l.strip() and not l.strip().startswith("#")]
    assert wants == ["kicad-python>=0.7.0"]


def test_reinstall_replaces_and_uninstall_removes(tmp_path):
    t = dev_install.install(tmp_path)
    (t / "stale.txt").write_text("x")
    t = dev_install.install(tmp_path)
    assert not (t / "stale.txt").exists()
    dev_install.uninstall(tmp_path)
    assert not t.exists()


def test_refuses_to_overwrite_a_folder_it_did_not_make(tmp_path):
    (tmp_path / dev_install.FOLDER).mkdir()
    with pytest.raises(SystemExit):
        dev_install.install(tmp_path)


def test_removes_the_old_whole_folder_link(tmp_path):
    legacy = tmp_path / "pcb-trace-length-analyzer"
    legacy.symlink_to(PLUGIN)
    dev_install.install(tmp_path)
    assert not legacy.exists()
