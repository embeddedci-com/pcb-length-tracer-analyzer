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


def plugin_identifier() -> str:
    """The identifier of the installed copy running this code.

    A development install links this source into KiCad under a plugin.json of
    its own, with its own identifier; that file sits beside the entry point
    KiCad ran, not beside this source. KICAD_PLUGIN_DIR is not something KiCad
    promises to set, so the entry point's own directory is asked first.
    """
    import json

    for d in (Path(sys.argv[0]).absolute().parent, PLUGIN_DIR):
        try:
            return json.loads((d / "plugin.json").read_text())["identifier"]
        except (OSError, ValueError, KeyError):
            continue
    return "pcb-trace-length-analyzer"


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

    instance = SingleInstance(name=plugin_identifier())
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

    ctl = Controller(engine, identifier=plugin_identifier())
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
    """How far the selected net is from its target, in a panel that leaves KiCad focused.

    With nothing selected the panel waits for a click in the PCB Editor.
    """
    argv = list(sys.argv if argv is None else argv)
    app = _app(argv)
    from concurrent.futures import ThreadPoolExecutor

    from . import boardio
    from .controller import Controller
    from .engine import Engine
    from .macos import make_accessory
    from .netpanel import NetPanel

    # Show without activating Python: KiCad keeps focus, and an open report
    # window is not brought forward.
    make_accessory()

    try:
        engine = Engine()
    except Exception as e:  # noqa: BLE001
        return _fatal("The analyzer engine could not start", e)
    # The same rules the report was last left with, for this board.
    ctl = Controller(engine, identifier=plugin_identifier())
    # KiCad calls from one thread only, as the report window does.
    worker = ThreadPoolExecutor(max_workers=1, thread_name_prefix="kicad")

    def run(fn, callback):
        fut = worker.submit(fn)
        fut.add_done_callback(lambda f: callback(*((f.result(), None) if not f.exception() else (None, f.exception()))))

    panel = NetPanel(
        read_board=ctl.rescan,
        selected=ctl.selected_nets,
        lookup=ctl.lookup,
        run=run,
        icon=PLUGIN_DIR / "icons" / "net-length-48.png",
    )
    panel.destroyed.connect(app.quit)
    panel.start()
    code = app.exec()
    worker.shutdown(wait=False, cancel_futures=True)
    engine.close()
    return code
