"""The release flow: the checks that stop it, and the order that publishes."""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "scripts"))

import release  # noqa: E402


def init_file(tmp_path: Path, version: str = "0.1.2") -> Path:
    p = tmp_path / "__init__.py"
    p.write_text(f'"""Doc."""\n\n__version__ = "{version}"\n\nOTHER = 1\n')
    return p


def test_version_must_look_like_one():
    assert release.parse_version("0.1.3") == (0, 1, 3)
    for bad in ("v0.1.3", "0.1", "0.1.3-rc1", ""):
        with pytest.raises(SystemExit):
            release.parse_version(bad)


def test_bump_rewrites_only_the_version(tmp_path):
    p = init_file(tmp_path)
    assert release.current_version(p) == "0.1.2"
    assert release.bump("0.1.3", p) is True
    assert release.current_version(p) == "0.1.3"
    assert "OTHER = 1" in p.read_text()
    # Running it twice is not an error: a half-finished release can be retried.
    assert release.bump("0.1.3", p) is False


def test_a_version_that_is_not_newer_is_refused(tmp_path, monkeypatch):
    monkeypatch.setattr(release, "INIT", init_file(tmp_path, "0.2.0"))
    for older in ("0.2.0", "0.1.9"):
        with pytest.raises(SystemExit) as e:
            release.preflight(older, tmp_path, publish=False, skip_tests=True)
        assert "not newer" in str(e.value)


def test_a_dirty_tree_is_refused_but_the_version_file_is_not_dirt(tmp_path, monkeypatch):
    monkeypatch.setattr(release, "INIT", init_file(tmp_path, "0.1.2"))
    monkeypatch.setattr(release, "ROOT", tmp_path)

    # The version file alone does not count: a retried release has bumped it.
    monkeypatch.setattr(release, "run", lambda *a, **k: " M __init__.py\n")
    release.preflight("0.1.3", tmp_path, publish=False, skip_tests=True)

    monkeypatch.setattr(release, "run", lambda *a, **k: " M ddr/presets.go\n")
    with pytest.raises(SystemExit) as e:
        release.preflight("0.1.3", tmp_path, publish=False, skip_tests=True)
    assert "commit or stash" in str(e.value)


def test_tests_run_unless_skipped(tmp_path, monkeypatch):
    monkeypatch.setattr(release, "INIT", init_file(tmp_path, "0.1.2"))
    monkeypatch.setattr(release, "ROOT", tmp_path)
    ran = []

    def fake_run(cmd, cwd=None, capture=False):
        ran.append(list(cmd))
        return ""

    monkeypatch.setattr(release, "run", fake_run)
    release.preflight("0.1.3", tmp_path, publish=False, skip_tests=False)
    assert ["make", "test"] in ran


# The trap this script exists to close: packages.json names the archive by URL,
# so a KiCad refreshing between the two would fetch one that is not there.
def test_the_archive_is_uploaded_before_the_index_is_pushed():
    steps = release.publish_steps(
        "0.1.3", "testing", Path("/tmp/kicad-plugins"), "org/repo", Path("/tmp/a.zip")
    )
    flat = [" ".join(s) for s in steps]
    upload = next(i for i, s in enumerate(flat) if s.startswith("gh release create"))
    index = next(i for i, s in enumerate(flat) if "push" in s)
    assert upload < index, flat
    assert "pcb-trace-length-analyzer-v0.1.3" in flat[upload]
    # And this repo's own version bump is committed too, last.
    assert flat[-1] == "git push"
    assert "Release 0.1.3" in flat[-2]


def test_nothing_is_published_without_a_yes(tmp_path, monkeypatch, capsys):
    monkeypatch.setattr("builtins.input", lambda *_: "n")
    assert not release.confirm("0.1.3", "testing", "org/repo", tmp_path / "a.zip", yes=False)
    monkeypatch.setattr("builtins.input", lambda *_: "y")
    assert release.confirm("0.1.3", "testing", "org/repo", tmp_path / "a.zip", yes=False)
    # A pipe with nothing on it is a no, not a crash.
    def raise_eof(*_):
        raise EOFError

    monkeypatch.setattr("builtins.input", raise_eof)
    assert not release.confirm("0.1.3", "testing", "org/repo", tmp_path / "a.zip", yes=False)
    assert release.confirm("0.1.3", "testing", "org/repo", tmp_path / "a.zip", yes=True)


# A release is stable unless it is deliberately not. The releases that have
# gone out were all stable, and a default of "testing" meant saying so on every
# command, with a version marked wrong for everyone the one time it was missed.
def test_a_release_is_stable_unless_asked_otherwise(monkeypatch):
    built = {}
    monkeypatch.setattr(release, "preflight", lambda *a, **k: None)
    monkeypatch.setattr(release, "build", lambda version, status, repo: built.update(status=status))

    release.main(["0.9.9", "--no-publish"])
    assert built["status"] == "stable"

    release.main(["0.9.9", "--no-publish", "--status", "testing"])
    assert built["status"] == "testing"
