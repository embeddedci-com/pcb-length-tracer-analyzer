"""The page's origin: a URL scheme answered inside this process.

The web front end is served at ``kicad-engine://app/``. Qt hands every request
for that scheme to :class:`SchemeHandler` rather than to the network, so there
is no server and no socket, not even on loopback. Three kinds of path:

``/api/...``
    forwarded to the engine over its pipes, unchanged -- the same requests the
    page makes on embeddedci.com.
``/kicad/...``
    answered by the plugin, which holds the connection to pcbnew: select nets,
    apply a result, read the board again.
anything else
    a file from the built bundle.

Two things plain HTTP has do not survive a scheme handler, and the page's fetch
(``webapp/kicad/fetch.ts``) makes up for both:

- Qt can only answer with status 200. The page's API client needs the real
  status -- a 404 is how it learns a session has gone, a 400 is how it shows
  why a parameter was refused -- so the status travels in ``X-Status``.
- The request body cannot be read: in PySide6 6.10,
  ``QWebEngineUrlRequestJob.requestBody()`` crashes the process, on a POST as
  much as a GET. Request headers arrive intact, so the page sends its body as
  base64 in ``X-Body``.
"""

from __future__ import annotations

import base64
import binascii
import json
import mimetypes
import threading
from concurrent.futures import Future
from pathlib import Path
from typing import Any, Callable, Dict, Optional, Tuple

from PySide6.QtCore import QBuffer, QByteArray, QIODevice, QObject, Signal, Slot
from PySide6.QtWebEngineCore import (
    QWebEngineUrlRequestJob,
    QWebEngineUrlScheme,
    QWebEngineUrlSchemeHandler,
)

SCHEME = b"kicad-engine"
ORIGIN = "kicad-engine://app"


def register_scheme() -> None:
    """Declare the scheme. Must run before the QApplication is created."""
    scheme = QWebEngineUrlScheme(SCHEME)
    scheme.setSyntax(QWebEngineUrlScheme.Syntax.Host)
    scheme.setFlags(
        QWebEngineUrlScheme.Flag.SecureScheme
        | QWebEngineUrlScheme.Flag.LocalAccessAllowed
        | QWebEngineUrlScheme.Flag.CorsEnabled
        | QWebEngineUrlScheme.Flag.FetchApiAllowed
        | QWebEngineUrlScheme.Flag.ContentSecurityPolicyIgnored
    )
    QWebEngineUrlScheme.registerScheme(scheme)


# (status, content type, body, extra headers)
Reply = Tuple[int, str, bytes, Dict[str, str]]


def json_reply(status: int, payload: Any) -> Reply:
    return status, "application/json", json.dumps(payload).encode("utf-8"), {}


