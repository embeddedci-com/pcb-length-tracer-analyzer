# PCB Trace Length Analyzer — KiCad plugin

The trace length analyzer from embeddedci.com, inside the PCB Editor, on the
board you have open. Nothing is uploaded, and nothing listens on a network port.

Two toolbar buttons:

- **Trace Length Analyzer** opens the report beside the editor: every interface
  on the board (DDR, Ethernet, USB, PCIe, MIPI, SD, differential pairs), what
  each net needs, and the plan to fix it.
  - Click a net's name in the report to select it in KiCad.
  - **Select what needs attention** selects every out-of-tolerance net at once.
  - **Applying to the board is not enabled yet.** The report still plans the
    meanders and previews them; nothing is written to your board. (Built and
    unit-tested, switched off by `APPLY_ENABLED` in `controller.py` until it
    has been tested against a live board.)
  - **One tolerance for every group** holds every group to the same tolerance.
  - **Follow KiCad selection** shows, in the status bar, how far whatever net
    you click in the editor is from its target: "extend by 1.493 mm".
- **Selected Net Length** answers the same question for the selected net
  without opening the report.

Pressing **Trace Length Analyzer** again while the window is open brings that
window forward and reads the board again, rather than opening a second one.
(A lock file with a heartbeat in the temp folder; no socket.)

## Requirements

- KiCad 10.0.1 or newer, with the API server on:
  **Preferences → Plugins → Enable KiCad API**.
- KiCad installs the Python dependencies (`requirements.txt`) into the plugin's
  own environment the first time a button is pressed. PySide6 with QtWebEngine
  is a large download — expect a few hundred MB and a minute or two, once.

## Install

Unzip the release for your platform into KiCad's plugins folder:

| OS      | Folder                                              |
|---------|-----------------------------------------------------|
| macOS   | `~/Documents/KiCad/10.0/plugins/`                   |
| Linux   | `~/.local/share/kicad/10.0/plugins/`                |
| Windows | `%USERPROFILE%\Documents\KiCad\10.0\plugins\`       |

Restart KiCad. The two buttons appear on the PCB Editor's toolbar.

Install by hand, not through the Plugin and Content Manager: PCM installs of
IPC plugins crash KiCad on Windows through 9.0.x and have lost plugins in
10.0.1-rc1.

### Troubleshooting

- **No buttons.** Check the API server is on, then restart KiCad. On some Linux
  installs KiCad fails to create the plugin's environment silently; create it
  yourself:
  ```bash
  python3 -m venv ~/.cache/kicad/10.0/python-environments/com.embeddedci.pcb-trace-length-analyzer
  ~/.cache/kicad/10.0/python-environments/com.embeddedci.pcb-trace-length-analyzer/bin/pip install -r requirements.txt
  ```
- **"The analyzer engine is not installed".** The zip for another platform was
  used; each carries only its own `bin/<os>-<arch>/` engine.
- **macOS says the engine cannot be opened.** Until releases are notarized:
  `xattr -dr com.apple.quarantine <plugins>/pcb-trace-length-analyzer`.
- Set `PCB_TLA_DEBUG=1` in KiCad's environment to have the engine's log and the
  page's console printed to stderr.

## How it works

```
KiCad (pcbnew) ── IPC API ──> plugin process (Python, PySide6)
                                 │  Qt window + QtWebEngine view
                                 │    kicad-engine://app/…   ← URL scheme answered in-process
                                 │       /api/…   ──pipes──> engine (Go, child process)
                                 │       /kicad/… ──> select nets, apply edits, rescan
                                 └  static report bundle (web/)
```

- **The engine** is the same Go control plane embeddedci.com mounts, built as
  `pcb-trace-length-analyzer-engine`. It serves HTTP handlers over stdin/stdout
  (`pipehttp`): length-prefixed frames, requests answered concurrently. When the
  plugin exits the pipe closes and the engine exits with it.
- **The report** is the website's own front end (`webapp/src`), wrapped by
  `webapp/kicad/main.tsx`. A host context turns on the KiCad-only controls.
- **The board** is read from the editor (`SaveDocumentToString`), unsaved edits
  included, so your file is never saved for you. The `.kicad_pro` and
  `.kicad_dru` beside it are read from disk.
- **Apply** (switched off for now) asks the engine for the result as edits (`GET …/changes`: tracks to
  remove by uuid with their geometry, tracks to add in nanometres), checks
  every track to be removed is still on the board unchanged, and makes the
  edits between `begin_commit` and `push_commit`.

Two things a scheme handler cannot do, and how they are covered:

- It can only reply 200: the real status travels in `X-Status` and the page's
  fetch restores it.
- PySide6 6.10 crashes on `QWebEngineUrlRequestJob.requestBody()`: the page
  sends request bodies as base64 in `X-Body`.

## Staying on par with the site

Nothing here is a copy, so there is no second report or engine to keep in sync:

- **The report** is built from `webapp/src`, the same folder embeddedci-server
  aliases as `@traces`. `webapp/kicad/` only wraps it (router, fetch, host).
  A change to the site's pages is in the plugin at the next `make plugin`.
- **The engine** is built from the same `server` package the site mounts.
  A new endpoint or a changed analysis is in the plugin at the next build.
- **KiCad-only controls** live in `webapp/src` behind `useHost()`, which is null
  on the site: `HostActions.test.tsx` checks they render nothing there.

What can drift is the seam, and each part of it has a test:

| Seam | Checked by |
|------|------------|
| the page's fetch over the scheme (status, bodies) | `webapp/kicad/fetch.test.ts` |
| the Python client against the engine's endpoints | `tests/test_engine.py`, `tests/test_controller.py` (real engine, demo board) |
| the report actually rendering in QtWebEngine | `tests/test_webview.py` |
| `/nets` and `/changes` | `server/kicad_test.go` |

`make test` runs the webapp half (including `kicad/`); `make test-all` also
runs the plugin tests when `PLUGIN_PY` has pytest, PySide6 and kicad-python.
`kicad-plugin/web/` is generated and gitignored, so a linked install shows the
report as of the last `make plugin`: rebuild after changing `webapp/src`.

## Developing

From the repository root:

```bash
make plugin           # engine + report into kicad-plugin/
make plugin-install   # link kicad-plugin/ into KiCad's plugins folder
make plugin-test PLUGIN_PY=/path/to/python   # needs pytest, PySide6, kicad-python
make plugin-dist      # release zips into dist/
```

The tests run the real engine against the demo board, and render the report
headless in QtWebEngine (`QT_QPA_PLATFORM=offscreen`). Only pcbnew is stood in
for.
