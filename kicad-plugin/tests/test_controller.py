"""The plugin's flows, with pcbnew stood in for."""

import pytest

from trace_length_analyzer import boardio
from trace_length_analyzer.controller import Controller, format_lookup, format_row


@pytest.fixture
def controller(engine, demo_files, monkeypatch):
    files = boardio.BoardFiles(
        text=demo_files["text"],
        filename=demo_files["filename"],
        project=demo_files["project"],
        rules=demo_files["rules"],
        project_dir=None,
        source="the editor",
    )
    monkeypatch.setattr(boardio, "read_board", lambda board: files)
    return Controller(engine, kicad=object(), board=object())


def test_rescan_carries_the_users_parameters_to_the_new_session(controller):
    first = controller.rescan()
    names = controller.group_names()
    assert names
    params = controller.current_params()
    params["group_tolerance_mm"] = {names[0]: 0.123}
    params["clock_offset_percent"] = 2.5
    controller.engine.plan(first, params)

    second = controller.rescan()
    assert second != first
    kept = controller.current_params()
    assert kept["group_tolerance_mm"] == {names[0]: 0.123}
    assert kept["clock_offset_percent"] == 2.5
    # The old session is gone: one board, one session.
    r = controller.engine.request("GET", f"/api/pcb-trace-length-analyzer/sessions/{first}")
    assert r.status == 404


def test_one_tolerance_everywhere_and_back(controller):
    controller.rescan()
    before = len(controller.attention_nets())
    controller.set_tolerance_everywhere(50.0)
    loose = controller.analysis()
    assert all(g["tolerance_mm"] == 50.0 for g in loose["groups"])
    # Nothing can be more than 50 mm out on this board except nets too long or unrouted legs.
    assert len(controller.attention_nets()) < before
    controller.set_tolerance_everywhere(None)
    assert len(controller.attention_nets()) == before


def test_lookup_formats_every_kind_of_answer(controller):
    controller.rescan()
    attention = controller.engine.nets(controller.session, attention=True)["nets"]
    net = attention[0]["net"]
    lines = format_lookup(controller.lookup([net, "GND", "NO_SUCH_NET"]))
    assert any(("extend by" in l or "shorten by" in l) for l in lines)
    assert any(l.startswith("GND:") and "not in any length-matched group" in l for l in lines)
    assert any("no such net" in l for l in lines)


def test_format_row_states_the_action():
    row = {
        "label": "DQ23", "interface": "DDR", "group": "byte lane 2", "routed": True,
        "length_mm": 54.2, "target_mm": 55.693, "tolerance_mm": 0.635,
        "deviation_mm": -1.493, "in_tolerance": False, "need_mm": 1.493,
    }
    assert format_row(row).endswith("extend by 1.493 mm")
    assert "54.200 mm (too short)" in format_row(row)
    assert "within tolerance" in format_row({**row, "in_tolerance": True, "need_mm": 0})
    too_long = format_row({**row, "need_mm": 0, "excess_mm": 0.4})
    assert "shorten by 0.400 mm" in too_long and "(too long)" in too_long
    assert "too " not in format_row({**row, "in_tolerance": True, "need_mm": 0})
    assert "reference" in format_row({**row, "reference": True})
    assert format_row({**row, "routed": False}).endswith("not routed")
    legged = {**row, "group": "address/command U3->U4", "leg": "U3->U4"}
    assert "U3->U4 U3->U4" not in format_row(legged)
    assert "(DDR / byte lane 2 U4->U5)" in format_row({**row, "leg": "U4->U5"})
    withparts = {**row, "parts": {"track_mm": 47.0, "via_mm": 3.188, "vias": 2, "pad_mm": 0.01, "package_mm": 4.07}}
    assert "54.200 mm (incl. 2 vias 3.188 mm, package 4.070 mm) (too short)" in format_row(withparts)
    off = {**row, "parts": {"track_mm": 54.2, "via_mm": 0, "vias": 2, "pad_mm": 0, "package_mm": 0}}
    assert "incl." not in format_row(off)


def test_apply_is_switched_off(controller):
    from trace_length_analyzer import controller as ctlmod

    assert ctlmod.APPLY_ENABLED is False
    controller.rescan()
    with pytest.raises(ctlmod.ApplyDisabled):
        controller.apply(controller.session)
