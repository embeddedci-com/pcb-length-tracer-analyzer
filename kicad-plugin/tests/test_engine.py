"""The engine over its pipes, against the real Go binary and the demo board."""

import threading
import time

import pytest

from trace_length_analyzer.engine import API, APIError, Engine, EngineError


def upload(engine, demo_files):
    return engine.upload(demo_files["text"], demo_files["filename"], demo_files["project"], demo_files["rules"])


def test_health(engine):
    r = engine.request("GET", "/api/health")
    assert r.status == 200 and r.json()["status"] == "ok"


def test_upload_reads_the_board_with_its_rules(engine, demo_files):
    res = upload(engine, demo_files)
    a = res["analysis"]
    assert res["session"]["filename"] == "ai-vision.kicad_pcb"
    assert a["board"]["has_project_file"] is True
    assert a["board"]["has_custom_dru"] is True
    assert len(a["groups"]) > 0


def test_error_status_and_message_come_through(engine):
    with pytest.raises(APIError) as e:
        engine.get_json(f"{API}/sessions/nope")
    assert e.value.status == 404
    assert "not found" in str(e.value)


def test_net_lookup_and_attention(engine, demo_files):
    sid = upload(engine, demo_files)["session"]["id"]
    attention = engine.nets(sid, attention=True)["nets"]
    assert attention and all(not r["in_tolerance"] for r in attention)
    one = attention[0]["net"]
    rows = engine.nets(sid, names=[one])["nets"]
    assert rows and rows[0]["net"] == one
    # A hierarchical name full of slashes survives the query string.
    assert "/" in one


def test_apply_then_changes(engine, demo_files):
    res = upload(engine, demo_files)
    sid = res["session"]["id"]
    nets = [c["net"] for c in res["analysis"]["candidates"] if c["need_mm"] > 0 and not c["needs_reroute"]][:3]
    out = engine.post_json(f"{API}/sessions/{sid}/apply", {"nets": nets})
    assert out["changed"]
    ch = engine.changes(sid)
    assert ch["remove"] and ch["add"] and ch["message"]
    assert all(r["uuid"] for r in ch["remove"])
    assert not any(a.get("uuid") for a in ch["add"])


def test_concurrent_requests_from_many_threads(engine, demo_files):
    sid = upload(engine, demo_files)["session"]["id"]
    errors = []

    def worker(i):
        try:
            path = f"{API}/sessions/{sid}/preview/geometry.bin" if i % 2 else f"{API}/sessions/{sid}"
            r = engine.request("GET", path)
            assert r.status == 200 and len(r.body) > 1000
        except BaseException as e:  # noqa: BLE001
            errors.append(e)

    threads = [threading.Thread(target=worker, args=(i,)) for i in range(12)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    assert not errors, errors


def test_binary_body_arrives_intact(engine, demo_files):
    sid = upload(engine, demo_files)["session"]["id"]
    doc = engine.get_json(f"{API}/sessions/{sid}/preview/board.json")
    geom = engine.request("GET", f"{API}/sessions/{sid}/preview/geometry.bin")
    assert len(geom.body) == doc["geometry"]["byte_length"]


def test_killed_engine_fails_waiting_requests_instead_of_hanging(engine_path, demo_files):
    e = Engine(engine_path)
    fut = e.request_async("POST", f"{API}/sessions", demo_files["text"], {"Content-Type": "text/plain"})
    e._proc.kill()
    with pytest.raises((EngineError, Exception)):
        # Either the answer came back (a 400 for the bad content type) before
        # the kill, or the waiter is told the engine died -- never a hang.
        r = fut.result(timeout=10)
        r.raise_for_status()
    with pytest.raises(EngineError):
        e.request("GET", "/api/health", timeout=10)
    e.close()


def test_close_ends_the_process(engine_path):
    e = Engine(engine_path)
    e.close()
    deadline = time.time() + 5
    while e._proc.poll() is None and time.time() < deadline:
        time.sleep(0.05)
    assert e._proc.poll() == 0


def test_missing_engine_is_a_clear_error(tmp_path, monkeypatch):
    from trace_length_analyzer.engine import find_engine

    monkeypatch.setenv("PCB_TLA_ENGINE", str(tmp_path / "nothing"))
    with pytest.raises(EngineError, match="not a file"):
        find_engine(tmp_path)
