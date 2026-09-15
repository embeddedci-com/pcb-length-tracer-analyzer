"""The report in a real QtWebEngine, served by the scheme handler.

This is the part the design called genuinely uncertain: that the web front end,
built for a browser tab talking HTTP, runs from a custom scheme answered inside
this process -- module scripts, fetch with bodies, binary geometry, and error
statuses the page has to see. It runs headless (QT_QPA_PLATFORM=offscreen) with
the real engine and the demo board; only pcbnew is stood in for.
"""

import base64
import json
import os
import time
from concurrent.futures import Future

import pytest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")
QtWebEngineCore = pytest.importorskip("PySide6.QtWebEngineCore")

from PySide6.QtCore import QEventLoop, QTimer, QUrl  # noqa: E402
from PySide6.QtWidgets import QApplication  # noqa: E402

from trace_length_analyzer.app import WEB_ROOT  # noqa: E402
from trace_length_analyzer.scheme import ORIGIN, SCHEME, SchemeHandler, register_scheme  # noqa: E402

_app = None


@pytest.fixture(scope="module")
def qapp():
    global _app
    if not (WEB_ROOT / "kicad.html").is_file():
        pytest.skip("web bundle not built (npm run build:kicad)")
    if _app is None:
        register_scheme()
        _app = QApplication.instance() or QApplication(["test"])
    return _app


def wait(pred, timeout=30.0):
    deadline = time.time() + timeout
    loop = QEventLoop()
    while time.time() < deadline:
        if pred():
            return True
        QTimer.singleShot(50, loop.quit)
        loop.exec()
    return pred()


def run_js(page, script, timeout=30.0):
    box = {}
    page.runJavaScript(script, 0, lambda v: box.setdefault("v", v))
    assert wait(lambda: "v" in box, timeout), f"script did not finish: {script[:80]}"
    return box["v"]


def fetch_js(page, path, method="GET", body=None):
    """Start a fetch in the page and poll for its result (runJavaScript does not await)."""
    init = {"method": method}
    if body is not None:
        # What webapp/kicad/fetch.ts does: the body travels base64 in X-Body,
        # because the scheme handler cannot read a request body.
        init["headers"] = {
            "Content-Type": "application/json",
            "X-Body": base64.b64encode(json.dumps(body).encode("utf-8")).decode("ascii"),
        }
    run_js(
        page,
        f"""window.__r = null;
        fetch({json.dumps(path)}, {json.dumps(init)})
          .then(async r => {{ window.__r = JSON.stringify({{status: r.status,
              xstatus: r.headers.get('X-Status'), text: (await r.text()).slice(0, 400)}}) }})
          .catch(e => {{ window.__r = JSON.stringify({{error: String(e)}}) }});
        true""",
    )
    assert wait(lambda: run_js(page, "window.__r") is not None)
    return json.loads(run_js(page, "window.__r"))


@pytest.fixture
def page(qapp, engine, demo_files):
    from PySide6.QtWebEngineCore import QWebEnginePage, QWebEngineProfile

    calls = []

    def select(payload):
        calls.append(payload)
        f = Future()
        f.set_result({"selected": len(payload.get("nets", []))})
        return f

    def refuse(payload):
        f = Future()
        f.set_exception(RuntimeError("The board has changed since it was analysed"))
        return f

    profile = QWebEngineProfile()
    handler = SchemeHandler(WEB_ROOT, engine.request_async, {"select": select, "apply": refuse})
    profile.installUrlSchemeHandler(SCHEME, handler)
    p = QWebEnginePage(profile)
    res = engine.upload(demo_files["text"], demo_files["filename"], demo_files["project"], demo_files["rules"])
    p.calls = calls
    p.session = res["session"]["id"]
    p._keep = (profile, handler)
    yield p
    p.deleteLater()
    QApplication.processEvents()


def test_the_report_renders_from_the_scheme(page):
    loaded = {}
    page.loadFinished.connect(lambda ok: loaded.setdefault("ok", ok))
    page.load(QUrl(f"{ORIGIN}/index.html?session={page.session}"))
    assert wait(lambda: "ok" in loaded) and loaded["ok"], "page did not load"
    # The board's name is the page title once the session has been fetched
    # through the scheme and the engine.
    assert wait(
        lambda: "ai-vision.kicad_pcb" in (run_js(page, "document.body.innerText") or ""), 60
    ), (run_js(page, "document.body.innerText") or "")[:500]
    text = run_js(page, "document.body.innerText")
    # The KiCad-only controls are there, because the host is.
    assert "Rescan" in text
    assert "Select what needs attention" in wait_text(page, "Select what needs attention")


def wait_text(page, needle, timeout=30):
    wait(lambda: needle in (run_js(page, "document.body.innerText") or ""), timeout)
    return run_js(page, "document.body.innerText") or ""


def test_status_codes_survive_the_scheme(page):
    page.load(QUrl(f"{ORIGIN}/index.html"))
    wait(lambda: run_js(page, "document.readyState") == "complete")
    missing = fetch_js(page, "/api/pcb-trace-length-analyzer/sessions/nope")
    assert missing.get("xstatus") == "404", missing
    assert "not found" in missing["text"]
    ok = fetch_js(page, f"/api/pcb-trace-length-analyzer/sessions/{page.session}/nets?attention=1")
    assert ok["xstatus"] == "200" and '"nets"' in ok["text"]


def test_post_bodies_reach_the_plugin(page):
    page.load(QUrl(f"{ORIGIN}/index.html"))
    wait(lambda: run_js(page, "document.readyState") == "complete")
    r = fetch_js(page, "/kicad/select", "POST", {"nets": ["/ddr4/DDR_DQ0", "/ddr4/DDR_DQ1"]})
    assert r["xstatus"] == "200", r
    assert page.calls == [{"nets": ["/ddr4/DDR_DQ0", "/ddr4/DDR_DQ1"]}]
    refused = fetch_js(page, "/kicad/apply", "POST", {"session": "x"})
    assert refused["xstatus"] == "409" and "changed since" in refused["text"]


def test_post_bodies_reach_the_engine(page):
    """plan is a POST with JSON params: what the report sends when a tolerance bar moves."""
    page.load(QUrl(f"{ORIGIN}/index.html"))
    wait(lambda: run_js(page, "document.readyState") == "complete")
    r = fetch_js(
        page,
        f"/api/pcb-trace-length-analyzer/sessions/{page.session}/plan",
        "POST",
        {"params": {"clock_offset_percent": 3.5}},
    )
    assert r["xstatus"] == "200", r
    assert '"clock_offset_percent":3.5' in r["text"].replace(" ", "") or "3.5" in r["text"]


def test_bundle_cannot_be_escaped(page):
    page.load(QUrl(f"{ORIGIN}/index.html"))
    wait(lambda: run_js(page, "document.readyState") == "complete")
    r = fetch_js(page, "/../../plugin.json")
    assert r.get("xstatus") in ("403", "404") or "identifier" not in r.get("text", "")
