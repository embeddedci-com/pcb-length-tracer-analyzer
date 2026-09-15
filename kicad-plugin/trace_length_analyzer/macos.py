"""Keeping a plugin window from taking focus away from KiCad on macOS.

Every plugin process is the same Python application as far as macOS is
concerned. A new window from it activates "Python" -- which can bring an
already open analyzer window forward over the PCB Editor -- and when it closes,
focus goes to whichever Python window is next rather than back to KiCad.

A popup that only reports a length should do neither. The fix is to make the
process an accessory application: no Dock icon, no menu bar, and not activated
by showing a window. PySide6 has no call for that, so it is made directly on
NSApplication through the Objective-C runtime; no PyObjC needed.

Everything here is a no-op elsewhere, where Qt's window flags are enough.
"""

from __future__ import annotations

import ctypes
import ctypes.util
import sys

# NSApplicationActivationPolicyAccessory
_ACCESSORY = 1


def _objc():
    lib = ctypes.util.find_library("objc")
    if not lib:
        return None
    objc = ctypes.cdll.LoadLibrary(lib)
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
