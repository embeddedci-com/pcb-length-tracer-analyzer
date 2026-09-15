"""multipart/form-data, for handing the engine a board the way the site's form does.

The engine's upload endpoint is the site's, so the board goes in as a form. A
dozen lines here rather than a dependency: the standard library has a parser
for this format but no encoder.
"""

from __future__ import annotations

import secrets
from typing import Iterable, Tuple


def encode(files: Iterable[Tuple[str, str, bytes]]) -> Tuple[bytes, str]:
    """Encode ``(field, filename, data)`` parts. Returns the body and its Content-Type."""
    parts = list(files)
    while True:
        boundary = "----pcbtla" + secrets.token_hex(16)
        marker = boundary.encode("ascii")
        # A boundary that happens to occur inside a board file would cut the
        # file short. With 128 random bits it never will, but it costs nothing
        # to be sure.
        if not any(marker in data for _, _, data in parts):
            break
    out = bytearray()
    for field, filename, data in parts:
        safe = filename.replace('"', "_").replace("\r", "_").replace("\n", "_")
        out += b"--" + marker + b"\r\n"
        out += f'Content-Disposition: form-data; name="{field}"; filename="{safe}"\r\n'.encode("utf-8")
        out += b"Content-Type: application/octet-stream\r\n\r\n"
        out += data
        out += b"\r\n"
    out += b"--" + marker + b"--\r\n"
    return bytes(out), f"multipart/form-data; boundary={boundary}"
