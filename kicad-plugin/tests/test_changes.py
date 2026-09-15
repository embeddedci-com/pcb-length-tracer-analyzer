from trace_length_analyzer.changes import stale, summarize_problems

SEG = {
    "uuid": "u1",
    "kind": "segment",
    "net": "/ddr4/DDR_DQ0",
    "layer": "F.Cu",
    "start_nm": [1000, 2000],
    "end_nm": [5000, 2000],
    "width_nm": 90000,
}
VIA = {
    "uuid": "v1",
    "kind": "via",
    "net": "/ddr4/DDR_DQ0",
    "start_nm": [5000, 2000],
    "end_nm": [5000, 2000],
    "size_nm": 400000,
    "drill_nm": 200000,
    "layer_top": "F.Cu",
    "layer_bottom": "B.Cu",
}


def board_copy(item, **over):
    g = {k: v for k, v in item.items() if k not in ("uuid", "layer_top", "layer_bottom")}
    g.update(over)
    return g


def test_unchanged_board_is_not_stale():
    current = {"u1": board_copy(SEG), "v1": board_copy(VIA)}
    assert stale([SEG, VIA], current) == []


def test_tuples_and_lists_compare_equal():
    current = {"u1": board_copy(SEG, start_nm=(1000, 2000), end_nm=(5000, 2000))}
    assert stale([SEG], current) == []


def test_deleted_track_is_stale():
    problems = stale([SEG], {})
    assert len(problems) == 1 and "deleted" in problems[0]


def test_moved_track_is_stale_even_with_the_same_uuid():
    problems = stale([SEG], {"u1": board_copy(SEG, end_nm=[5001, 2000])})
    assert len(problems) == 1 and "end_nm" in problems[0]


def test_rewidthed_or_relayered_track_is_stale():
    assert "width_nm" in stale([SEG], {"u1": board_copy(SEG, width_nm=100000)})[0]
    assert "layer" in stale([SEG], {"u1": board_copy(SEG, layer="B.Cu")})[0]


def test_track_moved_to_another_net_is_stale():
    assert "net" in stale([SEG], {"u1": board_copy(SEG, net="GND")})[0]


def test_via_is_judged_on_position_size_and_drill_not_its_layer_label():
    current = {"v1": board_copy(VIA, layer="F.Cu")}
    assert stale([VIA], current) == []
    assert stale([VIA], {"v1": board_copy(VIA, drill_nm=300000)})


def test_summary_counts_everything_but_lists_a_few():
    msg = summarize_problems([f"p{i}" for i in range(9)], limit=3)
    assert "p0; p1; p2" in msg and "6 more" in msg and "9 items" in msg and "Rescan" in msg