class SchemeHandler(QWebEngineUrlSchemeHandler):
    """Answers kicad-engine:// requests.

    ``engine_request(method, path, body, headers) -> Future[Response]`` sends a
    request to the engine. ``kicad_routes`` maps ``/kicad/<name>`` to a function
    taking the decoded JSON body and returning a Future of a JSON-able result;
    raising gives a 409 with the exception's message, which is what the page
    shows.

    Every answer is made on the Qt thread: futures complete on worker threads,
    and their results are handed back through a queued signal.
    """

    _deliver = Signal(object, object)

    def __init__(
        self,
        web_root: Path,
        engine_request: Callable[..., "Future[Any]"],
        kicad_routes: Dict[str, Callable[[Any], "Future[Any]"]],
        parent: Optional[QObject] = None,
    ):
        super().__init__(parent)
        self._root = Path(web_root).resolve()
        self._engine_request = engine_request
        self._routes = kicad_routes
        # Jobs Qt has since destroyed -- the page navigated away mid-request.
        # Replying to one would touch a deleted C++ object.
        self._live: Dict[int, QWebEngineUrlRequestJob] = {}
        self._lock = threading.Lock()
        self._next = 1
        self._deliver.connect(self._on_deliver)

    # ---- Qt entry point ----

    def requestStarted(self, job: QWebEngineUrlRequestJob) -> None:  # noqa: N802 (Qt name)
        url = job.requestUrl()
        path = url.path() or "/"
        method = bytes(job.requestMethod().data()).decode("ascii", "replace").upper()

        headers = {
            bytes(k.data()).decode("latin-1"): bytes(v.data()).decode("latin-1")
            for k, v in self._request_headers(job).items()
        }
        try:
            body = self._body(headers)
        except ValueError as e:
            self._reply(job, json_reply(400, {"error": str(e)}))
            return

        if path.startswith("/api/"):
            full = path + (("?" + url.query()) if url.hasQuery() else "")
            token = self._track(job)
            fut = self._engine_request(method, full, body, headers)
            fut.add_done_callback(lambda f: self._deliver.emit(token, ("engine", f)))
            return

        if path.startswith("/kicad/"):
            name = path[len("/kicad/"):]
            route = self._routes.get(name)
            if route is None or method != "POST":
                self._reply(job, json_reply(404, {"error": f"no such call: {name}"}))
                return
            try:
                payload = json.loads(body or b"{}")
            except ValueError:
                self._reply(job, json_reply(400, {"error": "the request was not JSON"}))
                return
            token = self._track(job)
            fut = route(payload)
            fut.add_done_callback(lambda f: self._deliver.emit(token, ("kicad", f)))
            return

        self._reply(job, self._static(path))

    # ---- replies ----

    def _track(self, job: QWebEngineUrlRequestJob) -> int:
        with self._lock:
            token = self._next
            self._next += 1
            self._live[token] = job
        job.destroyed.connect(lambda *_: self._forget(token))
        return token

    def _forget(self, token: int) -> None:
        with self._lock:
            self._live.pop(token, None)

    @Slot(object, object)
    def _on_deliver(self, token: int, what: Tuple[str, "Future[Any]"]) -> None:
        with self._lock:
            job = self._live.pop(token, None)
        if job is None:
            return
        kind, fut = what
        if kind == "engine":
            try:
                res = fut.result()
                reply: Reply = (
                    res.status,
                    res.headers.get("Content-Type", "application/octet-stream"),
                    res.body,
                    {k: v for k, v in res.headers.items() if k.lower() not in ("content-type", "content-length")},
                )
            except Exception as e:  # noqa: BLE001 -- engine gone: tell the page why
                reply = json_reply(502, {"error": f"the analyzer engine is not answering: {e}"})
        else:
            try:
                reply = json_reply(200, fut.result() or {})
            except Exception as e:  # noqa: BLE001 -- a refusal is shown to the user as is
                reply = json_reply(409, {"error": str(e)})
        self._reply(job, reply)

    def _reply(self, job: QWebEngineUrlRequestJob, reply: Reply) -> None:
        status, content_type, body, headers = reply
        # PySide maps a dict to QMultiMap by iterating each value, so a bare
        # QByteArray arrives as one header per byte ("4, 0, 4"). A list of one
        # is one value.
        extra = {QByteArray(b"X-Status"): [QByteArray(str(status).encode("ascii"))]}
        for k, v in headers.items():
            extra[QByteArray(k.encode("latin-1"))] = [QByteArray(v.encode("latin-1", "replace"))]
        try:
            job.setAdditionalResponseHeaders(extra)
            buf = QBuffer(job)
            buf.setData(QByteArray(body))
            buf.open(QIODevice.OpenModeFlag.ReadOnly)
            job.reply(content_type.split(";")[0].strip().encode("ascii", "replace"), buf)
        except RuntimeError:
            # The job was destroyed between the check and the reply.
            pass

    # ---- helpers ----

    @staticmethod
    def _body(headers: Dict[str, str]) -> bytes:
        """The request body, which the page sends base64-encoded in X-Body.

        Removes the header, so the engine sees the request as it was written.
        """
        for k in list(headers):
            if k.lower() == "x-body":
                raw = headers.pop(k)
                try:
                    return base64.b64decode(raw, validate=True)
                except (binascii.Error, ValueError) as e:
                    raise ValueError(f"the request body was not valid base64: {e}") from e
        return b""

    @staticmethod
    def _request_headers(job: QWebEngineUrlRequestJob) -> Dict[Any, Any]:
        try:
            return dict(job.requestHeaders())
        except Exception:  # noqa: BLE001
            return {}

    def _static(self, path: str) -> Reply:
        rel = path.lstrip("/")
        if rel in ("", "index.html"):
            rel = "kicad.html"
        target = (self._root / rel).resolve()
        # No escaping the bundle with "..".
        if self._root not in target.parents and target != self._root:
            return json_reply(403, {"error": "outside the bundle"})
        if not target.is_file():
            return 404, "text/plain", f"not found: {path}".encode("utf-8"), {}
        mime = mimetypes.guess_type(target.name)[0] or "application/octet-stream"
        if target.suffix == ".js":
            mime = "text/javascript"
        return 200, mime, target.read_bytes(), {"Cache-Control": "no-store"}
