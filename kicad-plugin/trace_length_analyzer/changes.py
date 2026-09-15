"""Deciding whether an engine result may be applied to the board open in KiCad.

The engine tunes its own copy of the board and describes the result as edits:
tracks to remove, by uuid and with the geometry they had when analysed, and
tracks to add. Between the analysis and the click on Apply the user may have
kept working. An edit written against a board that has since changed could
join a meander to a track that is no longer there, or delete one the user has
just moved, so the rule is strict: every track to be removed must still be on
the board, unchanged, or nothing is applied.

Kept free of KiCad so it can be tested without it. ``boardio`` turns pcbnew's
items into the plain dictionaries this compares.
"""

from __future__ import annotations

from typing import Any, Dict, Iterable, List, Mapping

# The fields that make two items the same piece of copper. uuid is the key, not
# a field; everything else that the engine sends is compared.
GEOMETRY = (
    "kind",
    "net",
    "layer",
    "start_nm",
    "end_nm",
    "mid_nm",
    "width_nm",
    "size_nm",
    "drill_nm",
)


def _norm(item: Mapping[str, Any]) -> Dict[str, Any]:
    out: Dict[str, Any] = {}
    for k in GEOMETRY:
        v = item.get(k)
        if isinstance(v, (list, tuple)):
            v = tuple(int(x) for x in v)
        elif k.endswith("_nm") and v is not None:
            v = int(v)
        if v in (None, "", 0) and k in ("layer", "mid_nm", "width_nm", "size_nm", "drill_nm"):
            v = None
        out[k] = v
    # A via's layer is its span, which the editor reports differently from the
    # file; its position, size and drill are what identify it.
    if out["kind"] == "via":
        out["layer"] = None
    return out


def describe(item: Mapping[str, Any]) -> str:
    kind = item.get("kind", "item")
    net = item.get("net") or "no net"
    layer = item.get("layer")
    return f"{kind} on {net}" + (f" ({layer})" if layer else "")


def stale(remove: Iterable[Mapping[str, Any]], current: Mapping[str, Mapping[str, Any]]) -> List[str]:
    """Reasons the edits no longer fit the board. Empty means they do.

    ``remove`` is the engine's list; ``current`` maps uuid to the item as it is
    on the board now, for as many of those uuids as are still there.
    """
    problems: List[str] = []
    for item in remove:
        uuid = item.get("uuid", "")
        now = current.get(uuid)
        if now is None:
            problems.append(f"a {describe(item)} has been deleted")
            continue
        a, b = _norm(item), _norm(now)
        diff = [k for k in GEOMETRY if a[k] != b[k]]
        if diff:
            problems.append(f"a {describe(item)} has changed ({', '.join(diff)})")
    return problems


def summarize_problems(problems: List[str], limit: int = 5) -> str:
    head = "; ".join(problems[:limit])
    more = f"; and {len(problems) - limit} more" if len(problems) > limit else ""
    n = len(problems)
    return (
        f"The board has changed since it was analysed, so nothing was applied: {head}{more}. "
        f"Rescan to analyse the board as it is now ({n} item{'s' if n != 1 else ''} differ)."
    )
