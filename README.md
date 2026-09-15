# pcb-trace-length-analyzer

A PCB trace length analyzer for KiCad boards. Reads a `.kicad_pcb`, finds the
interfaces on it -- DDR, Ethernet, USB, PCIe, MIPI, SD and any differential
pair -- measures how far each net is from the length it should be, and says what
to do about each one: match it where it is, open some room beside it, or route
it to a given length instead.

It can also fold the meanders in for you. That half is opt-in and experimental;
the analysis is the product.

DDR is the one it plans and tunes in full, because it is the hardest case: byte
lanes matched to their own strobe, address and command matched to the clock per
leg of the fly-by chain. The rest are recognised from the net names, measured,
and reported for you to confirm or correct.

It does not route. Nothing here creates a connection, moves a via, changes a
layer or rips anything up: a straight stretch of an existing track is replaced
by a longer path between the same two points, and everything else is left
exactly as it was.

**Reading a board is the solid half; writing to one is experimental.** The
report, the measurements and the triage are what this tool is for and are
checked against KiCad and against golden lengths. The changes it writes are
checked against KiCad's own DRC on the boards here and behave -- but no board
tuned by this has come back from a fab and been measured. Run DRC on the
result, look at the copper, and treat what comes out as a draft. Your own file
is never written to; the result is always a new one.

## Layout

```
cmd/pcb-trace-length-analyzer/        the command line tool
cmd/pcb-trace-length-analyzer-server/ the control plane on its own, for development
sexpr/                     KiCad S-expression parser, byte-exact round trip
geom/                      planar geometry: points, segments, three-point arcs, shapes
board/                     the board model: stackup, footprints, pads, tracks, zones
netlen/                    connectivity and length: what is routed, and how long it is
ddr/                       what a DDR interface is, and what has to match what
drc/                       clearance checking, indexed for speed
tune/                      meander generation and insertion
report/                    what the user reads before anything is changed
server/                    the HTTP control plane, mounted into a host application
webapp/                    React front end; src/ is a drop-in folder for the host
scripts/                   fixtures and verification, both driven by kicad-cli
demo-pcb/                  a real six-layer STM32MP257 + DDR4 board, used by the tests
testdata/                  net lengths extracted from KiCad, used as ground truth
```

## Using it

```
make report                       # analyse the demo board, change nothing
make tune                         # tune a copy into out/, then verify with KiCad
make serve                        # the web front end and the API on localhost:8091
make test                         # unit tests, no KiCad needed
make test-all                     # plus the end-to-end run against kicad-cli
```

Against your own board:

```
pcb-trace-length-analyzer board.kicad_pcb                                  # report only
pcb-trace-length-analyzer -apply -out tuned.kicad_pcb board.kicad_pcb      # report, ask, write
pcb-trace-length-analyzer -apply -only-group "byte lane 3" board.kicad_pcb # one group
pcb-trace-length-analyzer -clock-offset 2.5 board.kicad_pcb                # address 2.5% over clock
pcb-trace-length-analyzer -apply -expand board.kicad_pcb                   # spread tight buses first
pcb-trace-length-analyzer -open-clearance 0.25 board.kicad_pcb             # 0.25 mm between traces in the open
```

Reading a board never writes to it. `-apply` prints the same report first and
asks before it changes anything; `-yes` skips the prompt for scripts, and
`-dry-run` tunes in memory and reports without writing.

## What else is on the board

DDR is the hardest interface to length-match, not the only one. The board is
read for everything it carries — Ethernet, USB, PCIe, MIPI, SD/eMMC, and any
differential pair that belongs to none of them — and each one is listed with
what is wrong with it, so the choice of what to work on is yours:

```
Interfaces on this board: 10
  * DDR memory                ddr    71 nets, 71 routed  4 pair(s) out of intra-pair tolerance
    Ethernet RGMII (ETH1)     rgmii  17 nets,  2 routed  it has copper, but no net is joined end to end yet
    MIPI D-PHY (CSI)          mipi   12 nets,  0 routed  none of it is routed yet
    PCI Express               pcie   10 nets,  6 routed  it has copper, but no net is joined end to end yet
```

Recognition is by net name, which is a reading and not a proof: nothing in the
geometry says a pair is PCIe rather than SATA. So the tool proposes and you
decide. Each interface says what it matched on, and each can be reassigned to
whatever it really is — the picker lists what every family usually carries
("a MIPI link usually carries D0+/-, D1+/-, D2+/-, D3+/- and CK+/- either way"),
because that is how you recognise your own nets in a list. Reassigning applies
that family's limits and impedance targets, and says whether it could find that
family's structure in your names or only its pairs.

