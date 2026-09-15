"""The Plugin and Content Manager release: archive, packages.json, repository.json."""

import hashlib
import json
import sys
import zipfile
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "scripts"))

import pcm_release  # noqa: E402
from trace_length_analyzer.engine import platform_tag  # noqa: E402

PLUGIN = Path(__file__).resolve().parent.parent
TAGS = [platform_tag()]
DL = "https://github.com/embeddedci-com/kicad-plugins/releases/download/v{v}/pcb-trace-length-analyzer-{v}.zip"
RAW = "https://raw.githubusercontent.com/embeddedci-com/kicad-plugins/main"


@pytest.fixture
def built():
    try:
        pcm_release.check_complete(PLUGIN, TAGS)
    except pcm_release.ReleaseError as e:
        pytest.skip(f"plugin not built (make plugin): {e}")


def do_release(tmp_path, version, status="testing", now=1_800_000_000):
    return pcm_release.release(
        version, tmp_path / "repo", DL.format(v=version), RAW, status,
        archive_dir=tmp_path / "dist", tags=TAGS, now=now,
    )


def sha(p: Path) -> str:
    return hashlib.sha256(p.read_bytes()).hexdigest()


def test_archive_is_laid_out_the_way_pcm_installs_it(built, tmp_path):
    out = do_release(tmp_path, "0.1.0")
    with zipfile.ZipFile(out["archive"]) as z:
        names = set(z.namelist())
        meta = json.loads(z.read("metadata.json"))
        engine = next(i for i in z.infolist() if i.filename.startswith("plugins/bin/") and "engine" in i.filename)
    assert "plugins/plugin.json" in names
    assert "plugins/analyze.py" in names and "plugins/LICENSE" in names
    assert "plugins/web/kicad.html" in names
    assert "resources/icon.png" in names
    # Nothing that is only for development.
    assert not any(n.startswith(("plugins/tests/", "plugins/scripts/")) for n in names)
    assert "plugins/pcm.json" not in names
    assert not any("__pycache__" in n for n in names)
    # The engine keeps its executable bit.
    assert (engine.external_attr >> 16) & 0o111
    assert meta["$schema"] == pcm_release.SCHEMA
    assert meta["identifier"] == "com.embeddedci.pcb-trace-length-analyzer"
    assert meta["license"] == "Apache-2.0"
    (v,) = meta["versions"]
    assert v["version"] == "0.1.0" and v["runtime"] == "ipc" and v["kicad_version"] == "10.0.1"
    assert not any(k.startswith("download_") for k in v), "PCM refuses download keys inside the archive"


def test_packages_json_points_at_the_archive_by_hash_and_size(built, tmp_path):
    out = do_release(tmp_path, "0.1.0")
    pkgs = json.loads((tmp_path / "repo" / "packages.json").read_text())
    (pkg,) = pkgs["packages"]
    (v,) = pkg["versions"]
    assert v["download_url"] == DL.format(v="0.1.0")
    assert v["download_sha256"] == sha(out["archive"])
    assert v["download_size"] == out["archive"].stat().st_size
    assert v["install_size"] > v["download_size"]
    assert v["platforms"] == ["macos", "linux", "windows"]


def test_repository_json_hashes_what_it_points_at(built, tmp_path):
    do_release(tmp_path, "0.1.0")
    repo = tmp_path / "repo"
    r = json.loads((repo / "repository.json").read_text())
    assert r["packages"]["url"] == RAW + "/packages.json"
    assert r["packages"]["sha256"] == sha(repo / "packages.json")
    assert r["resources"]["sha256"] == sha(repo / "resources.zip")
    assert r["packages"]["update_time_utc"] == "2027-01-15 08:00:00"
    with zipfile.ZipFile(repo / "resources.zip") as z:
        assert z.namelist() == ["com.embeddedci.pcb-trace-length-analyzer/icon.png"]


def test_a_new_version_keeps_the_old_ones_newest_first(built, tmp_path):
    do_release(tmp_path, "0.1.0")
    do_release(tmp_path, "0.2.0")
    do_release(tmp_path, "0.10.0")
    (pkg,) = json.loads((tmp_path / "repo" / "packages.json").read_text())["packages"]
    assert [v["version"] for v in pkg["versions"]] == ["0.10.0", "0.2.0", "0.1.0"]


def test_releasing_a_version_again_replaces_it(built, tmp_path):
    do_release(tmp_path, "0.1.0", status="testing")
    do_release(tmp_path, "0.1.0", status="stable")
    (pkg,) = json.loads((tmp_path / "repo" / "packages.json").read_text())["packages"]
    assert [(v["version"], v["status"]) for v in pkg["versions"]] == [("0.1.0", "stable")]


def test_other_packages_in_the_repository_are_left_alone(built, tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    other = {"identifier": "com.embeddedci.other", "name": "Other", "versions": [{"version": "1.0"}]}
    (repo / "packages.json").write_text(json.dumps({"packages": [other]}))
    with zipfile.ZipFile(repo / "resources.zip", "w") as z:
        z.writestr("com.embeddedci.other/icon.png", b"png")
    do_release(tmp_path, "0.1.0")
    pkgs = json.loads((repo / "packages.json").read_text())["packages"]
    assert [p["identifier"] for p in pkgs] == ["com.embeddedci.other", "com.embeddedci.pcb-trace-length-analyzer"]
    assert pkgs[0] == other
    with zipfile.ZipFile(repo / "resources.zip") as z:
        assert z.read("com.embeddedci.other/icon.png") == b"png"


def test_same_inputs_give_the_same_archive(built, tmp_path):
    a = do_release(tmp_path / "a", "0.1.0")
    b = do_release(tmp_path / "b", "0.1.0")
    assert sha(a["archive"]) == sha(b["archive"])


def test_refuses_a_build_missing_an_engine(built, tmp_path):
    with pytest.raises(pcm_release.ReleaseError, match="bin/windows-riscv"):
        pcm_release.release("0.1.0", tmp_path / "repo", "https://x/y.zip", RAW, tags=["windows-riscv"],
                            archive_dir=tmp_path / "dist")


def test_refuses_a_malformed_version(built, tmp_path):
    with pytest.raises(ValueError):
        do_release(tmp_path, "v0.1")


def test_generated_files_pass_kicads_own_schema(built, tmp_path):
    if sys.version_info < (3, 10):
        pytest.skip("kipy.packaging needs Python >= 3.10")
    jsonschema = pytest.importorskip("jsonschema")
    packaging = pytest.importorskip("kipy.packaging.validate")
    out = do_release(tmp_path, "0.1.0")
    report = packaging.validate(str(out["archive"]))
    assert report.ok, [m.message for m in report.messages]
    from importlib.resources import files

    schema = json.loads(files("kipy.packaging.schemas").joinpath("pcm.v2.schema.json").read_text())
    repo = tmp_path / "repo"
    for name, definition in (("packages.json", "PackageArray"), ("repository.json", "Repository")):
        doc = json.loads((repo / name).read_text())
        jsonschema.validate(doc, {**schema, "$ref": f"#/definitions/{definition}"})
