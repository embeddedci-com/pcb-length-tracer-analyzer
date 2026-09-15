"""One analyzer window, however many times the button is pressed.

KiCad starts a new process for every press. The first one takes a lock file and
keeps it fresh; a later one finds the lock alive, leaves a note asking the
window to come forward, waits for the note to be picked up, and exits. The
window reads the board again when it comes forward, since the user has usually
changed it in between.

Files rather than a local socket, because this plugin listens on nothing. A
lock is judged by its heartbeat, not by its pid: a crashed window stops
touching its lock and is taken over within seconds, and a pid reused by some
other process cannot make a dead lock look alive.
"""

from __future__ import annotations

import getpass
import os
import secrets
import tempfile
import time
from pathlib import Path
from typing import Optional

HEARTBEAT_S = 1.0
# A lock not touched for this long belongs to a window that is gone.
STALE_S = 6.0


def _default_dir() -> Path:
    try:
        user = getpass.getuser()
    except Exception:  # noqa: BLE001
        user = "user"
    return Path(tempfile.gettempdir()) / f"pcb-trace-length-analyzer-{user}"


class SingleInstance:
    def __init__(self, directory: Optional[Path] = None):
        self.dir = Path(directory) if directory else _default_dir()
        self.dir.mkdir(parents=True, exist_ok=True)
        self.lock = self.dir / "window.lock"
        self.request = self.dir / "show.request"
        self.owned = False
        # What this process writes into the lock, so it can tell its own lock
        # from one a later press took over after judging this one stale.
        self.token = f"{os.getpid()}-{secrets.token_hex(8)}"

    # ---- the first press ----

    def acquire(self) -> bool:
        """Take the lock. False when a live window already holds it."""
        for _ in range(2):
            try:
                fd = os.open(str(self.lock), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            except FileExistsError:
                if not self._stale():
                    return False
                # Gone without cleaning up. Remove and try once more; if another
                # press got there first, O_EXCL settles it.
                try:
                    self.lock.unlink()
                except FileNotFoundError:
                    pass
                continue
            with os.fdopen(fd, "w") as f:
                f.write(self.token)
            self.owned = True
            # A request left over from before this window existed is not for it.
            self._remove(self.request)
            return True
        return False

    def heartbeat(self) -> None:
        if self.owned and self._mine():
            try:
                os.utime(self.lock, None)
            except FileNotFoundError:
                self.owned = False
        else:
            # A later press judged this window stale and took the lock over.
            self.owned = False

    def take_request(self) -> bool:
        """True, once, when a later press has asked this window to come forward."""
        if self.owned and self.request.exists():
            self._remove(self.request)
            return True
        return False

    def release(self) -> None:
        if self.owned and self._mine():
            self._remove(self.lock)
        self.owned = False

    # ---- a later press ----

    def ask_to_show(self, timeout: float = 4.0) -> bool:
        """Ask the running window to come forward. True when it picked the request up."""
        tmp = self.dir / f"show.{os.getpid()}.tmp"
        tmp.write_text(str(time.time()))
        os.replace(tmp, self.request)
        deadline = time.time() + timeout
        while time.time() < deadline:
            if not self.request.exists():
                return True
            time.sleep(0.1)
        # Nobody took it: the window is hung or gone. Do not leave the request
        # for a window opened later.
        self._remove(self.request)
        return False

    # ---- internals ----

    def _mine(self) -> bool:
        try:
            return self.lock.read_text() == self.token
        except (FileNotFoundError, OSError):
            return False

    def _stale(self) -> bool:
        try:
            return time.time() - self.lock.stat().st_mtime > STALE_S
        except FileNotFoundError:
            return True

    @staticmethod
    def _remove(p: Path) -> None:
        try:
            p.unlink()
        except FileNotFoundError:
            pass