Two numbers decide an interface's impedance: how wide its tracks are, and how
far apart the two halves of a pair run. Where it is routed, both come off the
board and are offered for confirmation; where it is not, the tool asks. Either
way it says what the geometry comes out at against what the family usually wants
— 94.8 Ω differential against USB's 90, say — as an estimate from the stackup in
the board file. That is good for catching a width nowhere near its target and is
not a substitute for the fabricator's stackup and a field solver, which the
report says rather than implying otherwise.

Two questions get asked of each. Whether the two halves of every differential
pair match each other — which applies to nearly every fast interface, needs no
knowledge of the protocol, and is where this earns its keep on USB, PCIe and
MIPI. And whether the members of a source-synchronous group match the clock they
travel with — RGMII data against GTX_CLK, SD data against SDMMC_CK, MIPI lanes
against the clock lane. The default limits are widely published starting points
and are marked as such; the figure that matters is in your controller's layout
guide.

## What it understands about DDR

A length is only meaningful next to the right reference, so the nets are
classified before they are measured:

The first thing it reports is the fly-by chain, above anything about lengths,
because it decides whether those lengths mean anything. Address, command,
control and clock have to reach the devices in series and end in a termination
resistor: that topology is what makes write levelling work, and a T or a star
leaves a stub on every one of those nets. The report names the chain, says which
hops the board actually has, and says how it worked the order out — the order
decides which span each length is compared over, so it comes from the placement
and, where the copper joining the devices exists, from that.

On the demo board the answer is blunt: `U3 -> U4` is routed on all 26 nets,
`U4 -> U5` on none of them. KiCad reports no unconnected item for any of them,
because each is a net with copper on it.

- a **DQ** bit or a **DM** mask is matched to the **strobe of its own byte
  lane**, and to nothing else;
- the two halves of a **DQS** or **CK** pair are matched to each other, far more
  tightly;
- **address and command** lines are matched to the **clock**, optionally offset
  by a percentage of it.

Byte lanes are worked out from the topology, not from names. On a x32 interface
built from two x16 devices, `DQS2` serves the lane carrying `DQ16..DQ23`;
nothing in the name says so, and the classifier reads it off which device each
net lands on. Names that merely look differential — `CASN`, `ACTN`, `RESETN` —
stay single-ended.

**Fly-by nets are matched per leg.** Address and command lines reach the memory
devices in series, and equal total length with unequal legs still fails write
levelling, so each leg is its own group.

Lengths are reported both as millimetres, which is what you check in pcbnew, and
as picoseconds, computed per layer from the stackup. A net that changes layers
mid-run — which every fly-by net on the demo board does — is mismatched by about
30 per cent of the difference if it is matched geometrically, because a
microstrip millimetre and a stripline millimetre are not the same delay.

## Where a meander can go, and how much it can add

The room beside a track varies along it. Asking for one amplitude that holds over
a whole track finds nothing at all on a bus routed at its minimum clearance, so
the room is profiled in steps and the best non-overlapping stretches are used.

`Headroom` reports how much length that room could hold, per net, and the report
divides the shortfalls into the three things you can do about them:

| | |
|---|---|
| **have the room** | apply it |
| **short of room** | the tool adds what fits; opening space in the layout closes the rest |
| **need rerouting** | asking for more than a quarter of their own length, which no meander supplies |

It also measures the clear space beside the buses these nets run in, which bounds
what re-spacing a bus could add — the number to look at before deciding that
moving the neighbours is worth it.

## Spreading a bus into space it has beside it

A bus routed at its minimum clearance has nowhere to fold a meander, however
much clear space lies beside the bus as a whole. `-expand` opts into moving it:
each trace steps sideways at 45 degrees inside its own extent, runs along its
new line, and steps back, so gaps open between them. Endpoints do not move, the
traces keep their order, and nothing is rerouted — but traces the user did not
select do move, so it is off by default and the report names every one of them.

On the demo board it takes the total added from 58.8 mm to 61.1 mm and brings
one more net inside tolerance. Two properties of the board bound what it can
achieve, and the report says which
one it hit for every bus it could not help. Moving a trace lengthens it, so a
trace that needs no length cannot move; and the gap a trace gets is how much
further the trace beyond it moved, so the outermost trace's own requirement caps
everything inside it. Byte lane 2 is the instructive case: 5 mm of clear space,
all of it behind DQ19, the one trace on the lane already at the target.

## Clearance in the open, not around the BGA

A fine-pitch BGA is the hardest part of a board to route, so the clearance on
the netlist is usually whatever got the escape out — 0.1 mm is common — and the
whole board inherits it. Between the components there is normally more room.

