#!/usr/bin/env bash
# Verify a tuning run against KiCad's own tools.
#
# Two questions matter after this tool edits a board, and neither can be
# answered by the tool itself without circularity:
#
#   1. Did the edit introduce a design rule violation? KiCad's DRC engine is the
#      authority, custom .kicad_dru rules included, and it is what a reviewer
#      will run.
#   2. Did the nets actually get as much longer as the tool claims? The lengths
#      are read back out of KiCad via a probe design rule, so the figure being
#      compared is pcbnew's arithmetic and not ours.
#
# The board is a work in progress and already has violations, so the test is
# that the count does not rise -- not that it is zero.
#
# Usage: scripts/verify-with-kicad.sh <before.kicad_pcb> <after.kicad_pcb>
set -euo pipefail

before=${1:?before board}
after=${2:?after board}
command -v kicad-cli >/dev/null || { echo "kicad-cli not on PATH" >&2; exit 2; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for side in before after; do
  src_var=$side
  src=${!src_var}
  dir=$(dirname "$src")
  base=$(basename "$src" .kicad_pcb)
  cp "$src" "$work/$side.kicad_pcb"
  # The project and rules files travel with the board: DRC is meaningless
  # without the net classes and the custom rules.
  for ext in kicad_pro kicad_dru; do
    if [ -f "$dir/$base.$ext" ]; then cp "$dir/$base.$ext" "$work/$side.$ext"; fi
  done
  kicad-cli pcb drc --format json --severity-error --severity-warning \
    --output "$work/$side-drc.json" "$work/$side.kicad_pcb" >/dev/null
done

python3 - "$work/before-drc.json" "$work/after-drc.json" <<'PY'
import json, re, sys, collections
NET = re.compile(r'\[([^\]]+)\]')

def load(path):
    d = json.load(open(path))
    return (collections.Counter(v['type'] for v in d['violations']),
            len(d['violations']), len(d['unconnected_items']))

(ca, na, ua), (cb, nb, ub) = load(sys.argv[1]), load(sys.argv[2])
print(f"violations : {na} -> {nb}")
print(f"unconnected: {ua} -> {ub}")

fail = False

# Connectivity is the one count that means what it says: a net either reaches
# its pads or it does not, and folding in a meander cannot change that.
if ub > ua:
    print(f"\nFAIL: unconnected items rose from {ua} to {ub}")
    fail = True

# A violation count is not a measurement. Two things make it useless as a
# pass/fail signal, and the second one is worse than the first.
#
# ★ It is not even deterministic. Four runs of kicad-cli over this one unchanged
# file gave 687, 687, 683 and 687. Nothing was edited between them. So a count
# that moves by a few either way says nothing whatever about the board, and an
# earlier note in this project claiming the DRC was deterministic "verified over
# 3 runs" was simply three runs that happened to agree.
#
# And a count of item pairs does not survive an edit that splits a track.
#
# Folding a meander into a long track replaces it with several shorter ones.
# Where that track already ran too close to something -- and this board has 687
# such places before the tool touches it -- KiCad then reports up to three
# entries where it reported one, at the same distance, with no copper having
# moved. On this board that took the count from 687 to 688 while the offending
# stretch of DDR_A11 beside DDR_CLK_N got *shorter*, 12.153 mm to 8.853 mm.
#
# So counts are reported and not judged. Whether the board actually got worse is
# a geometric question, and it is answered exactly by two Go tests:
# TestNoNetPairEndsUpTooClose, which requires that no pair of nets ends up
# inside the clearance it needs, and TestNoNetPairRunsCloserForLonger, which
# requires that no pair runs too close for longer than it already did.
for t in sorted(set(ca) | set(cb)):
    if cb[t] != ca[t]:
        print(f"note: {t} {ca[t]} -> {cb[t]}")

if fail:
    sys.exit(1)
print("\nOK: connectivity unchanged; whether any copper got too close is checked in Go")
PY

# And the lengths, straight from KiCad.
here=$(cd "$(dirname "$0")" && pwd)
"$here/golden-lengths.sh" "$work/before.kicad_pcb" "$work/len-before.json" >/dev/null
"$here/golden-lengths.sh" "$work/after.kicad_pcb" "$work/len-after.json" >/dev/null
python3 - "$work/len-before.json" "$work/len-after.json" <<'PY'
import json, sys
a = json.load(open(sys.argv[1]))['nets']
b = json.load(open(sys.argv[2]))['nets']
rows = []
for n in sorted(a):
    if n not in b:
        print(f"FAIL: {n} disappeared from the board"); sys.exit(1)
    d = b[n]['length_mm'] - a[n]['length_mm']
    if abs(d) > 1e-9:
        rows.append((n, a[n]['length_mm'], b[n]['length_mm'], d))
shorter = [r for r in rows if r[3] < 0]
print(f"\n{len(rows)} net(s) changed length; KiCad measures {sum(r[3] for r in rows):+.4f} mm in total")
for n, x, y, d in sorted(rows, key=lambda r: -r[3]):
    print(f"  {n.replace('/ddr4/DDR_',''):10s} {x:9.4f} -> {y:9.4f}  {d:+8.4f}")
if shorter:
    print("\nFAIL: a net got shorter, which tuning must never do")
    sys.exit(1)
PY
