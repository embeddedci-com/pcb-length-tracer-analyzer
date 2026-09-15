"""The analyzer's window beside pcbnew.

A toolbar of the things that need no report -- read the board again, select
everything out of tolerance, one tolerance for every group, and a line that
follows whatever net is selected in KiCad -- over the web front end, which is
the report itself.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any, List, Optional

from PySide6.QtCore import QObject, QTimer, QUrl, Signal, Slot
from PySide6.QtGui import QIcon
from PySide6.QtWidgets import (
    QCheckBox,
    QDoubleSpinBox,
    QLabel,
    QMainWindow,
    QMessageBox,
    QSizePolicy,
    QToolBar,
    QWidget,
)
from PySide6.QtWebEngineCore import QWebEnginePage, QWebEngineProfile
from PySide6.QtWebEngineWidgets import QWebEngineView

from .controller import APPLY_ENABLED, Controller, format_lookup
from .scheme import ORIGIN, SCHEME, SchemeHandler


class _Bridge(QObject):
    """Carries results from worker threads back to the Qt thread."""

    done = Signal(object, object)  # callback, (result, error)


class _Page(QWebEnginePage):
    """Opens outside links in the system browser rather than in the plugin."""

    def acceptNavigationRequest(self, url: QUrl, nav_type, is_main_frame: bool) -> bool:  # noqa: N802
        if url.scheme() == SCHEME.decode():
            return True
        if url.scheme() in ("http", "https"):
            from PySide6.QtGui import QDesktopServices

            QDesktopServices.openUrl(url)
        return False

    def javaScriptConsoleMessage(self, level, message, line, source):  # noqa: N802
        import os
        import sys

        if os.environ.get("PCB_TLA_DEBUG"):
            print(f"page: {source}:{line}: {message}", file=sys.stderr)


class AnalyzerWindow(QMainWindow):
    SELECTION_POLL_MS = 800

    def __init__(self, controller: Controller, web_root: Path, icon: Optional[Path] = None):
        super().__init__()
        self.ctl = controller
        self.setWindowTitle("PCB Trace Length Analyzer")
        if icon and icon.is_file():
            self.setWindowIcon(QIcon(str(icon)))
        self.resize(1280, 900)

        self._bridge = _Bridge()
        self._bridge.done.connect(self._on_done)

        # A profile of our own, off the record: nothing the page stores is
        # written to disk, and the scheme handler belongs to this window only.
        self._profile = QWebEngineProfile(self)
        self._handler = SchemeHandler(
            web_root,
            engine_request=self._engine_request,
            kicad_routes=self._routes(),
            parent=self,
        )
        self._profile.installUrlSchemeHandler(SCHEME, self._handler)
        self.view = QWebEngineView(self)
        self.view.setPage(_Page(self._profile, self.view))
        self.setCentralWidget(self.view)

        self._build_toolbar()
        self.statusBar().showMessage("Reading the board from KiCad…")

        self._last_selection: List[str] = []
        self._poll = QTimer(self)
        self._poll.setInterval(self.SELECTION_POLL_MS)
        self._poll.timeout.connect(self._poll_selection)
        self._polling = False

    def _engine_request(self, method, path, body, headers):
        """Forward a request to the engine, and keep the rules when the page changes them."""
        import json
        import re

        fut = self.ctl.engine.request_async(method, path, body, headers)
        if method == "POST" and re.search(r"/sessions/[^/]+/(plan|apply)$", path.split("?")[0]):

            def keep(f):
                try:
                    res = f.result()
                    if res.status == 200:
                        self.ctl.remember_response(json.loads(res.body))
                except Exception:  # noqa: BLE001 -- the page still gets its answer
                    pass

            fut.add_done_callback(keep)
        return fut

    def _routes(self):
        routes = {
            "select": lambda p: self.ctl.on_kicad(lambda: self.ctl.select(list(p.get("nets") or []))),
            "rescan": lambda p: self.ctl.on_kicad(lambda: {"session": self.ctl.rescan()}),
        }
        # Not offered at all while switched off, so no request from the page
        # can reach it.
        if APPLY_ENABLED:
            routes["apply"] = lambda p: self.ctl.on_kicad(lambda: self.ctl.apply(str(p.get("session") or "")))
        return routes

    # ---- toolbar ----

    def _build_toolbar(self) -> None:
        tb = QToolBar("Analyzer", self)
        tb.setMovable(False)
        self.addToolBar(tb)

        tb.addAction("Rescan", self.rescan).setToolTip(
            "Read the board from KiCad again, with any edits made since"
        )
        tb.addAction("Select what needs attention", self.select_attention).setToolTip(
            "Select every net that is out of tolerance, on every interface, in KiCad"
        )
        tb.addSeparator()

        self._tol_on = QCheckBox("One tolerance for every group:")
        self._tol_on.setToolTip(
            "Hold every group to the same tolerance instead of each group's default. "
            "Per-group tolerances can still be set in the report."
        )
        self._tol = QDoubleSpinBox()
        self._tol.setDecimals(3)
        self._tol.setRange(0.005, 50.0)
        self._tol.setSingleStep(0.05)
        self._tol.setSuffix(" mm")
        self._tol.setValue(0.635)
        self._tol.setEnabled(False)
        self._tol_on.toggled.connect(self._tol.setEnabled)
        self._tol_on.toggled.connect(lambda _: self._tolerance_changed())
        self._tol.editingFinished.connect(self._tolerance_changed)
        tb.addWidget(self._tol_on)
        tb.addWidget(self._tol)
        tb.addSeparator()

        self._follow = QCheckBox("Follow KiCad selection")
        self._follow.setChecked(True)
        self._follow.setToolTip("Show the length of whichever net is selected in KiCad")
        self._follow.toggled.connect(self._follow_toggled)
        tb.addWidget(self._follow)

        spacer = QWidget()
        spacer.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Preferred)
        tb.addWidget(spacer)

        self._net_line = QLabel("")
        self._net_line.setTextInteractionFlags(self._net_line.textInteractionFlags())
        self.statusBar().addPermanentWidget(self._net_line, 1)

    # ---- actions ----

    def start(self) -> None:
        self.rescan()
        if self._follow.isChecked():
            self._poll.start()

    def bring_forward(self) -> None:
        """The button was pressed again: show this window and read the board afresh."""
        from PySide6.QtCore import Qt

        if self.isMinimized():
            self.showNormal()
        self.show()
        # macOS will not hand focus to a background process on request. A
        # moment on top gets the window in front of pcbnew either way.
        self.setWindowFlag(Qt.WindowType.WindowStaysOnTopHint, True)
        self.show()
        self.raise_()
        self.activateWindow()
        QTimer.singleShot(300, self._drop_on_top)
        self.rescan()

    def _drop_on_top(self) -> None:
        from PySide6.QtCore import Qt

        self.setWindowFlag(Qt.WindowType.WindowStaysOnTopHint, False)
        self.show()

    def rescan(self) -> None:
        self.statusBar().showMessage("Reading the board from KiCad…")
        self._run_kicad(self.ctl.rescan, self._rescanned)

    def _rescanned(self, sid: Any, err: Optional[BaseException]) -> None:
        if self.view is None:
            return
        if err:
            self.statusBar().showMessage("Could not read the board")
            QMessageBox.warning(self, "Could not read the board", str(err))
            self.view.setUrl(QUrl(f"{ORIGIN}/index.html"))
            return
        self.statusBar().showMessage(f"Read {self.ctl.filename} from {self.ctl.source}", 8000)
        from . import __version__

        self.view.setUrl(QUrl(f"{ORIGIN}/index.html?session={sid}&v={__version__}"))
        self._last_selection = []

    def select_attention(self) -> None:
        if not self.ctl.session:
            return

        def work():
            nets = self.ctl.attention_nets()
            if nets:
                self.ctl.select(nets)
            return len(nets)

        def done(n: Any, err: Optional[BaseException]) -> None:
            if err:
                QMessageBox.warning(self, "Could not select", str(err))
            elif n == 0:
                self.statusBar().showMessage("Every matched net is within tolerance", 8000)
            else:
                self.statusBar().showMessage(f"Selected {n} net{'s' if n != 1 else ''} out of tolerance in KiCad", 8000)

        self._run_kicad(work, done)

    def _tolerance_changed(self) -> None:
        if not self.ctl.session:
            return
        mm = self._tol.value() if self._tol_on.isChecked() else None

        def done(_: Any, err: Optional[BaseException]) -> None:
            if err:
                QMessageBox.warning(self, "Tolerance not applied", str(err))
                return
            self.statusBar().showMessage(
                f"Every group held to ±{mm:.3f} mm" if mm is not None else "Each group back to its own tolerance",
                8000,
            )
            if self.view is not None:
                self.view.reload()
            self._last_selection = []

        self._run_kicad(lambda: self.ctl.set_tolerance_everywhere(mm), done)

    # ---- selection following ----

    def _follow_toggled(self, on: bool) -> None:
        if on:
            self._last_selection = []
            self._poll.start()
        else:
            self._poll.stop()
            self._net_line.setText("")

    def _poll_selection(self) -> None:
        if self._polling or not self.ctl.session:
            return
        self._polling = True

        def work():
            nets = self.ctl.selected_nets()
            if nets == self._last_selection:
                return None
            return nets, (self.ctl.lookup(nets[:8]) if nets else None)

        def done(res: Any, err: Optional[BaseException]) -> None:
            self._polling = False
            if err or res is None:
                return
            nets, lookup = res
            self._last_selection = nets
            if not nets:
                self._net_line.setText("")
                return
            lines = format_lookup(lookup or {})
            more = f"  (+{len(nets) - 8} more nets selected)" if len(nets) > 8 else ""
            self._net_line.setText("   |   ".join(lines) + more)
            self._net_line.setToolTip("\n".join(lines))

        self._run_kicad(work, done)

    # ---- plumbing ----

    def _run_kicad(self, fn, callback) -> None:
        fut = self.ctl.on_kicad(fn)

        def finished(f):
            try:
                self._bridge.done.emit(callback, (f.result(), None))
            except BaseException as e:  # noqa: BLE001
                self._bridge.done.emit(callback, (None, e))

        fut.add_done_callback(finished)

    @Slot(object, object)
    def _on_done(self, callback, outcome) -> None:
        result, err = outcome
        callback(result, err)

    def closeEvent(self, event) -> None:  # noqa: N802
        self._poll.stop()
        self.release_web()
        super().closeEvent(event)

    def release_web(self) -> None:
        """Destroy the page, then the view, now -- before the profile.

        Qt destroys a window's children in the order they were made, which puts
        the profile before the page that uses it: Qt warns "Release of profile
        requested but WebEnginePage still not deleted", and the process then
        dies with a bus error on the way out. Deleting the page first, at once
        rather than with deleteLater, is the order QtWebEngine needs.
        """
        import shiboken6

        view = getattr(self, "view", None)
        if view is None or not shiboken6.isValid(view):
            return
        page = view.page()
        view.hide()
        if page is not None and shiboken6.isValid(page):
            shiboken6.delete(page)
        shiboken6.delete(view)
        self.view = None
