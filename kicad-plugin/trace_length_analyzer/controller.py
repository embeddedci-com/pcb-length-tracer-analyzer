"""The plugin's state: one engine, one KiCad connection, the current session.

Qt-free, so the flows -- read the board into a session, keep the user's
parameters across a rescan, look a net up, apply a result -- can be driven
from a test or a script with a stand-in for pcbnew.

KiCad is talked to from one worker thread only. The IPC client is not
documented as safe to share across threads, and pcbnew answers API calls on its
own UI thread anyway, so there is nothing to gain from asking it two things at
once.
"""

from __future__ import annotations

import threading
from concurrent.futures import Future, ThreadPoolExecutor
from typing import Any, Callable, Dict, List, Optional

from . import boardio
from .engine import Engine
from .rules_store import RELEASE_ID, RulesStore, fallback_dir, shared_identifier

# Writing a result into the open board. Off until it has been tested against a
# live pcbnew: the edits are built, checked and unit-tested, but nobody has yet
# pressed Apply on a real board and confirmed KiCad measures what the engine
# reported. While it is off the window does not offer the call at all.
APPLY_ENABLED = False


class ApplyDisabled(RuntimeError):
    """Applying to the board is switched off in this version."""


def _includes(parts: Optional[Dict[str, Any]]) -> str:
    """What a length counts besides track, when it does: " incl. 2 vias, package"."""
    if not parts:
        return ""
    extra = []
    vias = int(parts.get("vias") or 0)
    if vias and (parts.get("via_mm") or 0) > 0:
        extra.append(f"{vias} via{'s' if vias != 1 else ''} {parts['via_mm']:.3f} mm")
    if (parts.get("package_mm") or 0) > 0:
        extra.append(f"package {parts['package_mm']:.3f} mm")
    return f" (incl. {', '.join(extra)})" if extra else ""


def _leaf(net: str) -> str:
    return net.rsplit("/", 1)[-1] or net


def format_row(row: Dict[str, Any]) -> str:
    """One net's standing in a sentence: what it is, and what to do about it."""
    # DDR groups already carry their leg in the name ("address/command U3->U4").
    leg = row.get("leg") or ""
    where = row["group"] + (f" {leg}" if leg and leg not in row["group"] else "")
    head = f"{row['label']} ({row['interface']} / {where})"
    # The net clicked continues this signal through a series part.
    if row.get("asked"):
        parts = ", ".join(row.get("through") or [])
        head = f"{_leaf(row['asked'])} is {row['label']}{f' past {parts}' if parts else ''} ({row['interface']} / {where})"
    if not row.get("routed"):
        return f"{head}: not routed"
    length = f"{row['length_mm']:.3f} mm{_includes(row.get('parts'))}"
    target = f"target {row['target_mm']:.3f} ±{row['tolerance_mm']:.3f}"
    if row.get("reference"):
        return f"{head}: {length}, the reference this group is matched to"
    if row.get("in_tolerance"):
        return f"{head}: {length}, {target}, within tolerance ({row['deviation_mm']:+.3f} mm)"
    if row.get("need_mm", 0) > 0:
        return f"{head}: {length} (too short), {target}, extend by {row['need_mm']:.3f} mm"
    if row.get("excess_mm", 0) > 0:
        return f"{head}: {length} (too long), {target}, shorten by {row['excess_mm']:.3f} mm"
    return f"{head}: {length}, {target}"


def format_lookup(resp: Dict[str, Any]) -> List[str]:
    lines = [format_row(r) for r in resp.get("nets") or []]
    for u in resp.get("unmatched") or []:
        if not u.get("exists"):
            lines.append(f"{u['label']}: no such net on the board as it was last read (rescan?)")
        else:
            state = "" if u.get("complete") else ", not fully routed"
            lines.append(f"{u['label']}: {u['length_mm']:.3f} mm{state}, not in any length-matched group")
    return lines


