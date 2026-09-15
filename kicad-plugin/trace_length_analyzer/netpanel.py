"""The Selected Net Length panel: a small floating window over the PCB Editor.

It never takes focus. KiCad keeps the keyboard and the mouse, so a click on a
track goes straight to KiCad rather than first bringing KiCad back to the
front, and no other plugin window -- an open report, say -- is pulled forward.

With something selected it shows that net's length against its target. With
nothing selected it asks for a click and waits: KiCad offers plugins no way to
change its cursor or start a pick tool, so the panel watches the selection
instead, and answers as soon as there is one. It keeps following the selection
until it is closed or left alone for a while.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any, Callable, List, Optional

from PySide6.QtCore import QObject, QPoint, Qt, QTimer, QUrl, Signal
from PySide6.QtGui import QCursor, QDesktopServices, QGuiApplication, QPixmap
from PySide6.QtWidgets import QFrame, QHBoxLayout, QLabel, QPushButton, QVBoxLayout, QWidget

from .controller import format_lookup

POLL_MS = 300
# Closed after this long with nothing new to show.
IDLE_CLOSE_MS = 90_000
HOMEPAGE = "https://embeddedci.com/tools/pcb-trace-length-analyzer"


class _Bridge(QObject):
    done = Signal(object, object)


class NetPanel(QWidget):
    """The floating panel.

    ``read_board()`` loads the board into the engine; ``selected()`` returns the
    selected nets; ``lookup(nets)`` returns the engine's answer for them. All
    three run on a worker thread supplied by ``run`` (``run(fn, callback)``).
    """

    def __init__(
        self,
        read_board: Callable[[], Any],
        selected: Callable[[], List[str]],
        lookup: Callable[[List[str]], Any],
        run: Callable[[Callable[[], Any], Callable[[Any, Optional[BaseException]], None]], None],
        icon: Optional[Path] = None,
    ):
        super().__init__(None)
        self._read_board, self._selected, self._lookup, self._run = read_board, selected, lookup, run
        self._ready = False
        self._busy = False
        self._shown: Optional[List[str]] = None

        self.setWindowFlags(
            Qt.WindowType.Tool
            | Qt.WindowType.FramelessWindowHint
            | Qt.WindowType.WindowStaysOnTopHint
            | Qt.WindowType.WindowDoesNotAcceptFocus
        )
        self.setAttribute(Qt.WidgetAttribute.WA_ShowWithoutActivating, True)
        self.setAttribute(Qt.WidgetAttribute.WA_DeleteOnClose, True)

        frame = QFrame(self)
        frame.setObjectName("panel")
        frame.setStyleSheet(
            "#panel { background: palette(window); border: 1px solid palette(mid); border-radius: 8px; }"
        )
        outer = QVBoxLayout(self)
        outer.setContentsMargins(0, 0, 0, 0)
        outer.addWidget(frame)
        box = QVBoxLayout(frame)
        box.setContentsMargins(12, 10, 10, 10)
        box.setSpacing(6)

        head = QHBoxLayout()
        self._icon = QLabel()
        if icon and icon.is_file():
            self._icon.setPixmap(QPixmap(str(icon)).scaled(28, 28, Qt.AspectRatioMode.KeepAspectRatio,
                                                           Qt.TransformationMode.SmoothTransformation))
        head.addWidget(self._icon)
        self._title = QLabel("<b>Trace length</b>")
        head.addWidget(self._title, 1)
        close = QPushButton("✕")
        close.setFlat(True)
        close.setFixedSize(24, 24)
        close.setFocusPolicy(Qt.FocusPolicy.NoFocus)
        close.setToolTip("Close")
        close.clicked.connect(self.close)
        head.addWidget(close)
        box.addLayout(head)

        self._body = QLabel("Reading the board from KiCad…")
        self._body.setWordWrap(True)
        self._body.setTextFormat(Qt.TextFormat.RichText)
        self._body.setMinimumWidth(420)
        self._body.setMaximumWidth(620)
        box.addWidget(self._body)

        foot = QLabel(f'<a href="{HOMEPAGE}">Plugin by embeddedci.com</a>')
        foot.setTextFormat(Qt.TextFormat.RichText)
        foot.setStyleSheet("color: palette(mid); font-size: 11px;")
        foot.linkActivated.connect(lambda url: QDesktopServices.openUrl(QUrl(url)))
        box.addWidget(foot, 0, Qt.AlignmentFlag.AlignRight)

        self._bridge = _Bridge()
        self._bridge.done.connect(lambda cb, res: cb(*res))
        self._poll = QTimer(self)
        self._poll.setInterval(POLL_MS)
        self._poll.timeout.connect(self._tick)
        self._idle = QTimer(self)
        self._idle.setSingleShot(True)
        self._idle.setInterval(IDLE_CLOSE_MS)
        self._idle.timeout.connect(self.close)

    # ---- lifecycle ----

    def start(self) -> None:
        self.adjustSize()
        self._place()
        self.show()
        self._idle.start()
        self._work(self._read_board, self._board_read)

    def _place(self) -> None:
        """Beside the pointer, kept on its screen."""
        at = QCursor.pos() + QPoint(24, 24)
        screen = QGuiApplication.screenAt(QCursor.pos()) or QGuiApplication.primaryScreen()
        area = screen.availableGeometry()
        w, h = max(self.width(), 460), max(self.height(), 90)
        x = min(max(at.x(), area.left() + 8), area.right() - w - 8)
        y = min(max(at.y(), area.top() + 8), area.bottom() - h - 8)
        self.move(x, y)

    def _work(self, fn, callback) -> None:
        self._run(fn, lambda res, err: self._bridge.done.emit(callback, (res, err)))

    # ---- states ----

    def _board_read(self, _: Any, err: Optional[BaseException]) -> None:
        if err:
            self._say(f"<b>Could not read the board.</b><br>{_esc(err)}")
            return
        self._ready = True
        self._tick()
        self._poll.start()

    def _tick(self) -> None:
        if not self._ready or self._busy:
            return
        self._busy = True

        def work():
            nets = self._selected()
            if nets == self._shown:
                return None
            return nets, (self._lookup(nets[:8]) if nets else None)

        self._work(work, self._answered)

    def _answered(self, res: Any, err: Optional[BaseException]) -> None:
        self._busy = False
        if err:
            self._say(f"<b>Could not measure the selection.</b><br>{_esc(err)}")
            return
        if res is None:
            return
        nets, lookup = res
        self._shown = nets
        self._idle.start()
        if not nets:
            self._say(
                "🔍 <b>Click a track, via or pad in the PCB Editor.</b><br>"
                "<span style='color: gray'>` only highlights. Click a track, then U selects the whole trace.</span>"
            )
            return
        lines = [_esc(l) for l in format_lookup(lookup or {})]
        more = f"<br><span style='color: gray'>and {len(nets) - 8} more selected</span>" if len(nets) > 8 else ""
        self._say("<br>".join(_emphasise(l) for l in lines) + more)

    def _say(self, html: str) -> None:
        self._body.setText(html)
        self.adjustSize()

    def closeEvent(self, event) -> None:  # noqa: N802
        self._poll.stop()
        self._idle.stop()
        super().closeEvent(event)


def _esc(s: Any) -> str:
    return str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def _emphasise(line: str) -> str:
    """Make the verdict stand out in a line from format_lookup."""
    for word, colour in (("(too short)", "#c05a00"), ("(too long)", "#c02020"), ("within tolerance", "#2b8a3e")):
        if word in line:
            return line.replace(word, f"<b style='color:{colour}'>{word}</b>")
    return line
