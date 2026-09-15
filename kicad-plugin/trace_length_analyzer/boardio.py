"""Everything the plugin does to the board open in pcbnew, through KiCad's IPC API.

Reading the board, pointing at nets on it, finding out which net the user has
clicked, and applying an engine result as one undoable commit.
"""

from __future__ import annotations

import os
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence

from . import changes as changelib


class KiCadUnavailable(RuntimeError):
    """KiCad is not running, its API server is off, or no board is open."""


API_HELP = (
    "Could not reach KiCad. Open the board in the PCB Editor, and make sure the API server is "
    "on: Preferences → Plugins → Enable KiCad API. (KiCad starts plugins with the connection "
    "details already set; running this script by hand needs KiCad open as well.)"
)


def connect():
    """The running KiCad and the board open in it."""
    try:
        from kipy import KiCad
    except ImportError as e:  # pragma: no cover - only without the dependency
        raise KiCadUnavailable("kicad-python is not installed in the plugin's environment") from e
    try:
        kicad = KiCad(client_name="pcb-trace-length-analyzer")
        kicad.ping()
        board = kicad.get_board()
    except Exception as e:  # noqa: BLE001 -- kipy raises several unrelated types here
        raise KiCadUnavailable(f"{API_HELP}\n\n({e})") from e
    return kicad, board


@dataclass
class BoardFiles:
    text: bytes
    filename: str
    project: Optional[bytes]
    rules: Optional[bytes]
    project_dir: Optional[Path]
    # How the board text was obtained, for the status line.
    source: str


def read_board(board) -> BoardFiles:
    """The board as it is in the editor, unsaved edits included, plus its rules.

    The board comes from pcbnew itself rather than from disk, so what is
    analysed is what the user is looking at, and their file is never saved on
    their behalf. The project and custom rules files are read from disk: the
    net classes and design rules live there, not in the board.
    """
    filename = Path(board.name).name or "board.kicad_pcb"
    text: Optional[bytes] = None
    source = "the editor"
    try:
        text = board.get_as_string().encode("utf-8")
    except Exception:  # noqa: BLE001 -- an older KiCad without SaveDocumentToString
        text = None
    if not text:
        # Save a copy -- not the user's file -- and read that.
        with tempfile.TemporaryDirectory(prefix="pcb-tla-") as tmp:
            dest = os.path.join(tmp, filename)
            board.save_as(dest, overwrite=True, include_project=False)
            text = Path(dest).read_bytes()
        source = "a saved copy"

    project_dir: Optional[Path] = None
    try:
        p = board.get_project().path
        if p:
            project_dir = Path(p)
    except Exception:  # noqa: BLE001
        project_dir = None

    stem = filename[: -len(".kicad_pcb")] if filename.endswith(".kicad_pcb") else filename

    def sibling(ext: str) -> Optional[bytes]:
        if project_dir is None:
            return None
        f = project_dir / (stem + ext)
        return f.read_bytes() if f.is_file() else None

    return BoardFiles(
        text=text,
        filename=filename,
        project=sibling(".kicad_pro"),
        rules=sibling(".kicad_dru"),
        project_dir=project_dir,
        source=source,
    )


# ---- nets ----

def _copper_types():
    from kipy.proto.common.types import KiCadObjectType

    return [KiCadObjectType.KOT_PCB_TRACE, KiCadObjectType.KOT_PCB_ARC, KiCadObjectType.KOT_PCB_VIA]


def copper_of_nets(board, nets: Sequence[str]) -> List[Any]:
    """Tracks, arcs and vias on these nets."""
    from kipy.board_types import Net

    wanted = set(nets)
    if not wanted:
        return []
    try:
        return list(board.get_items_by_net([Net(name=n) for n in wanted], _copper_types()))
    except Exception:  # noqa: BLE001 -- before KiCad 10.0.1; filter everything instead
        items = list(board.get_tracks()) + list(board.get_vias())
        return [i for i in items if i.net.name in wanted]


def select_nets(kicad, board, nets: Sequence[str]) -> int:
    """Select these nets' copper and bring it into view. Returns how many items."""
    items = copper_of_nets(board, nets)
    board.clear_selection()
    if items:
        board.add_to_selection(items)
        zoom_to_selection(kicad)
    return len(items)


def zoom_to_selection(kicad) -> None:
    # KiCad does not promise action names are stable, so a missing one is not
    # an error: the selection is made either way, and only the framing is lost.
    # run_action reports an unknown action as a status, not an exception.
    # KiCad 10.0.5 answers RAS_OK for the first and RAS_INVALID for the second.
    from kipy.proto.common.commands.editor_commands_pb2 import RunActionStatus

    for action in ("common.Control.zoomFitSelection", "pcbnew.Control.zoomFitSelection"):
        try:
            if kicad.run_action(action).status == RunActionStatus.RAS_OK:
                return
        except Exception:  # noqa: BLE001
            continue


def selected_nets(board) -> List[str]:
    """The nets of whatever copper or pads are selected, in selection order."""
    seen: Dict[str, None] = {}
    try:
        items = board.get_selection()
    except Exception:  # noqa: BLE001
        return []
    for item in items:
        net = getattr(item, "net", None)
        name = getattr(net, "name", "") if net is not None else ""
        if name:
            seen.setdefault(name, None)
    return list(seen)


