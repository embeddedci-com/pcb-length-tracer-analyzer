"""Cut a plugin release: bump, build, publish, in one command.

The parts were always separate commands, which meant five chances to do them
in the wrong order. Two of those orderings are traps: packages.json must not
be pushed before the archive it points at exists, or every KiCad that refreshes
in between gets a download error; and the version in __init__.py must match the
one being built, or the plugin reports a version nobody can install.

So this does the lot, in the order that is safe, and stops before anything
leaves the machine to show what it is about to publish:

    python scripts/release.py 0.1.3              # build, then ask; a stable release
    python scripts/release.py 0.1.3 --status testing
    python scripts/release.py 0.1.3 --no-publish # build only, print the rest
    python scripts/release.py 0.1.3 --yes        # for a script

Everything before the confirmation is local and repeatable: a failed build
leaves nothing published and nothing pushed.
"""

from __future__ import annotations

import argparse
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import List, Optional, Sequence, Tuple

PLUGIN = Path(__file__).resolve().parent.parent
ROOT = PLUGIN.parent
INIT = PLUGIN / "trace_length_analyzer" / "__init__.py"

VERSION_RE = re.compile(r'^__version__ = "([^"]+)"', re.M)


class Stop(SystemExit):
    """A check said no. The message is for the person, not a traceback."""

    def __init__(self, message: str):
        super().__init__(f"release: {message}")


# ---- versions ----


def parse_version(v: str) -> Tuple[int, ...]:
    if not re.fullmatch(r"\d+\.\d+\.\d+", v):
        raise Stop(f"{v!r} is not a version like 0.1.3")
    return tuple(int(p) for p in v.split("."))


# INIT is looked up at call time, not bound as a default: a default argument is
# evaluated once when the function is defined, which would make the path
# impossible to point somewhere else in a test.
def current_version(init: Optional[Path] = None) -> str:
    init = init or INIT
    m = VERSION_RE.search(init.read_text())
    if not m:
        raise Stop(f"no __version__ in {init}")
    return m.group(1)


def bump(version: str, init: Optional[Path] = None) -> bool:
    """Write the version. Returns whether the file changed."""
    init = init or INIT
    text = init.read_text()
    if current_version(init) == version:
        return False
    init.write_text(VERSION_RE.sub(f'__version__ = "{version}"', text, count=1))
    return True


# ---- shell ----


def run(cmd: Sequence[str], cwd: Optional[Path] = None, capture: bool = False) -> str:
    """Run a command, stopping the release if it fails."""
    cwd = cwd or ROOT
    if capture:
        p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    else:
        print(f"  $ {' '.join(cmd)}", flush=True)
        p = subprocess.run(cmd, cwd=cwd, text=True)
    if p.returncode != 0:
        detail = (p.stderr or "").strip() if capture else ""
        raise Stop(f"{' '.join(cmd)} failed{': ' + detail if detail else ''}")
    return (p.stdout or "") if capture else ""


def git_dirty(repo: Path, ignoring: Optional[Path] = None) -> List[str]:
    """Paths with changes, other than one the release is allowed to have made."""
    out = run(["git", "status", "--porcelain"], cwd=repo, capture=True)
    paths = []
    for line in out.splitlines():
        p = line[3:].strip()
        if ignoring is not None and Path(repo / p).resolve() == ignoring.resolve():
            continue
        paths.append(p)
    return paths


# ---- the flow ----


def preflight(version: str, pcm_repo: Path, publish: bool, skip_tests: bool) -> None:
    if parse_version(version) <= parse_version(current_version()):
        raise Stop(f"{version} is not newer than the released {current_version()}")

    dirty = git_dirty(ROOT, ignoring=INIT)  # ROOT and INIT read at call time
    if dirty:
        raise Stop(
            "commit or stash first, so the release is of something that exists in "
            "history:\n  " + "\n  ".join(dirty)
        )

    if publish:
        if not shutil.which("gh"):
            raise Stop("gh is not installed, and the release is uploaded with it")
        run(["gh", "auth", "status"], capture=True)
        if not (pcm_repo / ".git").exists():
            raise Stop(f"{pcm_repo} is not a checkout of the repository the plugin is served from")
        pcm_dirty = git_dirty(pcm_repo)
        if pcm_dirty:
            raise Stop(f"{pcm_repo} has uncommitted changes:\n  " + "\n  ".join(pcm_dirty))

    if not skip_tests:
        print("tests")
        run(["make", "test"])


def build(version: str, status: str, pcm_repo: Path) -> None:
    print(f"building {version}")
    bump(version)
    run(["make", "pcm-release", f"VERSION={version}", f"PCM_STATUS={status}", f"PCM_REPO={pcm_repo}"])


def publish_steps(version: str, status: str, pcm_repo: Path, github: str, archive: Path) -> List[List[str]]:
    """The commands that publish, in the only order that is safe.

    The release first: packages.json names an archive by URL, and a KiCad that
    refreshes between the two would try to download one that is not there.
    """
    tag = f"pcb-trace-length-analyzer-v{version}"
    return [
        ["gh", "release", "create", tag, str(archive), "-R", github,
         "--title", f"PCB Trace Length Analyzer {version}",
         "--notes", f"Plugin and engine {version} ({status})."],
        ["git", "-C", str(pcm_repo), "add", "-A"],
        ["git", "-C", str(pcm_repo), "commit", "-m", f"PCB Trace Length Analyzer {version}"],
        ["git", "-C", str(pcm_repo), "push"],
        ["git", "commit", "-am", f"Release {version}"],
        ["git", "push"],
    ]


def confirm(version: str, status: str, github: str, archive: Path, yes: bool) -> bool:
    size = archive.stat().st_size / 1e6 if archive.exists() else 0
    print()
    print(f"  version    {version} ({status})")
    print(f"  archive    {archive.name}  {size:.1f} MB")
    print(f"  release    https://github.com/{github}/releases/tag/pcb-trace-length-analyzer-v{version}")
    print(f"  then       the repository index, so KiCad offers it")
    print()
    if yes:
        return True
    try:
        return input("publish this? [y/N] ").strip().lower() in ("y", "yes")
    except EOFError:
        return False


def main(argv: List[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("version", help="the version to release, e.g. 0.1.3")
    # Stable unless said otherwise: every release so far has been one, and a
    # default of "testing" marks a version wrong for everyone who forgets.
    ap.add_argument("--status", default="stable", choices=["stable", "testing", "development", "deprecated"])
    ap.add_argument("--repo", type=Path, default=ROOT.parent / "kicad-plugins")
    ap.add_argument("--github", default="embeddedci-com/kicad-plugins")
    ap.add_argument("--no-publish", action="store_true", help="build only, and print what publishing would run")
    ap.add_argument("--yes", action="store_true", help="do not ask before publishing")
    ap.add_argument("--skip-tests", action="store_true")
    args = ap.parse_args(argv)

    publish = not args.no_publish
    preflight(args.version, args.repo, publish, args.skip_tests)
    build(args.version, args.status, args.repo)

    archive = ROOT / "dist" / "pcm" / f"pcb-trace-length-analyzer-{args.version}.zip"
    steps = publish_steps(args.version, args.status, args.repo, args.github, archive)

    if not publish:
        print("\nbuilt but not published. To publish:")
        for s in steps:
            print("  " + " ".join(s))
        return 0

    if not confirm(args.version, args.status, args.github, archive, args.yes):
        print("nothing published. The build is in dist/pcm and the version is bumped.")
        return 1

    for s in steps:
        run(s)
    print(f"\npublished {args.version}. KiCad caches the repository per session: restart it to see the update.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