class Controller:
    def __init__(
        self,
        engine: Engine,
        kicad: Any = None,
        board: Any = None,
        identifier: str = RELEASE_ID,
        store: Optional[RulesStore] = None,
    ):
        self.engine = engine
        self.kicad = kicad
        self.board = board
        self.identifier = identifier
        # Where the rules are kept between runs; found on the first read of
        # the board, since KiCad names the folder.
        self.store = store
        # The board file the rules belong to.
        self.board_key: str = ""
        self.session: Optional[str] = None
        self.filename: str = ""
        self.source: str = ""
        self._kicad_thread = ThreadPoolExecutor(max_workers=1, thread_name_prefix="kicad")
        self._lock = threading.Lock()

    # ---- threading ----

    def on_kicad(self, fn: Callable[[], Any]) -> "Future[Any]":
        """Run ``fn`` on the KiCad thread."""
        return self._kicad_thread.submit(fn)

    def close(self) -> None:
        self._kicad_thread.shutdown(wait=False, cancel_futures=True)
        self.engine.close()

    # ---- flows ----

    def ensure_connected(self) -> None:
        if self.board is None:
            self.kicad, self.board = boardio.connect()

    def rescan(self) -> str:
        """Read the board from the editor into a new session. Returns its id.

        The parameters the user had set on the last session -- tolerances, the
        clock offset, the areas they drew, what they said an interface is --
        are carried over, so reading the board again does not undo their
        decisions. Parameters that no longer fit the board (an area off its
        edge after the outline moved) are dropped with the defaults kept.
        """
        self.ensure_connected()
        files = boardio.read_board(self.board)
        self.board_key = str(files.project_dir / files.filename) if files.project_dir else files.filename
        # The rules from this window's last read of the board, or failing that
        # the ones saved for it the last time the plugin ran.
        previous = self.current_params() or self._rules().load(self.board_key)
        res = self.engine.upload(files.text, files.filename, files.project, files.rules)
        sid = res["session"]["id"]
        if previous:
            try:
                self.engine.plan(sid, previous)
            except Exception:  # noqa: BLE001 -- keep the defaults rather than fail the rescan
                pass
        with self._lock:
            old, self.session = self.session, sid
            self.filename, self.source = files.filename, files.source
        if old and old != sid:
            # One board is only ever open once; the old copy is dead weight.
            try:
                self.engine.request("DELETE", f"/api/pcb-trace-length-analyzer/sessions/{old}", timeout=10)
            except Exception:  # noqa: BLE001
                pass
        return sid

    def _rules(self) -> RulesStore:
        if self.store is None:
            path = None
            try:
                if self.kicad is not None:
                    path = self.kicad.get_plugin_settings_path(shared_identifier(self.identifier))
            except Exception:  # noqa: BLE001 -- an older KiCad: keep them in the user's config instead
                path = None
            self.store = RulesStore(path or fallback_dir(shared_identifier(self.identifier)))
        return self.store

    def remember(self, params: Optional[Dict[str, Any]]) -> None:
        """Keep the board's rules for the next run of the plugin."""
        if not params or not self.board_key:
            return
        try:
            self._rules().save(self.board_key, params)
        except OSError:
            pass  # a settings folder that cannot be written costs persistence, not the analysis

    def remember_response(self, response: Any) -> None:
        """Keep the rules a plan or apply response says are now in force."""
        try:
            self.remember(response["session"]["params"])
        except (KeyError, TypeError):
            pass

    def current_params(self) -> Optional[Dict[str, Any]]:
        if not self.session:
            return None
        try:
            got = self.engine.get_json(f"/api/pcb-trace-length-analyzer/sessions/{self.session}", timeout=60)
            return got["session"]["params"]
        except Exception:  # noqa: BLE001
            return None

    def analysis(self) -> Dict[str, Any]:
        if not self.session:
            raise RuntimeError("no board has been read yet")
        return self.engine.get_json(f"/api/pcb-trace-length-analyzer/sessions/{self.session}")["analysis"]

    def group_names(self) -> List[str]:
        """Every group a tolerance can be set on, named the way the engine keys them."""
        a = self.analysis()
        names = [g["name"] for g in a.get("groups") or []]
        for i in a.get("interfaces") or []:
            if i.get("planner"):
                continue
            for g in i.get("groups") or []:
                names.append(f"{i['name']} / {g['name']}")
        return names

    def set_tolerance_everywhere(self, mm: Optional[float]) -> None:
        """Hold every group to one tolerance, or back to each group's default."""
        params = self.current_params() or {}
        if mm is None:
            params["group_tolerance_mm"] = {}
        else:
            params["group_tolerance_mm"] = {name: float(mm) for name in self.group_names()}
        self.remember_response(self.engine.plan(self.session, params))

    def attention_nets(self) -> List[str]:
        resp = self.engine.nets(self.session, attention=True)
        seen: Dict[str, None] = {}
        for r in resp.get("nets") or []:
            seen.setdefault(r["net"], None)
        return list(seen)

    def lookup(self, names: List[str]) -> Dict[str, Any]:
        return self.engine.nets(self.session, names=names)

    def select(self, nets: List[str]) -> Dict[str, Any]:
        self.ensure_connected()
        n = boardio.select_nets(self.kicad, self.board, nets)
        return {"selected": n}

    def apply(self, session: str) -> Dict[str, Any]:
        """Apply a session's result to the open board as one commit."""
        if not APPLY_ENABLED:
            raise ApplyDisabled("Applying to the board in KiCad is not enabled in this version.")
        self.ensure_connected()
        change = self.engine.changes(session)
        return boardio.apply_changes(self.board, change)

    def selected_nets(self) -> List[str]:
        self.ensure_connected()
        return boardio.selected_nets(self.board)
