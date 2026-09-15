"""Draw the toolbar icons: a meander between two pads, with a length bracket.

Run with any Python that has PySide6:  python scripts/make_icons.py
Writes icons/<name>-<size>[-dark].png. The PNGs are committed; this is here so
they can be redrawn rather than edited by hand.
"""

import sys
from pathlib import Path

from PySide6.QtCore import QPointF, QRectF, Qt
from PySide6.QtGui import QColor, QGuiApplication, QImage, QPainter, QPainterPath, QPen

OUT = Path(__file__).resolve().parent.parent / "icons"


def draw(size: int, fg: QColor, accent: QColor, kind: str) -> QImage:
    img = QImage(size, size, QImage.Format.Format_ARGB32)
    img.fill(Qt.GlobalColor.transparent)
    p = QPainter(img)
    p.setRenderHint(QPainter.RenderHint.Antialiasing)
    s = size / 24.0
    pad = QRectF(1.5 * s, 14 * s, 4 * s, 4 * s)
    p.setPen(Qt.PenStyle.NoPen)
    p.setBrush(fg)
    p.drawRoundedRect(pad, 0.8 * s, 0.8 * s)
    p.drawRoundedRect(QRectF(18.5 * s, 14 * s, 4 * s, 4 * s), 0.8 * s, 0.8 * s)
    # The meander.
    path = QPainterPath(QPointF(5.5 * s, 16 * s))
    # Three periods, each: up, across, down, along.
    x = 6.5
    for _ in range(3):
        path.lineTo(QPointF(x * s, 16 * s))
        path.lineTo(QPointF(x * s, 9.5 * s))
        path.lineTo(QPointF((x + 2.0) * s, 9.5 * s))
        path.lineTo(QPointF((x + 2.0) * s, 16 * s))
        x += 4.0
    path.lineTo(QPointF(18.5 * s, 16 * s))
    pen = QPen(accent, max(1.0, 1.0 * s))
    pen.setJoinStyle(Qt.PenJoinStyle.MiterJoin)
    p.setPen(pen)
    p.setBrush(Qt.BrushStyle.NoBrush)
    p.drawPath(path)
    # The length bracket, or a magnifier for the single-net lookup.
    pen = QPen(fg, max(1.0, 1.2 * s))
    p.setPen(pen)
    if kind == "analyzer":
        p.drawLine(QPointF(3.5 * s, 5 * s), QPointF(20.5 * s, 5 * s))
        p.drawLine(QPointF(3.5 * s, 3 * s), QPointF(3.5 * s, 7 * s))
        p.drawLine(QPointF(20.5 * s, 3 * s), QPointF(20.5 * s, 7 * s))
    else:
        p.drawEllipse(QRectF(13 * s, 1.5 * s, 6 * s, 6 * s))
        p.drawLine(QPointF(18.2 * s, 6.7 * s), QPointF(21.5 * s, 9.5 * s))
    p.end()
    return img


def main() -> int:
    QGuiApplication(sys.argv)
    OUT.mkdir(exist_ok=True)
    light = (QColor("#3a3a3a"), QColor("#c07a00"))
    dark = (QColor("#e6e6e6"), QColor("#ffb52e"))
    for kind in ("analyzer", "net-length"):
        for size in (24, 48):
            draw(size, *light, kind).save(str(OUT / f"{kind}-{size}.png"))
            draw(size, *dark, kind).save(str(OUT / f"{kind}-{size}-dark.png"))
    # The Plugin and Content Manager's icon: 64x64, light only.
    draw(64, *light, "analyzer").save(str(OUT / "analyzer-64.png"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
