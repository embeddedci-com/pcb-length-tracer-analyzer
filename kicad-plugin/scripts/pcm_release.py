"""Build a Plugin and Content Manager release and add it to a repository.

KiCad's PCM reads a repository from three files: repository.json, which points
at packages.json and resources.zip with their hashes. packages.json lists every
package and every version with a download URL and hash; resources.zip holds an
icon per package. This script produces all of it:

    1. the package archive, laid out the way PCM installs it:
         plugins/          the plugin folder (plugin.json at its root)
         resources/icon.png
         metadata.json     the package, with this one version and no download keys
    2. packages.json in the repository directory, with this version added (or
       replaced) and every earlier version kept
    3. resources.zip and repository.json, re-hashed

Every platform's engine goes into the one archive. PCM's "platforms" field is
per operating system, not per architecture, and no package in KiCad's own
repository publishes the same version twice for different platforms, so one
archive per version is the arrangement PCM is known to handle.

Standard library only. Usage (normally through `make pcm-release`):

    python scripts/pcm_release.py --version 0.1.0 --repo ../kicad-plugins \\
        --download-url https://github.com/.../releases/download/v0.1.0/x.zip \\
        --repo-url https://raw.githubusercontent.com/.../main
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
import time
import zipfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Tuple

PLUGIN = Path(__file__).resolve().parent.parent
SCHEMA = "https://go.kicad.org/pcm/schemas/v2"

# What goes into plugins/. Everything else in the folder -- tests, scripts, the
# package description, caches -- stays out.
INCLUDE = [
    "plugin.json",
    "requirements.txt",
    "README.md",
    "LICENSE",
    "analyze.py",
    "net_length.py",
    "icons",
    "trace_length_analyzer",
    "web",
    "bin",
]
EXCLUDE_PARTS = {"__pycache__", ".pytest_cache", ".DS_Store"}

# Every engine a release carries. find_engine() picks by bin/<os>-<arch>/.
RELEASE_TAGS = ["darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64", "windows-amd64"]

# A fixed timestamp inside the archive, so the same inputs give the same bytes
# and the same hash.
ZIP_TIME = (2020, 1, 1, 0, 0, 0)


class ReleaseError(RuntimeError):
    pass


def version_key(v: str) -> Tuple[int, ...]:
    return tuple(int(p) for p in v.split("."))


def package_entry(desc: Dict[str, Any]) -> Dict[str, Any]:
    """The package's fields from pcm.json, without the per-version ones."""
    return {k: v for k, v in desc.items() if not k.startswith("$") and k not in ("kicad_version", "platforms")}


def version_entry(desc: Dict[str, Any], version: str, status: str) -> Dict[str, Any]:
    return {
        "version": version,
        "status": status,
        "kicad_version": desc["kicad_version"],
        "platforms": desc["platforms"],
        "runtime": "ipc",
    }


def check_complete(plugin: Path, tags: List[str]) -> None:
    """Refuse to package a plugin that would not run on every platform it claims."""
    missing = []
    if not (plugin / "web" / "kicad.html").is_file():
        missing.append("web/kicad.html (npm --prefix webapp run build:kicad)")
    for tag in tags:
        exe = "pcb-trace-length-analyzer-engine" + (".exe" if tag.startswith("windows") else "")
        if not (plugin / "bin" / tag / exe).is_file():
            missing.append(f"bin/{tag}/{exe}")
    if missing:
        raise ReleaseError("the plugin is not built for release; missing:\n  " + "\n  ".join(missing))


def _files(plugin: Path) -> List[Path]:
    out = []
    for name in INCLUDE:
        p = plugin / name
        if p.is_file():
            out.append(p)
        elif p.is_dir():
            out.extend(f for f in sorted(p.rglob("*")) if f.is_file() and not (set(f.parts) & EXCLUDE_PARTS))
    return out


def _add(z: zipfile.ZipFile, arcname: str, data: bytes, executable: bool = False) -> None:
    info = zipfile.ZipInfo(arcname, ZIP_TIME)
    info.compress_type = zipfile.ZIP_DEFLATED
    # Unix permissions in the high bits. Whether PCM restores them is up to
    # its unzip; engine.py sets the executable bit itself if it was lost.
    info.external_attr = ((0o755 if executable else 0o644) | 0o100000) << 16
    info.create_system = 3
    z.writestr(info, data)