`-open-clearance` (0.2 mm by default, 0 to switch it off) holds this tool to a
wider figure away from the components while leaving the board's own rules in
force around them; `-open-margin` sets how far outside a component's pads still
counts as being around it. A differential pair is exempt either way. It only
tightens — a board whose own rule is stricter keeps it — and it applies to every
measurement, not just the copper written out, so the room the report promises is
the room the tuner works to.

## Meandering can only add length

There is no way to shorten a track without rerouting it. So a group's target
starts at what its reference asks for and is then raised to clear the group's
longest member; where that happens the report says so, and says by how much the
reference itself now falls short.

Where the target cannot be reached at all, the tool adds what fits and reports
the rest. It does not quietly stop, and it does not pretend.

## What it will not do

It will not make anything worse. A meander is drawn only if the copper it adds
clears every design rule, and only if the complete replacement — the meander and
the untouched ends of the original track alike — is no closer to anything than
the track it replaces. That distinction matters on a real board: the demo board
already has 687 violations, and re-creating an unchanged stretch of track
inherits whatever it was already too close to.

It will not exceed its brief either. Meanders keep three track widths of clear
space between their legs, because coupling between them eats the delay the
meander was added for; where a differential pair's halves are too far apart to
fix by padding one of them, that is reported as needing a reroute rather than
done badly.

## Seeing it

A length in a table is a claim. `/api/pcb-trace-length-analyzer/sessions/{id}/preview/board.json`
and its `geometry.bin` are the copper that claim was written into: the board as
triangles a browser can draw, in the format the EMI Analyzer's viewer already
reads, so the front end reuses that viewer rather than growing a second one.
`?of=result` is the board after tuning, and flipping between the two at the same
zoom is the only way to see a meander — it is obvious on screen and invisible in
a diff.

The front end picks a net to highlight and frames it, marks the nets an apply
changed, and keeps the view when you flip between before and after. Copper pours
are off by default: they are most of a board's copper by area, they sit under
the traces this tool changes, and drawing them doubles the download.

## The board's own design rules

A `.kicad_dru` is read as far as it can be trusted. What matters is the one kind
of rule that changes where copper may go: a clearance with a condition. The demo
board has five, and three say the same important thing — inside a BGA's
courtyard, 0.1 mm is allowed rather than the 0.2 mm the net class asks for.

Those used to be reported as "not interpreted" and the tool worked to the
stricter figure. That is the right default and it cost nothing while the tool
only lengthened existing traces — being stricter means declining to put a
meander somewhere it would have fitted. It stopped being free once the tool
could create copper: **a BGA ball cannot be escaped at 0.2 mm**, so every one of
the demo board's 26 second-hop connections was refused as having no way through.

Reading a rule that *relaxes* a clearance is a different risk from ignoring one,
so three things hold it down. Only conditions it fully understands are used, and
anything else is named in the report rather than guessed at. A relaxation
applies only when the condition holds for **both** pieces of copper, where KiCad
asks about one. And "inside a courtyard" means wholly inside, where KiCad says
intersects. All three are stricter than KiCad, in the direction that declines
rather than allows — and the flow still ends by asking KiCad, which is the only
authority on its own rule language.

## Where copper may go

Deciding how much length a net needs is arithmetic. Deciding **where** to put it
is a judgement about the board, and "wherever there is room" is not the same
answer as "wherever you would want it" — room beside a trace can be in a BGA
fanout, under a connector, across a split in a reference plane, or in the one
part of the board somebody has left clear on purpose. A clearance check cannot
tell the difference.

So the person who drew the board says. In the web front end, the step before
applying is a board view with a **Draw an area** button: drag out the regions
the tool may meander into, and they are listed and can be removed. On the
command line, `-area x0,y0,x1,y1` in board millimetres, repeated for several.

**Suggest areas** fills them in first, so you adjust rather than invent. A blank
board and "draw where copper may go" is a worse question than it looks — the
answer depends on where the nets that need length run and where there happens to
be space beside them, both of which the tool has already measured and you would
have to hunt for. On the demo board it proposes two regions holding 74.0 mm of
the 75.9 mm the whole board offers. They are not a recommendation: the tool
cannot see a connector, a plane split, or the part of the board you were keeping
clear. They are where the length could come from.

Each area carries **its own minimum spacing**, because that is where you know
it. A global "keep 0.25 mm apart away from the components" makes the tool guess
what "away from the components" means; "keep 0.25 mm apart in here" needs
nothing guessed, since you drew the here. It only ever tightens — an area is
somewhere you chose to put copper, not a licence to crowd it.

Leaving it empty means anywhere, which is what the tool did before and is right
for a first look. Once areas are given, nothing is added outside them — not a
meander, not a bus being spread — and the room reported drops to what is
actually usable, so the plan you read is the plan you get.

