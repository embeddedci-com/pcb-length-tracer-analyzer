"""Keeping a plugin window from taking focus away from KiCad on macOS, and
handing focus back to it.

Every plugin process is the same Python application as far as macOS is
concerned. A new window from it activates "Python" -- which can bring an
already open analyzer window forward over the PCB Editor -- and when it closes,
focus goes to whichever Python window is next rather than back to KiCad.

A popup that only reports a length should do neither. The fix is to make the
process an accessory application: no Dock icon, no menu bar, and not activated
by showing a window. PySide6 has no call for that, so it is made directly on
NSApplication through the Objective-C runtime; no PyObjC needed.

Selecting nets from the analyzer window is the opposite case: the user is in
the plugin and the next thing they want is KiCad, to look at what was selected.
activate_kicad() brings KiCad forward, so the selection can be used without a
click to get back to it first.

Everything here is a no-op elsewhere, where Qt's window flags are enough.
"""

from __future__ import annotations

import ctypes
import ctypes.util
import os
import subprocess
import sys
from typing import Iterator, Optional

# NSApplicationActivationPolicyAccessory
_ACCESSORY = 1

# NSApplicationActivateAllWindows | NSApplicationActivateIgnoringOtherApps
_ACTIVATE = 1 | 2

# The standalone PCB Editor is its own app; run from the project manager, the
# editor lives inside KiCad's.
_KICAD_BUNDLES = (b"org.kicad.kicad", b"org.kicad.pcbnew")


def _objc():
    lib = ctypes.util.find_library("objc")
    if not lib:
        return None
    objc = ctypes.cdll.LoadLibrary(lib)
    # NSRunningApplication and NSApplication live in AppKit. Qt has loaded it
    # already inside the plugin; loading it again is free, and makes this work
    # in a process without Qt too.
    appkit = ctypes.util.find_library("AppKit")
    if appkit:
        ctypes.cdll.LoadLibrary(appkit)
    objc.objc_getClass.restype = ctypes.c_void_p
    objc.objc_getClass.argtypes = [ctypes.c_char_p]
    objc.sel_registerName.restype = ctypes.c_void_p
    objc.sel_registerName.argtypes = [ctypes.c_char_p]
    return objc


def make_accessory() -> bool:
    """Turn this process into an accessory app. Call after the QApplication exists.

    Returns True when it was applied.
    """
    if sys.platform != "darwin":
        return False
    try:
        objc = _objc()
        if objc is None:
            return False
        send = objc.objc_msgSend
        # [NSApplication sharedApplication]
        send.restype = ctypes.c_void_p
        send.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
        app = send(objc.objc_getClass(b"NSApplication"), objc.sel_registerName(b"sharedApplication"))
        if not app:
            return False
        # [app setActivationPolicy:NSApplicationActivationPolicyAccessory]
        send.restype = ctypes.c_bool
        send.argtypes = [ctypes.c_void_p, ctypes.c_void_p, ctypes.c_long]
        return bool(send(app, objc.sel_registerName(b"setActivationPolicy:"), _ACCESSORY))
    except Exception:  # noqa: BLE001 -- a focus nicety must never stop the plugin
        return False


def _ancestors(pid: int) -> Iterator[int]:
    """This process's parent, its parent, and so on up to launchd."""
    seen = set()
    while pid > 1 and pid not in seen:
        seen.add(pid)
        try:
            out = subprocess.run(["ps", "-o", "ppid=", "-p", str(pid)], capture_output=True, text=True, timeout=2)
            pid = int(out.stdout.strip() or 0)
        except (OSError, ValueError, subprocess.SubprocessError):
            return
        if pid > 1:
            yield pid


_kicad_pid: Optional[int] = None


def _msg(objc, restype, receiver, selector: bytes, *args):
    """Send one Objective-C message. ``args`` are (ctypes type, value) pairs.

    objc_msgSend is one C function called with a different signature each
    time, so the signature is set per call on a fresh function pointer rather
    than on a shared one, where a return type left over from the last call
    turns a pointer into a crash.
    """
    fn = ctypes.CFUNCTYPE(restype, ctypes.c_void_p, ctypes.c_void_p, *(t for t, _ in args))(
        ctypes.cast(objc.objc_msgSend, ctypes.c_void_p).value
    )
    return fn(receiver, objc.sel_registerName(selector), *(v for _, v in args))


def _is_kicad(objc, app) -> bool:
    """Whether a running application is KiCad, by its bundle id.

    An ancestor that is an app is not necessarily KiCad: run from a terminal or
    another tool, the first app up the tree is that tool, and bringing it
    forward would be worse than doing nothing.
    """
    ident = _msg(objc, ctypes.c_void_p, app, b"bundleIdentifier")
    if not ident:
        return False
    raw = _msg(objc, ctypes.c_char_p, ident, b"UTF8String")
    return bool(raw) and raw.startswith(b"org.kicad.")


def _kicad_app(objc) -> Optional[int]:
    """The NSRunningApplication for the KiCad that started this plugin.

    Found by walking up the process tree rather than by name, because two KiCad
    instances can be open and only one of them has the board the selection went
    to. Failing that, a running KiCad by bundle id.
    """
    global _kicad_pid
    cls = objc.objc_getClass(b"NSRunningApplication")

    def by_pid(pid: int):
        return _msg(objc, ctypes.c_void_p, cls, b"runningApplicationWithProcessIdentifier:", (ctypes.c_int, pid))

    if _kicad_pid is None:
        _kicad_pid = 0
        for pid in _ancestors(os.getpid()):
            app = by_pid(pid)
            if app and _is_kicad(objc, app):
                _kicad_pid = pid
                break
    if _kicad_pid:
        app = by_pid(_kicad_pid)
        if app:
            return app

    nsstring = objc.objc_getClass(b"NSString")
    for bundle in _KICAD_BUNDLES:
        ident = _msg(objc, ctypes.c_void_p, nsstring, b"stringWithUTF8String:", (ctypes.c_char_p, bundle))
        apps = _msg(objc, ctypes.c_void_p, cls, b"runningApplicationsWithBundleIdentifier:", (ctypes.c_void_p, ident))
        first = _msg(objc, ctypes.c_void_p, apps, b"firstObject") if apps else None
        if first:
            return first
    return None


def activate_kicad() -> bool:
    """Bring KiCad to the front. Returns True when it was asked to."""
    if sys.platform != "darwin":
        return False
    try:
        objc = _objc()
        if objc is None:
            return False
        app = _kicad_app(objc)
        if not app:
            return False
        # Since macOS 14 activation is cooperative: the active app yields to
        # the one it hands over to. Older systems have no such call and do not
        # need it.
        me = _msg(objc, ctypes.c_void_p, objc.objc_getClass(b"NSApplication"), b"sharedApplication")
        yield_to = objc.sel_registerName(b"yieldActivationToApplication:")
        if me and _msg(objc, ctypes.c_bool, me, b"respondsToSelector:", (ctypes.c_void_p, yield_to)):
            _msg(objc, None, me, b"yieldActivationToApplication:", (ctypes.c_void_p, app))
        return bool(_msg(objc, ctypes.c_bool, app, b"activateWithOptions:", (ctypes.c_ulong, _ACTIVATE)))
    except Exception:  # noqa: BLE001 -- a focus nicety must never stop the plugin
        return False
