"""The analyzer engine, run as a child process and spoken to over its pipes.

The engine is the Go control plane embeddedci.com mounts, built as
``pcb-trace-length-analyzer-engine``. It reads requests on stdin and answers on
stdout, framed as ``pipehttp`` describes: a length-prefixed JSON header, then a
length-prefixed body, in both directions. Nothing listens on a port.

One engine serves the plugin for as long as the plugin runs, because the
analysis lives in it: a board is read once into a session, and every question
after that -- the report, the room beside each route, the geometry for the
viewer, an apply -- is asked of that session.

Requests may be made from any thread. Answers come back in whatever order they
finish, matched to their question by id.
"""

from __future__ import annotations

import collections
import json
import os
import platform
import struct
import subprocess
import sys
import threading
from concurrent.futures import Future
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Deque, Dict, Mapping, Optional

ENGINE_NAME = "pcb-trace-length-analyzer-engine"
API = "/api/pcb-trace-length-analyzer"

# Same bound as the Go side: a length beyond this is a corrupt stream.
MAX_FRAME = 512 << 20


class EngineError(RuntimeError):
    """The engine could not be started, or stopped answering."""


class APIError(RuntimeError):
    """The engine answered with an error status."""

    def __init__(self, status: int, message: str):
        super().__init__(message)
        self.status = status


@dataclass
class Response:
    status: int
    headers: Dict[str, str] = field(default_factory=dict)
    body: bytes = b""

    def json(self) -> Any:
        return json.loads(self.body.decode("utf-8")) if self.body else None

    def raise_for_status(self) -> "Response":
        if self.status >= 400:
            message = ""
            try:
                message = (self.json() or {}).get("error", "")
            except (ValueError, AttributeError):
                pass
            raise APIError(self.status, message or f"engine answered {self.status}")
        return self


def platform_tag() -> str:
    """The directory a release keeps this machine's engine in: ``darwin-arm64``."""
    system = {"darwin": "darwin", "linux": "linux", "win32": "windows"}.get(sys.platform, sys.platform)
    machine = platform.machine().lower()
    arch = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(machine, machine)
    return f"{system}-{arch}"


def find_engine(plugin_dir: Optional[Path] = None) -> Path:
    """Locate the engine binary.

    In order: ``$PCB_TLA_ENGINE``; the release layout,
    ``<plugin>/bin/<os>-<arch>/``; a flat ``<plugin>/bin/``; and the repository's
    own ``bin/`` one level up, which is where ``make build`` puts it while the
    plugin is being developed in place.
    """
    exe = ENGINE_NAME + (".exe" if sys.platform == "win32" else "")
    override = os.environ.get("PCB_TLA_ENGINE")
    if override:
        p = Path(override)
        if p.is_file():
            return p
        raise EngineError(f"PCB_TLA_ENGINE points at {override}, which is not a file")
    here = plugin_dir or Path(__file__).resolve().parent.parent
    candidates = [
        here / "bin" / platform_tag() / exe,
        here / "bin" / exe,
        here.parent / "bin" / exe,
    ]
    for c in candidates:
        if c.is_file():
            return _executable(c)
    tried = "\n  ".join(str(c) for c in candidates)
    raise EngineError(f"the analyzer engine is not installed; looked for:\n  {tried}")


def _executable(path: Path) -> Path:
    """Make sure the engine can be run.

    The Plugin and Content Manager installs from a zip, and not every unzip
    keeps the executable bit, so a freshly installed engine may arrive as a
    plain file.
    """
    if sys.platform != "win32" and not os.access(path, os.X_OK):
        try:
            path.chmod(path.stat().st_mode | 0o111)
        except OSError as e:
            raise EngineError(f"the engine at {path} is not executable and could not be made so: {e}") from e
    return path


