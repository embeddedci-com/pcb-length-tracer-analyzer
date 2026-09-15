"""The Selected Net Length panel, with KiCad and the engine stood in for."""

import os
import time

import pytest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")
pytest.importorskip("PySide6.QtWidgets")

from PySide6.QtCore import QEventLoop, Qt, QTimer  # noqa: E402
from PySide6.QtWidgets import QApplication  # noqa: E402

from trace_length_analyzer.netpanel import NetPanel  # noqa: E402


@pytest.fixture(scope="module")
def qapp():
    return QApplication.instance() or QApplication(["test"])


def spin(pred, timeout=5.0):
    loop = QEventLoop()
    end = time.time() + timeout
    while time.time() < end and not pred():
        QTimer.singleShot(20, loop.quit)
        loop.exec()
    return pred()


def run_inline(fn, callback):
    try:
        callback(fn(), None)
    except Exception as e:  # noqa: BLE001
        callback(None, e)


ROW = {
    "net": "/ddr4/DDR_A6", "label": "A6", "interface": "DDR", "group": "address/command U3->U4",
    "routed": True, "length_mm": 18.891, "target_mm": 28.486, "tolerance_mm": 3.55,
    "deviation_mm": -9.595, "in_tolerance": False, "need_mm": 9.595,
    "parts": {"track_mm": 12.0, "via_mm": 3.188, "vias": 2, "pad_mm": 0.01, "package_mm": 3.693},
}


def make(selection, read=lambda: None, lookup=None):
    return NetPanel(
        read_board=read,
        selected=lambda: list(selection),
        lookup=lookup or (lambda nets: {"nets": [ROW] if nets else []}),
        run=run_inline,
    )


def test_it_never_takes_focus(qapp):
    p = make([])
    flags = p.windowFlags()
    assert flags & Qt.WindowType.WindowDoesNotAcceptFocus
    assert flags & Qt.WindowType.WindowStaysOnTopHint
    assert p.testAttribute(Qt.WidgetAttribute.WA_ShowWithoutActivating)
    p.close()


def test_with_nothing_selected_it_asks_for_a_click_then_answers(qapp):
    selection = []
    p = make(selection)
    p.start()
    assert spin(lambda: "Click a track" in p._body.text())
    selection.append("/ddr4/DDR_A6")
    assert spin(lambda: "too short" in p._body.text())
    text = p._body.text()
    assert "A6" in text and "extend by 9.595 mm" in text
    assert "incl. 2 vias 3.188 mm, package 3.693 mm" in text
    p.close()


def test_it_follows_a_new_selection_and_back_to_waiting(qapp):
    selection = ["/ddr4/DDR_A6"]
    p = make(selection)
    p.start()
    assert spin(lambda: "too short" in p._body.text())
    selection.clear()
    assert spin(lambda: "Click a track" in p._body.text())
    p.close()


def test_a_board_that_cannot_be_read_says_why(qapp):
    def fail():
        raise RuntimeError("Could not reach KiCad")

    p = make(["x"], read=fail)
    p.start()
    assert spin(lambda: "Could not reach KiCad" in p._body.text())
    p.close()


def test_the_footer_links_to_embeddedci(qapp):
    p = make([])
    from PySide6.QtWidgets import QLabel

    assert any("embeddedci.com" in l.text() for l in p.findChildren(QLabel))
    p.close()
