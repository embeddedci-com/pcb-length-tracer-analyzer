from email.parser import BytesParser
from email.policy import HTTP

from trace_length_analyzer.multipart import encode


def parse(body, content_type):
    msg = BytesParser(policy=HTTP).parsebytes(
        b"Content-Type: " + content_type.encode() + b"\r\n\r\n" + body
    )
    return {
        part.get_param("name", header="content-disposition"): (part.get_filename(), part.get_payload(decode=True))
        for part in msg.iter_parts()
    }


def test_parts_round_trip_byte_for_byte():
    board = b"(kicad_pcb (version 20260206)\r\n\x00\xff binary-ish \n)"
    body, ctype = encode([("board", "a.kicad_pcb", board), ("rules", "a.kicad_dru", b"(version 1)")])
    got = parse(body, ctype)
    assert got["board"] == ("a.kicad_pcb", board)
    assert got["rules"] == ("a.kicad_dru", b"(version 1)")


def test_boundary_never_appears_in_the_data():
    body, ctype = encode([("board", "x.kicad_pcb", b"----pcbtla" * 1000)])
    boundary = ctype.split("boundary=")[1].encode()
    assert body.count(b"--" + boundary) == 2  # one opening, one closing


def test_quotes_in_a_filename_cannot_break_the_header():
    body, ctype = encode([("board", 'we"ird\r\nname.kicad_pcb', b"x")])
    got = parse(body, ctype)
    assert got["board"][1] == b"x"