class Engine:
    """A running engine."""

    def __init__(self, path: Optional[Path] = None, verbose: bool = False):
        self.path = Path(path) if path else find_engine()
        args = [str(self.path)] + (["-v"] if verbose else [])
        creationflags = 0
        if sys.platform == "win32":
            # No console window flashing up behind KiCad.
            creationflags = getattr(subprocess, "CREATE_NO_WINDOW", 0)
        try:
            self._proc = subprocess.Popen(
                args,
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                bufsize=0,
                creationflags=creationflags,
            )
        except OSError as e:
            raise EngineError(f"could not start {self.path}: {e}") from e

        self._write_lock = threading.Lock()
        self._pending: Dict[int, Future] = {}
        self._pending_lock = threading.Lock()
        self._next_id = 1
        self._closed = False
        self._stderr: Deque[str] = collections.deque(maxlen=50)

        self._reader = threading.Thread(target=self._read_loop, name="engine-stdout", daemon=True)
        self._reader.start()
        # stderr has to be drained or a chatty engine fills the pipe and
        # blocks; the tail is kept for error messages.
        threading.Thread(target=self._drain_stderr, name="engine-stderr", daemon=True).start()

    # ---- requests ----

    def request_async(
        self,
        method: str,
        path: str,
        body: bytes = b"",
        headers: Optional[Mapping[str, str]] = None,
    ) -> "Future[Response]":
        fut: Future = Future()
        with self._pending_lock:
            if self._closed:
                fut.set_exception(EngineError("the engine has stopped" + self._stderr_tail()))
                return fut
            rid = self._next_id
            self._next_id += 1
            self._pending[rid] = fut
        header = json.dumps(
            {"id": rid, "method": method, "path": path, "headers": dict(headers or {})}
        ).encode("utf-8")
        frame = struct.pack(">I", len(header)) + header + struct.pack(">I", len(body)) + body
        try:
            with self._write_lock:
                assert self._proc.stdin is not None
                self._proc.stdin.write(frame)
                self._proc.stdin.flush()
        except (OSError, ValueError) as e:
            with self._pending_lock:
                self._pending.pop(rid, None)
            fut.set_exception(EngineError(f"the engine is not accepting requests: {e}" + self._stderr_tail()))
        return fut

    def request(
        self,
        method: str,
        path: str,
        body: bytes = b"",
        headers: Optional[Mapping[str, str]] = None,
        timeout: Optional[float] = 300.0,
    ) -> Response:
        return self.request_async(method, path, body, headers).result(timeout)

    def get_json(self, path: str, timeout: Optional[float] = 300.0) -> Any:
        return self.request("GET", path, timeout=timeout).raise_for_status().json()

    def post_json(self, path: str, payload: Any, timeout: Optional[float] = 300.0) -> Any:
        body = json.dumps(payload).encode("utf-8")
        return (
            self.request("POST", path, body, {"Content-Type": "application/json"}, timeout=timeout)
            .raise_for_status()
            .json()
        )

    # ---- the operations the plugin itself needs ----

    def upload(
        self,
        board_text: bytes,
        filename: str,
        project: Optional[bytes] = None,
        rules: Optional[bytes] = None,
    ) -> Dict[str, Any]:
        """Read a board into a new session. Returns ``{"session", "analysis"}``."""
        from .multipart import encode

        stem = filename[: -len(".kicad_pcb")] if filename.endswith(".kicad_pcb") else filename
        files = [("board", filename, board_text)]
        if project:
            files.append(("project", stem + ".kicad_pro", project))
        if rules:
            files.append(("rules", stem + ".kicad_dru", rules))
        body, content_type = encode(files)
        return (
            self.request("POST", API + "/sessions", body, {"Content-Type": content_type})
            .raise_for_status()
            .json()
        )

    def plan(self, session: str, params: Dict[str, Any]) -> Dict[str, Any]:
        return self.post_json(f"{API}/sessions/{session}/plan", {"params": params})

    def nets(self, session: str, names=(), attention: bool = False) -> Dict[str, Any]:
        from urllib.parse import quote, urlencode

        q = [("name", n) for n in names]
        if attention:
            q.append(("attention", "1"))
        qs = urlencode(q, quote_via=quote)
        return self.get_json(f"{API}/sessions/{session}/nets" + (f"?{qs}" if qs else ""))

    def changes(self, session: str) -> Dict[str, Any]:
        return self.get_json(f"{API}/sessions/{session}/changes")

    # ---- lifecycle ----

    def close(self, timeout: float = 5.0) -> None:
        """Close stdin, which the engine takes as the end, and wait for it."""
        if self._proc.stdin and not self._proc.stdin.closed:
            try:
                self._proc.stdin.close()
            except OSError:
                pass
        try:
            self._proc.wait(timeout)
        except subprocess.TimeoutExpired:
            self._proc.kill()
            self._proc.wait()
        self._reader.join(timeout)

    def __enter__(self) -> "Engine":
        return self

    def __exit__(self, *exc) -> None:
        self.close()

    @property
    def running(self) -> bool:
        return self._proc.poll() is None and not self._closed

    # ---- internals ----

    def _read_exact(self, n: int) -> Optional[bytes]:
        assert self._proc.stdout is not None
        buf = bytearray()
        while len(buf) < n:
            chunk = self._proc.stdout.read(n - len(buf))
            if not chunk:
                return None
            buf.extend(chunk)
        return bytes(buf)

    def _read_chunk(self) -> Optional[bytes]:
        raw = self._read_exact(4)
        if raw is None:
            return None
        (size,) = struct.unpack(">I", raw)
        if size > MAX_FRAME:
            raise EngineError(f"the engine sent a {size} byte frame; the stream is corrupt")
        return self._read_exact(size) if size else b""

    def _read_loop(self) -> None:
        err: Optional[BaseException] = None
        try:
            while True:
                header = self._read_chunk()
                if header is None:
                    break
                body = self._read_chunk()
                if body is None:
                    raise EngineError("the engine's output ended part-way through an answer")
                h = json.loads(header.decode("utf-8"))
                with self._pending_lock:
                    fut = self._pending.pop(int(h.get("id", 0)), None)
                if fut is not None and not fut.done():
                    fut.set_result(Response(int(h.get("status", 0)), h.get("headers") or {}, body))
        except BaseException as e:  # noqa: BLE001 -- every waiter must hear about it
            err = e
        finally:
            with self._pending_lock:
                self._closed = True
                pending, self._pending = self._pending, {}
            if pending:
                # Give the process a moment to exit so its last words are in
                # the tail we report.
                try:
                    self._proc.wait(1.0)
                except subprocess.TimeoutExpired:
                    pass
                reason = str(err) if err else "the engine exited"
                for fut in pending.values():
                    if not fut.done():
                        fut.set_exception(EngineError(reason + self._stderr_tail()))

    def _drain_stderr(self) -> None:
        assert self._proc.stderr is not None
        for line in iter(self._proc.stderr.readline, b""):
            text = line.decode("utf-8", "replace").rstrip()
            self._stderr.append(text)
            if os.environ.get("PCB_TLA_DEBUG"):
                print("engine:", text, file=sys.stderr)

    def _stderr_tail(self) -> str:
        lines = [l for l in list(self._stderr)[-8:] if l]
        return ("\n" + "\n".join(lines)) if lines else ""
