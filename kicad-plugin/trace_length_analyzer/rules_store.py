"""The rules a board is judged by, kept between runs of the plugin.

Tolerances, the clock offset, package lengths, drawn areas and what an
interface was said to be are all set in the report, and they belong to the
board: closing the window or restarting KiCad must not put them back to the
defaults, and the Selected Net Length panel must judge a net by the same rules
the report shows.

One JSON file per board, in the settings folder KiCad gives the plugin, keyed
by the board file's path. The development and released copies of the plugin
share it: the rules are the board's, not the build's.
"""

from __future__ import annotations

import hashlib
import json
import os
import time
from pathlib import Path
from typing import Any, Dict, Optional

RELEASE_ID = "com.embeddedci.pcb-trace-length-analyzer"


def shared_identifier(identifier: str) -> str:
    """The identifier settings are kept under: a ".dev" copy shares the release's."""
    return identifier[: -len(".dev")] if identifier.endswith(".dev") else identifier


def fallback_dir(identifier: str = RELEASE_ID) -> Path:
    """Where to keep settings when KiCad cannot say (an older KiCad, or no connection)."""
    base = os.environ.get("XDG_CONFIG_HOME")
    if base:
        return Path(base) / identifier
    if os.name == "nt":
        return Path(os.environ.get("APPDATA", Path.home())) / identifier
    return Path.home() / ".config" / identifier


class RulesStore:
    def __init__(self, directory: Path):
        self.dir = Path(directory) / "boards"

    def _file(self, board: str) -> Path:
        key = hashlib.sha1(board.encode("utf-8")).hexdigest()[:20]
        return self.dir / f"{key}.json"

    def load(self, board: str) -> Optional[Dict[str, Any]]:
        """The rules saved for this board, or None."""
        try:
            data = json.loads(self._file(board).read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return None
        if data.get("board") != board or not isinstance(data.get("params"), dict):
            return None
        return data["params"]

    def save(self, board: str, params: Dict[str, Any]) -> None:
        """Keep the rules for this board. Written atomically: a crash mid-write keeps the old file."""
        self.dir.mkdir(parents=True, exist_ok=True)
        path = self._file(board)
        tmp = path.with_suffix(f".{os.getpid()}.tmp")
        tmp.write_text(
            json.dumps({"board": board, "saved_at": int(time.time()), "params": params}, indent=2),
            encoding="utf-8",
        )
        os.replace(tmp, path)

    def forget(self, board: str) -> None:
        try:
            self._file(board).unlink()
        except FileNotFoundError:
            pass
