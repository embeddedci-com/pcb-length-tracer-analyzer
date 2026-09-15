"""Entry points: what KiCad runs when a toolbar button is pressed.

Each press starts a fresh process, so each entry point starts its own engine
and connects to KiCad itself.
"""

from __future__ import annotations

import os
import sys
import traceback
from pathlib import Path

PLUGIN_DIR = Path(__file__).resolve().parent.parent

# KiCad's ` key highlights a net without selecting anything, and the API can
# only see the selection -- it has no call for the highlighted net. So a net
# picked out with ` looks like nothing at all from here.
NOTHING_SELECTED = (
    "Nothing is selected in the PCB Editor.\n\n"
    "The ` key highlights a net but does not select it, and KiCad does not let "
    "plugins see which net is highlighted.\n\n"
    "Click a track, via or pad instead. To select the whole trace, click a track "
    "and press U (Select/Expand Connection); then run this again."
)
WEB_ROOT = PLUGIN_DIR / "web"
ICON = PLUGIN_DIR / "icons" / "analyzer-48.png"


def _app(argv):
    # The scheme has to be declared before the application exists.
    from PySide6.QtWidgets import QApplication

    from .scheme import register_scheme

    register_scheme()
    app = QApplication.instance() or QApplication(argv)
    app.setApplicationName("PCB Trace Length Analyzer")
    return app


def _fatal(title: str, err: BaseException) -> int:
    from PySide6.QtWidgets import QMessageBox

    detail = "".join(traceback.format_exception(type(err), err, err.__traceback__))
    if os.environ.get("PCB_TLA_DEBUG"):
        print(detail, file=sys.stderr)
    box = QMessageBox(QMessageBox.Icon.Critical, title, str(err))
    box.setDetailedText(detail)
    box.exec()
    return 1


def analyze(argv=None) -> int:
    """Open the analyzer window on the board open in pcbnew.

    If one is already open, bring it forward (it reads the board again) and
    exit, before paying for Qt, a web view and another engine.
    """
    argv = list(sys.argv if argv is None else argv)
    from .single import HEARTBEAT_S, SingleInstance

    instance = SingleInstance()
    if not instance.acquire():
        if instance.ask_to_show():
            return 0
        # The window holding the lock did not answer: take over from it.
        if not instance.acquire():
            return 0
    try:
        return _analyze(argv, instance, HEARTBEAT_S)
    finally:
        instance.release()


def _analyze(argv, instance, heartbeat_s: float) -> int:
    app = _app(argv)
    from PySide6.QtCore import QTimer

    from .controller import Controller
    from .engine import Engine
    from .window import AnalyzerWindow

    if not (WEB_ROOT / "kicad.html").is_file():
        return _fatal(
            "The report is not built",
            FileNotFoundError(f"{WEB_ROOT}/kicad.html is missing; run `make plugin` in the repository"),
        )
    try:
        engine = Engine(verbose=bool(os.environ.get("PCB_TLA_DEBUG")))
    except Exception as e:  # noqa: BLE001
        return _fatal("The analyzer engine could not start", e)

    ctl = Controller(engine)
    win = AnalyzerWindow(ctl, WEB_ROOT, ICON)
    win.show()
    win.raise_()
    win.activateWindow()
    win.start()

    def tick() -> None:
        instance.heartbeat()
        if instance.take_request():
            win.bring_forward()

    timer = QTimer()
    timer.setInterval(int(heartbeat_s * 1000))
    timer.timeout.connect(tick)
    timer.start()
    code = app.exec()
    timer.stop()
    # The page before its profile, and both before the engine they talk to.
    win.release_web()
    win.deleteLater()
    app.processEvents()
    ctl.close()
    return code


def net_length(argv=None) -> int:
    """Say how far each selected net is from its target, with no report at all."""
    argv = list(sys.argv if argv is None else argv)
    _app(argv)
    from PySide6.QtWidgets import QMessageBox

    from . import boardio
    from .controller import Controller, format_lookup
    from .engine import Engine

    try:
        kicad, board = boardio.connect()
        nets = boardio.selected_nets(board)
        if not nets:
            QMessageBox.information(
                None,
                "Trace length",
                NOTHING_SELECTED,
            )
            return 0
        with Engine() as engine:
            ctl = Controller(engine, kicad, board)
            ctl.rescan()
            lines = format_lookup(ctl.lookup(nets))
    except Exception as e:  # noqa: BLE001
        return _fatal("Could not measure the selected net", e)

    box = QMessageBox(QMessageBox.Icon.Information, "Trace length", "\n\n".join(lines))
    box.exec()
    return 0