# ---- apply ----

def item_geometry(item) -> Dict[str, Any]:
    """A pcbnew track, arc or via in the shape the engine describes copper."""
    from kipy.board_types import ArcTrack, Track, Via
    from kipy.util.board_layer import canonical_name

    def xy(v) -> List[int]:
        return [int(v.x), int(v.y)]

    if isinstance(item, Via):
        return {
            "kind": "via",
            "net": item.net.name,
            "start_nm": xy(item.position),
            "end_nm": xy(item.position),
            "size_nm": int(item.diameter),
            "drill_nm": int(item.drill_diameter),
        }
    kind = "arc" if isinstance(item, ArcTrack) else "segment" if isinstance(item, Track) else None
    if kind is None:
        return {"kind": type(item).__name__}
    g = {
        "kind": kind,
        "net": item.net.name,
        "layer": canonical_name(item.layer),
        "start_nm": xy(item.start),
        "end_nm": xy(item.end),
        "width_nm": int(item.width),
    }
    if kind == "arc":
        g["mid_nm"] = xy(item.mid)
    return g


def build_item(spec: Dict[str, Any]):
    """A new pcbnew item from the engine's description of one."""
    from kipy.board_types import ArcTrack, Net, Track, Via
    from kipy.geometry import Vector2
    from kipy.proto.board.board_types_pb2 import BoardLayer, ViaType
    from kipy.util.board_layer import layer_from_canonical_name

    def v(p) -> Vector2:
        return Vector2.from_xy(int(p[0]), int(p[1]))

    def layer(name: str):
        l = layer_from_canonical_name(name or "")
        if l == BoardLayer.BL_UNKNOWN:
            raise ValueError(f"unknown layer {name!r}")
        return l

    kind = spec.get("kind")
    if kind == "via":
        via = Via()
        via.position = v(spec["start_nm"])
        via.net = Net(name=spec["net"])
        top, bottom = spec.get("layer_top") or "F.Cu", spec.get("layer_bottom") or "B.Cu"
        if (top, bottom) != ("F.Cu", "B.Cu"):
            via.type = ViaType.VT_BLIND_BURIED
            via.padstack.drill.start_layer = layer(top)
            via.padstack.drill.end_layer = layer(bottom)
        via.diameter = int(spec["size_nm"])
        via.drill_diameter = int(spec["drill_nm"])
        return via
    if kind == "arc":
        arc = ArcTrack()
        arc.start, arc.mid, arc.end = v(spec["start_nm"]), v(spec["mid_nm"]), v(spec["end_nm"])
        arc.width = int(spec["width_nm"])
        arc.layer = layer(spec["layer"])
        arc.net = Net(name=spec["net"])
        return arc
    if kind == "segment":
        t = Track()
        t.start, t.end = v(spec["start_nm"]), v(spec["end_nm"])
        t.width = int(spec["width_nm"])
        t.layer = layer(spec["layer"])
        t.net = Net(name=spec["net"])
        return t
    raise ValueError(f"cannot create a {kind!r}")


class StaleBoard(RuntimeError):
    """The board has changed since the analysis the edits were made against."""


def current_items(board, uuids: Sequence[str]) -> Dict[str, Dict[str, Any]]:
    """The items with these uuids as they are on the board now."""
    from kipy.proto.common.types import KIID

    if not uuids:
        return {}
    ids = []
    for u in uuids:
        k = KIID()
        k.value = u
        ids.append(k)
    try:
        items = board.get_items_by_id(ids)
    except Exception:  # noqa: BLE001 -- before KiCad 10: scan every track and via
        wanted = set(uuids)
        items = [i for i in list(board.get_tracks()) + list(board.get_vias()) if i.id.value in wanted]
    return {i.id.value: item_geometry(i) for i in items}


def apply_changes(board, change: Dict[str, Any]) -> Dict[str, Any]:
    """Make the engine's edits as one commit: one Ctrl+Z undoes all of it.

    Refuses, having changed nothing, when any track to be removed has been
    deleted or altered since the analysis.
    """
    remove = change.get("remove") or []
    add = change.get("add") or []
    if not remove and not add:
        raise ValueError("there is nothing to apply")

    problems = changelib.stale(remove, current_items(board, [r["uuid"] for r in remove]))
    if problems:
        raise StaleBoard(changelib.summarize_problems(problems))

    # Build everything before touching the board, so a bad item fails here and
    # not half-way through a commit.
    new_items = [build_item(a) for a in add]
    message = change.get("message") or "Trace length matching"

    from kipy.proto.common.types import KIID

    ids = []
    for r in remove:
        k = KIID()
        k.value = r["uuid"]
        ids.append(k)

    commit = board.begin_commit()
    try:
        if ids:
            board.remove_items_by_id(ids)
        created = board.create_items(new_items) if new_items else []
    except BaseException:
        board.drop_commit(commit)
        raise
    board.push_commit(commit, message)
    return {"removed": len(ids), "added": len(created), "message": message}
