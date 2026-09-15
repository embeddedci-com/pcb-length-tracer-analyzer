import os
import threading
import time

from trace_length_analyzer import single
from trace_length_analyzer.single import SingleInstance


def test_first_press_takes_the_lock_and_a_second_does_not(tmp_path):
    a, b = SingleInstance(tmp_path), SingleInstance(tmp_path)
    assert a.acquire()
    assert not b.acquire()
    a.release()
    assert b.acquire()
    b.release()


def test_second_press_hands_over_to_the_running_window(tmp_path):
    window = SingleInstance(tmp_path)
    assert window.acquire()
    seen = []

    def run_window():
        for _ in range(40):
            window.heartbeat()
            if window.take_request():
                seen.append(True)
                return
            time.sleep(0.05)

    t = threading.Thread(target=run_window)
    t.start()
    assert SingleInstance(tmp_path).ask_to_show(timeout=3)
    t.join()
    assert seen == [True]
    assert not window.take_request()  # once only
    window.release()


def test_a_window_that_stopped_heartbeating_is_taken_over(tmp_path, monkeypatch):
    dead = SingleInstance(tmp_path)
    assert dead.acquire()
    old = time.time() - single.STALE_S - 5
    os.utime(dead.lock, (old, old))
    later = SingleInstance(tmp_path)
    assert later.acquire()
    # The old window wakes up: it must notice the lock is not its own, and
    # closing it must not delete the new window's lock.
    dead.heartbeat()
    assert not dead.owned
    dead.release()
    assert later.lock.exists()
    assert not SingleInstance(tmp_path).acquire()
    later.release()
    assert not later.lock.exists()


def test_an_unanswered_request_does_not_linger(tmp_path):
    hung = SingleInstance(tmp_path)
    assert hung.acquire()
    assert not SingleInstance(tmp_path).ask_to_show(timeout=0.3)
    assert not hung.request.exists()
    hung.release()


def test_a_request_left_before_the_window_opened_is_ignored(tmp_path):
    (tmp_path / "show.request").write_text("old")
    w = SingleInstance(tmp_path)
    assert w.acquire()
    assert not w.take_request()
    w.release()