def build_archive(plugin: Path, desc: Dict[str, Any], version: str, status: str, out: Path) -> Dict[str, int]:
    """Write the package archive. Returns its download and install sizes."""
    metadata = {"$schema": SCHEMA, **package_entry(desc), "versions": [version_entry(desc, version, status)]}
    icon = plugin / "icons" / "analyzer-64.png"
    if not icon.is_file():
        raise ReleaseError(f"{icon} is missing (python scripts/make_icons.py)")

    install = 0
    out.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(out, "w") as z:
        for f in _files(plugin):
            rel = f.relative_to(plugin).as_posix()
            data = f.read_bytes()
            install += len(data)
            _add(z, "plugins/" + rel, data, executable=rel.startswith("bin/"))
        data = icon.read_bytes()
        install += len(data)
        _add(z, "resources/icon.png", data)
        data = (json.dumps(metadata, indent=2) + "\n").encode("utf-8")
        install += len(data)
        _add(z, "metadata.json", data)
    return {"download_size": out.stat().st_size, "install_size": install}


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def merge_packages(existing: Dict[str, Any], desc: Dict[str, Any], version: Dict[str, Any]) -> Dict[str, Any]:
    """packages.json with this version added.

    The package's text is refreshed from pcm.json; its other versions are kept,
    newest first, so a user can still see and roll back to them. Publishing a
    version that is already there replaces it -- a re-release of a broken zip.
    """
    packages = list(existing.get("packages") or [])
    ident = desc["identifier"]
    current = next((p for p in packages if p.get("identifier") == ident), None)
    versions = [v for v in (current or {}).get("versions", []) if v.get("version") != version["version"]]
    versions.append(version)
    versions.sort(key=lambda v: version_key(v["version"]), reverse=True)
    entry = {**package_entry(desc), "versions": versions}
    packages = [p for p in packages if p.get("identifier") != ident] + [entry]
    packages.sort(key=lambda p: p["identifier"])
    return {"packages": packages}


def write_resources(repo: Path, packages: Dict[str, Any], icons: Dict[str, bytes]) -> Path:
    """resources.zip: <identifier>/icon.png for every package that has an icon.

    Icons already in the archive are kept for packages this run does not touch.
    """
    path = repo / "resources.zip"
    kept: Dict[str, bytes] = {}
    if path.is_file():
        with zipfile.ZipFile(path) as z:
            for name in z.namelist():
                kept[name] = z.read(name)
    for ident, data in icons.items():
        kept[f"{ident}/icon.png"] = data
    live = {p["identifier"] for p in packages["packages"]}
    with zipfile.ZipFile(path, "w") as z:
        for name in sorted(kept):
            if name.split("/", 1)[0] in live:
                _add(z, name, kept[name])
    return path


def resource_ref(url: str, path: Path, now: int) -> Dict[str, Any]:
    return {
        "url": url,
        "sha256": sha256(path),
        "update_timestamp": now,
        "update_time_utc": datetime.fromtimestamp(now, timezone.utc).strftime("%Y-%m-%d %H:%M:%S"),
    }


def release(
    version: str,
    repo: Path,
    download_url: str,
    repo_url: str,
    status: str = "testing",
    plugin: Path = PLUGIN,
    archive_dir: Path | None = None,
    tags: List[str] = RELEASE_TAGS,
    now: int | None = None,
) -> Dict[str, Any]:
    version_key(version)  # a malformed version fails here, not in KiCad
    desc = json.loads((plugin / "pcm.json").read_text(encoding="utf-8"))
    check_complete(plugin, tags)

    repo.mkdir(parents=True, exist_ok=True)
    archive_dir = archive_dir or (plugin.parent / "dist" / "pcm")
    archive = archive_dir / f"pcb-trace-length-analyzer-{version}.zip"
    sizes = build_archive(plugin, desc, version, status, archive)

    ver = version_entry(desc, version, status)
    ver.update(download_url=download_url, download_sha256=sha256(archive), **sizes)

    pkg_path = repo / "packages.json"
    existing = json.loads(pkg_path.read_text(encoding="utf-8")) if pkg_path.is_file() else {}
    packages = merge_packages(existing, desc, ver)
    pkg_path.write_text(json.dumps(packages, indent=2) + "\n", encoding="utf-8")

    resources = write_resources(repo, packages, {desc["identifier"]: (plugin / "icons" / "analyzer-64.png").read_bytes()})

    now = int(time.time()) if now is None else now
    base = repo_url.rstrip("/")
    repository = {
        "$schema": SCHEMA,
        "name": "EmbeddedCI KiCad plugins",
        "maintainer": desc["maintainer"],
        "packages": resource_ref(f"{base}/packages.json", pkg_path, now),
        "resources": resource_ref(f"{base}/resources.zip", resources, now),
    }
    (repo / "repository.json").write_text(json.dumps(repository, indent=2) + "\n", encoding="utf-8")
    return {"archive": archive, "version": ver, "repository": repo / "repository.json"}


def main(argv: List[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--version", required=True)
    ap.add_argument("--repo", required=True, type=Path, help="the repository checkout to update")
    ap.add_argument("--download-url", required=True, help="where the archive will be downloadable from")
    ap.add_argument("--repo-url", required=True, help="the URL repository.json, packages.json and resources.zip are served under")
    ap.add_argument("--status", default="stable", choices=["stable", "testing", "development", "deprecated"])
    ap.add_argument("--archive-dir", type=Path, default=None)
    args = ap.parse_args(argv)
    try:
        out = release(args.version, args.repo, args.download_url, args.repo_url, args.status, archive_dir=args.archive_dir)
    except ReleaseError as e:
        print(f"pcm_release: {e}", file=sys.stderr)
        return 1
    v = out["version"]
    print(f"archive     {out['archive']}  ({v['download_size'] / 1e6:.1f} MB, sha256 {v['download_sha256'][:12]}…)")
    print(f"repository  {out['repository']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
