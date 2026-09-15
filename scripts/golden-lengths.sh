#!/usr/bin/env bash
# Extract ground-truth net lengths from KiCad itself.
#
# There is no kicad-cli command that prints net lengths. But a custom design
# rule with an impossible length constraint makes the DRC engine measure every
# net in the class and state the figure it measured in the violation message.
# That is the only way to get pcbnew's own arithmetic out of a script, and it is
# what testdata/golden-lengths.json is built from.
#
# Running it twice, with use_height_for_length_calcs on and off, also isolates
# how much of each net's length is via barrel.
#
# Usage: scripts/golden-lengths.sh <board.kicad_pcb> <out.json> [netclass...]
set -euo pipefail

board=${1:?board path}
out=${2:?output path}
shift 2
classes=("$@")
if [ ${#classes[@]} -eq 0 ]; then classes=(DDR DDR_DIFF); fi

command -v kicad-cli >/dev/null || { echo "kicad-cli not on PATH" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
base=$(basename "$board" .kicad_pcb)
src=$(dirname "$board")

cp "$board" "$work/$base.kicad_pcb"
if [ -f "$src/$base.kicad_pro" ]; then cp "$src/$base.kicad_pro" "$work/"; fi

# The probe rule has to replace the board's own rules file: only one is active.
{
  echo "(version 1)"
  printf '(rule "probe_length"\n  (condition "'
  for i in "${!classes[@]}"; do
    if [ "$i" -gt 0 ]; then printf ' || '; fi
    printf "A.NetClass == '%s'" "${classes[$i]}"
  done
  printf '")\n  (constraint length (max 0.001mm))\n)\n'
} > "$work/$base.kicad_dru"

for use_height in true false; do
  if [ -f "$work/$base.kicad_pro" ]; then
    python3 - "$work/$base.kicad_pro" "$use_height" <<'PY'
import json,sys
p,flag=sys.argv[1],sys.argv[2]=="true"
d=json.load(open(p))
d.setdefault("board",{}).setdefault("design_settings",{}).setdefault("rules",{})["use_height_for_length_calcs"]=flag
json.dump(d,open(p,"w"),indent=2)
PY
  fi
  kicad-cli pcb drc --format json --severity-error \
    --output "$work/drc-$use_height.json" "$work/$base.kicad_pcb" >/dev/null
done

python3 - "$work/drc-true.json" "$work/drc-false.json" "$out" <<'PY'
import json,re,sys
def lengths(path):
    out={}
    for v in json.load(open(path))["violations"]:
        if v["type"]!="length_out_of_range": continue
        L=float(re.search(r"actual ([0-9.]+) mm", v["description"]).group(1))
        net=re.search(r"\[([^\]]+)\]", v["items"][0]["description"]).group(1)
        pads=[]
        for i in v["items"]:
            m=re.match(r"Pad (\S+) \[.*\] of (\S+)", i["description"])
            if m: pads.append(m.group(2)+"."+m.group(1))
        out[net]={"length_mm":L,"pads":sorted(pads)}
    return out
withv, without = lengths(sys.argv[1]), lengths(sys.argv[2])
nets=sorted(withv)
data={"source":"kicad-cli pcb drc length constraint",
      "nets":{n:{**withv[n], "length_no_via_mm":without.get(n,{}).get("length_mm")} for n in nets}}
json.dump(data, open(sys.argv[3],"w"), indent=1, sort_keys=True)
print("wrote %d nets to %s" % (len(nets), sys.argv[3]))
PY