The area is a statement about where copper may be, not about where centrelines
may be: a meander wanders sideways from the track it is folded into, so the
room at each point is capped by the distance to the area's edge as surely as by
a neighbouring track.

**And it tells you whether the area will do.** Drawing a region and being told
nothing is only half a tool, so the room inside it is measured — about a second,
and faster with an area than without, since there is less to look at — and
reported as a verdict: how much of what is needed is reachable, per group, and
what the shortfall is made of. That last part is the useful half:

> 43.554 mm of the 235.223 mm needed is reachable inside the areas: 1 of 36 nets
> have all the room they need. Of the 191.669 mm still missing, 40.474 mm is on
> nets that are merely short of room — a bigger area may reach it. 151.195 mm is
> on 20 nets asking for more than a quarter of their own length, which no area
> can supply: those need rerouting, not more space.

The two halves of a shortfall are different problems. One is answered by drawing
a bigger area; the other never is, because the limit is the track rather than
the space around it.

## Verifying the result

The tool's own checks are not the last word, and it does not interpret custom
`.kicad_dru` rules. Every run ends by telling you to ask KiCad, and
`make verify` does it:

```
scripts/verify-with-kicad.sh before.kicad_pcb after.kicad_pcb
```

That runs KiCad's DRC on both boards and compares them, and reads the net
lengths back out of KiCad to confirm they grew by what was claimed. There is no
kicad-cli command that prints a net length, so `scripts/golden-lengths.sh` gets
them out with a design rule that every net violates, which makes the DRC engine
state the length it measured. The same trick produced
`testdata/golden-lengths.json`, against which the length engine is tested.

## The web front end

`make serve` builds `webapp/` and serves it alongside the API at
<http://localhost:8091/tools/pcb-trace-length-analyzer>. Upload a board, read the report, set the
tolerances and the clock offset, choose what to change, and download the result — the same
report-then-confirm-then-apply order the command line uses, for the same reason.

Upload the `.kicad_pro` along with the board. Without it there are no net classes, so every
clearance falls back to the board minimum and the tool works to rules the design did not
set. The report says when it is missing.

### How it will ship

The same way the EMI analyzer does: mounted into embeddedci-server rather than deployed
beside it. There is no container, no queue and no worker, because the engine is Go and runs
in process — reading a 2.4 MB board and measuring its whole DDR interface takes about
150 ms, so a request answers directly.

| half | linked by | seam |
|---|---|---|
| `server/` | one `replace` in `embeddedci-server/go.mod` → `../pcb-trace-length-analyzer` | `server.New(deps).Mount(mux, prefix, userAuth)` |
| `webapp/src/` | a Vite alias → `embeddedci-server/webapp/src/pcb-trace-length-analyzer/` | `autorouteRoutes(api)`, a route fragment with no prefix of its own |

One module and one `replace`, deliberately: a path-based link per half is what made CI need
two checkouts last time.

Everything the host has to supply is an interface in [server/deps.go](server/deps.go):
where to keep an uploaded board (`Store`), and who is asking. This package authenticates
nobody — it refuses to act for an identity the host did not vouch for with `WithUser`, and
an anonymous request is refused rather than filed under nobody.

`cmd/pcb-trace-length-analyzer-server` is a development harness, not a deployment target. It has no
authentication and runs every request as whatever `-user` says.

## The demo board

`demo-pcb/` is a real work-in-progress six-layer board: an STM32MP257 with two
x16 DDR4 devices in a fly-by chain. It is the fixture for almost every test
here, and it is genuinely unfinished in ways worth knowing about, because the
tests assert them:

- the 44 point-to-point nets — the DQ bits, strobes and masks — are fully routed;
- **not one of the 27 fly-by nets has its second leg routed**, from the near
  device to the far one, and only the clock pair has any copper at its
  termination. KiCad's DRC reports no unconnected item for any DDR net and sums
  length straight across the gap, so this tool works routing completeness out
  from the geometry instead;
- byte lanes 0, 1 and 2 were tuned by hand and sit within about 0.7 mm; lane 3
  was never touched and is spread over 7.6 mm;
- **the byte lanes are routed at exactly the minimum DDR clearance.** The
  nearest neighbour of one DQ24 segment sits 0.2000 mm away against a 0.2 mm
  rule. The room beside a track is not uniform along it, though, and profiling
  it in steps finds sixty-seven times more usable capacity on byte lane 3 than
  asking for one amplitude over the whole track — enough to give DQ24 its full
  7.609 mm;
- **the fly-by bus is matched against a clock nearly twice the length of its
  shortest member.** A11 is asked to grow by 14.251 mm on a 16.888 mm route. No
  meander does that, so the tool says so rather than trying: twenty of the
  thirty-six candidates are flagged as needing a reroute, which is why the
  shortfall stays large however good the tuning gets.
