# Design

How the tool is put together, and the things about KiCad that had to be worked
out before any of it could be trusted.

## Shape of the thing

Seven packages, in dependency order.

| package | job |
|---|---|
| `sexpr` | parse and write KiCad's S-expressions, byte-exact |
| `geom` | points, segments, three-point arcs, pad shapes, distances |
| `board` | the board model over the parsed tree; net classes from the project |
| `netlen` | connectivity and length: what is routed, and how long it is |
| `ddr` | what a DDR interface is; grouping and matching rules |
| `drc` | is this copper allowed here |
| `tune` | meander geometry, and splicing it into a track |

`report` renders, `cmd/pcb-trace-length-analyzer` sequences.

## Byte-exact round trip

A `.kicad_pcb` is a file the user opens in pcbnew every day, and the demo board
is 2.4 MB. A tool that rewrote all of it to change four tracks would produce an
unreviewable diff, and would silently drop any construct its parser did not
model.

So every node remembers the source text it came from, including the whitespace
between its children, and writing echoes that text unless the node actually
changed. There are three cases: an untouched subtree is copied verbatim; a node
whose descendant changed keeps its own separators and recurses, so only the
changed descendant is reformatted; a node that itself changed regenerates its
separators in KiCad's style. Loading and saving the demo board, a 494 KB
schematic and the rules file all come back identical byte for byte, which is a
test.

New numbers are rounded onto KiCad's 1 nm grid before they are written, because
a coordinate that does not sit on that grid reads back as a different value and
lengths stop adding up.

## KiCad 10 moved the nets

In format 20260206 nets are referenced **by name** — `(net "GND")` — in pads,
segments, arcs and vias. The numeric net index that KiCad 7, 8 and 9 used is
gone. Code written against the old semantics does not parse these files.

## What KiCad means by "net length"

This mattered enough to reverse-engineer properly, because a tool that reports a
length the user cannot reproduce in pcbnew is a tool the user cannot check.

There is no kicad-cli command that prints a net length. But a custom rule with
an impossible length constraint makes the DRC engine measure every net in the
class and state the figure in the violation message, so
`scripts/golden-lengths.sh` gets pcbnew's own arithmetic out of a script. Running
it with `use_height_for_length_calcs` both ways isolates the via contribution.
What that shows:

**KiCad's figure is a sum, not a route.** It adds up every piece of copper on
the net — dangling stubs, both sides of a loop, and even islands no signal could
reach — plus qualifying via barrels. The `Pad A … Pad B` items in the message
are the two extreme pads, not the path measured. On a finished board the sum and
the route agree; on a board still being routed they do not, and the difference is
the signal that something is unrouted.

**A via barrel counts only if the net has copper on two or more of the layers it
spans.** Every via on the demo board is declared F.Cu to B.Cu, so counting them
naively charges three barrels on an address net where pcbnew charges two — a
1.594 mm error, larger than any DDR matching budget. That one rule is why the
clock nets report three barrels and every address net two: the third via is on
the leg that was never routed.

**A barrel is worth the full stackup height**, 1.594 mm here, measured from the
top face of the upper layer to the bottom face of the lower one.

**pcbnew shortens the part of a track lying inside a pad**, by an amount that
depends on how it polygonises the pad outline. Per-segment bisection — delete one
segment, re-run DRC, see what the total loses — shows only the segments touching
a BGA pad lose anything, and the loss is a per-pad constant that scales with pad
radius: 0.0117 mm at a 0.15 mm pad, 0.0134 mm at 0.175 mm. Hand-built synthetic
cases would not reproduce the rule, so it is left unmodelled. `KiCadLength` is
therefore exact on the 33 of 71 nets whose copper clears its pads and reads up
to 0.22 mm high on the rest, which is asserted rather than hoped for.

## Connectivity is copper overlap

pcbnew's router does not always land two tracks on the same coordinate. Corners
on the demo board are out by up to 30 µm, and one meander turn leaves a 66 µm gap
between two arcs with no segment between them at all. All of it is solid copper,
because the tracks are 90 µm wide. Keying a graph on exact coordinates finds
several DDR nets in a dozen disconnected pieces.

So two ends are the same junction when their copper overlaps, and they are
**merged into one node** rather than bridged with a zero-length edge. The
difference is not cosmetic. An earlier version bridged, and a route could then
cut the corner where two tracks meet through a short connector segment — a real
copper shortcut, but not the length any tool, guide or reviewer means by "track
length". It cost 0.52 mm on DQ13, a net with a dozen tuning corners each leaking
a little, and it made the lanes that had already been tuned by hand look
mismatched.

Two safeguards keep that from coming back. A merge is refused when it would put
both ends of one track in the same cluster, which is what stops a 14 µm connector
segment at a tuning corner from collapsing. And a track is divided where
something lands partway along it only when that division is what joins two
otherwise separate pieces — a fly-by T needs it, a near-corner artifact does not.
The test is differential: every two-pad net is measured with dividing on and off,
and the answers must agree to the micrometre. They agree exactly.

Pad connections are charged the straight-line distance from the anchor to where
the copper lands, which costs the same as walking the copper there and so leaves
no route cheaper than the centreline.

## Byte lanes come from the topology

A DQ bit's lane follows from its number. A strobe's does not. On a x32 interface
made of two x16 devices, `DQS2` serves the lane carrying `DQ16..DQ23`, and the
only reliable way to know is to see which device each net lands on. So lanes are
derived from the data bits first, and strobes and masks are attached to the lane
they share a device with.

Names that end in `N` are a trap: `CASN`, `ACTN`, `WEN` and `RESETN` are
active-low singles, not the complement halves of pairs. A polarity rule that
looks only at the last letter invents pairs that do not exist and then tries to
match them against nothing.

## Targets only ever rise

A meander adds copper; nothing here shortens a track. So a group's target starts
at what its reference asks for and is then raised to clear the group's longest
member. Where that happens the group ends up matched to itself rather than to
the reference, and the gap is recorded and reported rather than absorbed.

## Not making anything worse

The demo board has 687 design rule violations before this tool touches it, so
"the edited copper is clean" is the wrong test. Replacing a track re-creates the
parts of it that did not change, and if the original was too close to something
then so is the copy.

Two rules, then. The meander itself — copper where there was none — must clear
every rule outright. The complete replacement must be **no worse** than the
track it replaces: nothing new clashed with, and nothing more tightly.

Clearance is resolved from the net classes at their strictest, and custom
`.kicad_dru` rules are not interpreted. On this board they only relax clearance
inside the BGA fanout, so being stricter costs nothing but a meander that would
have fitted somewhere it does not belong. A rule that tightened clearance could
in principle let something through, which is why the flow ends at KiCad's DRC.

## Verifying against KiCad, and what cannot be compared

`scripts/verify-with-kicad.sh` runs KiCad's DRC on both boards, and reads the net
lengths back out of KiCad. The lengths are the part that compares cleanly: every
net the tool says it lengthened has to have grown by that much according to
pcbnew, and the end-to-end test checks all of them.

**A violation count is not a comparable quantity**, and two separate effects make
it so.

The first is which item KiCad names. It reports one violation per pair of items
and suppresses the rest, so which member of a cluster of equally close things it
blames depends on the order it walked the board. On this board an A10 track sits
0.1000 mm from a GND via both before and after; KiCad named a different, looser
A10 track the first time and quoted 0.1944 mm. Nothing had moved, and measuring
the pristine board directly proves it.

The second is that folding in a meander splits a track. Where the original ran
too close to something -- and this board has 687 such places before the tool
touches it -- three shorter tracks produce up to three entries where one produced
one, at the same distance. That took the count from 687 to 688 while the
offending stretch of DDR_A11 beside DDR_CLK_N got *shorter*, 12.153 mm to
8.853 mm.

So counts are reported and not judged. Only the unconnected count is a failure
condition there, because a net either reaches its pads or it does not.

Whether the board got worse is a geometric question, and two Go tests answer it
exactly:

- `TestNoNetPairEndsUpTooClose` -- for every pair of nets, the closest approach
  between their copper. No pair may end up inside its required clearance, nor may
  a pair that was already too close get tighter. It is not a rule that nothing
  may get closer: moving copper closer while staying inside the clearance is what
  a meander is allowed to do.
- `TestNoNetPairRunsCloserForLonger` -- the total length of one net's copper
  running inside the clearance it shares with another. This catches what the first
  cannot: copper that was already too close being *extended*.

On the demo board: 499 unconnected items before and after, 3480 net pairs with
none inside its clearance, 13062 pairs against 21 changed nets with none running
too close for longer, and KiCad measuring +58.8354 mm added -- matched net by net
against what the tool reported.

## Finding the room on a dense board

The demo board's byte lanes are routed at exactly the minimum DDR clearance: the
nearest neighbour of one DQ24 segment sits 0.2000 mm away against a 0.2 mm rule.

The first version asked for one amplitude that held over a whole track, and on
those tracks it found nothing -- it reported that DQ24 could not be lengthened by
a single micrometre. That was the wrong question. The room beside a track is not
uniform along it: a 20 mm track squeezed by a neighbour for 2 mm of its length
has plenty of space over the other 18, and insisting on one figure throws all of
it away.

So the room is profiled in steps along each track and the best non-overlapping
stretches are taken, scored by how much length each could hold. On byte lane 3
that turns 0.4 mm of apparent capacity into 26.1 mm -- sixty-seven times more --
and DQ24 gets its whole 7.609 mm. Across the board the out-of-tolerance nets go
from 49 mm of usable capacity to 67 mm, and what the tool actually adds went from
36.2 mm to 58.8 mm by KiCad's own measurement.

Profiling costs clearance queries: one bisection per step per side, rather than
one per track. Eight halvings settle the amplitude to about 5 micrometres, far
finer than any decision made from it, and the step is half the shortest
meanderable run.

### All of a track's meanders go in at once

Because one track now offers several stretches, a track is replaced once with
every meander it carries. Replacing it once per stretch does not work: the second
edit holds a pointer to a track that no longer exists, `ReplaceTrack` silently
does nothing, and the length is counted anyway. That is exactly what happened --
DDR_A11 was reported 0.234 mm longer than KiCad could find. `ReplaceTrack` now
returns whether it did anything, so the mistake cannot be made quietly again.

## Telling a shortfall of space from a reroute

Profiling finds the room that exists; it cannot invent room that does not. What
is left over is two completely different problems, and reporting them the same
way leaves the user to guess which they have.

`Headroom` measures how much length the space beside a route could hold.
`NeedsReroute` asks a different question: whether the net is being asked to grow
by more than a quarter of its own length. On the demo board:

| net | needs | room | verdict |
|---|---|---|---|
| DQ24 | 7.609 mm | 23.797 mm | tune it |
| DQ26 | 4.331 mm | 0.777 mm | short of room |
| A11 | 14.251 mm | -- | 14.251 mm on a 16.888 mm route; reroute |

No meander turns a 16.9 mm route into a 31.1 mm one, whatever room is opened
beside it. Twenty of the demo board's thirty-six candidates are that case, which
is why the total shortfall stays large however good the tuning gets: the fly-by
bus is matched against a clock nearly twice the length of its shortest member,
and its second leg is not routed at all.

The room measurement is a request of its own in the web API, because it probes
the design rules along every candidate track -- seconds, where the rest of the
analysis is milliseconds. The user asks for it when they are deciding what to
change.

Summing room across nets would be the obvious mistake here, and it is worth
saying why it is wrong: a lane where one net has 24 mm of room and seven have
none is not nearly covered, however the totals add up. Room beside one net cannot
be lent to another, so what is gettable is each net's own room capped at what it
needs.

## One tool, several interfaces

The engine was always general. `netlen` measures a net, `drc` says whether
copper fits, `tune` folds in length, `preview` draws it -- none of that knows
what DDR is. What was DDR-specific was knowing what to measure against what, and
that is what `proto` supplies: it reads a board, says which interfaces are on
it, and describes each as differential pairs plus groups of nets that match a
reference.

Two questions cover almost everything:

- **Do the two halves of a pair match each other?** This needs no knowledge of
  the protocol beyond "these two are a pair", applies to nearly every fast
  interface on a board, and is where a length matcher earns its keep on USB,
  PCIe and MIPI. PCIe is *only* this: each lane recovers its own clock, so
  lane-to-lane matching is not a requirement the way it is on a
  source-synchronous bus, and the detector deliberately emits no group for it.
- **Do the members of a group match their reference?** The source-synchronous
  question -- data against the clock it travels with. RGMII transmit against
  GTX_CLK and receive against RX_CLK, separately, because each direction is its
  own source-synchronous bus. SD command and data against SDMMC_CK. MIPI lanes
  against the clock lane.

DDR gets no generic group, on purpose. Its members are not one set matched to
one reference -- each byte lane goes to its own strobe, the address bus to the
clock, and both per leg of the fly-by chain -- so a group here would produce the
spread of 71 nets against the longest, which is arithmetic rather than a
requirement. `proto` describes DDR and names the analyser that plans it.

**The tool proposes, the user decides.** Detection is a reading, so every part
of it can be replaced: which family a set of nets belongs to, which net is its
reference, and the two numbers the board cannot supply. `proto.Reassign` applies
a family to any set of nets and is careful about what it then claims -- it looks
for that family's structure in the names first, and where it is not there it
still applies the family's limits but says plainly that there are no groups,
only pairs. A set of three unrelated names gets neither, and says so; naming the
reference is the smallest thing a user can say that makes it actionable.

**Width and pair spacing are measured where the board has them and asked for
where it does not.** Both decide the impedance, and both are on the board once
it is routed -- asking a user to retype what they have drawn is not a question,
it is an insult. So `MeasureGeometry` reads the dominant track width by how much
copper is at each, and the pair spacing as the median over the stretches where
the two halves actually run coupled. Median rather than minimum: a pair necks
down at a via, and the tightest point is not what it was drawn to. The
measurement checks out against the board's own net classes, which is an
independent source it never reads -- USB measures 0.134 mm and the class says
0.134, PCIe 0.2 against 0.2, the expansion pairs 0.105 against 0.105.

**Impedance is estimated and labelled as an estimate.** The models are the
IPC-2141 closed forms: Hammerstad for microstrip, the symmetric-stripline
expression, and the standard coupling terms for an edge-coupled pair. They come
with their validity ranges and say when the geometry is outside them. What they
are for is catching a width nowhere near its target -- the demo board's DDR is
0.09 mm on an outer layer, which comes out at 55 ohms where DDR4 usually wants
40 -- not for holding a board to a few percent, which needs the fabricator's
stackup and a field solver. Where nothing is routed the layer itself is a guess,
and that is said too, because microstrip and stripline differ by far more than
the models' own error.

**Recognition is by net name, and that is stated rather than hidden.** A net
name is what the designer wrote down to say what the net is, and no amount of
geometry will tell you a pair is PCIe rather than SATA. So every interface
carries the evidence it matched on, the web app makes each one a tick box, and
a board that names nothing usefully is reported as having nothing recognisable
rather than being guessed at. Three rules keep the guessing down:

- **A pair needs both halves.** A trailing N is otherwise indistinguishable from
  an active-low suffix, and DDR is full of those -- RESETN, CASN, ACTN. Asking
  for the partner settles it without a list of exceptions: there is no RESETP on
  any board.
- **A bus needs both its halves too.** Four nets named TXD are not RGMII; RGMII
  is transmit *and* receive, each with its own clock.
- **A name has to say what it is.** A bare TX/RX pair could be anything, so
  only nets that say PCIe are claimed as PCIe. The rest are reported as
  differential pairs belonging to no interface this recognises -- which is still
  a real check, because their two halves still have to match.

What this found on the demo board is worth recording: ten interfaces, and only
DDR has a net joined end to end. Ethernet has copper on 2 of 17 nets, the eMMC
on all ten and not one of them joined, MIPI nothing at all. The board is a work
in progress throughout, not only in its DDR.

## Fly-by, and getting its order right

DDR3 and DDR4 address, command, control and clock are fly-by: one net leaves
the controller, reaches the first device, carries on to the next, and ends in a
termination resistor. It is not a style. The topology is what makes write
levelling work -- the controller measures and compensates for the skew the
chain deliberately introduces -- and a T or a star instead puts a stub on every
one of those nets, whose reflections are the thing fly-by exists to avoid. So a
board missing a hop has not routed that bus, however finished the copper on it
looks.

The demo board is exactly that board, and it is worth being precise about it:
every one of its 27 fly-by nets is **three separate islands** -- `U3+U4`, the
termination resistor on its own, and `U5` on its own. The first hop is routed on
all of them; `U4 -> U5` is routed on none. Only the clock pair has its last stub
to the terminator drawn, which is a hop drawn out of sequence rather than
evidence of a different chain. KiCad reports no unconnected item for any of
those nets, because each one is a net with copper on it and connectivity between
the pieces is not what its DRC checks. That is why the report states the chain
and its hops above everything about lengths: what follows is the matching of the
hops that exist, true as far as it goes, and not the board's address timing.

**The order of the chain is a conclusion, and it used to be a guess.** Every
per-hop length is compared over one span, so the order the devices are visited
decides what is compared with what. `Interface.Devices` was documented as
"ordered along the fly-by chain from the controller outward" and was in fact
`sort.Strings` -- alphabetical by reference designator. On the demo board U4 is
both the nearer device and the earlier name, so the guess was right and nothing
noticed. On a board whose near device is U5, or numbered U9 and U10 where "U10"
sorts first, every address length would have been compared over the wrong span.

It now comes from the board, in two stages:

- **Placement.** The devices are sorted by how far their own DDR pads sit from
  the controller's, since a chain runs outward. On the demo board that reads
  U4 at 17 mm and U5 at 32 mm, and the report says so, because it is an
  inference and the reader should be able to disagree with it.
- **The copper, where it exists, which wins.** Not by adjacency -- being joined
  is not being next, and on a finished chain the controller reaches the last
  device as surely as the first, through the ones between. By distance: each hop
  adds length, so the chain visits the devices in increasing order of routed
  length from the controller. Where that disagrees with the placement, the copper
  is followed and the disagreement is reported.

## Where copper may go, and who decides

How much length a net needs is arithmetic and this tool is good at it. Where to
put that length is a judgement about the board, and the tool is not in a
position to make it: room beside a trace can be in a BGA fanout, under a
connector, across a split in a reference plane, or in the one clear patch
somebody was saving. A clearance check sees none of that. It knows whether
copper fits.

So the areas are the user's. `tune.Areas` is a list of rectangles in the board's
own coordinates; empty means anywhere, which is the right default for a first
look and is what the tool did before. Once they are given, nothing is added
outside them -- not a meander, not a bus being spread -- and, just as
importantly, the *room reported* drops to what is usable, so the plan that is
read is the plan that gets applied.

**The areas are proposed, not demanded.** "Draw where copper may go" on a blank
board is a worse question than it looks: the answer depends on where the nets
that need length run and where there is space beside them, and both are things
this has already measured and the user would have to hunt for. Starting from
nothing means drawing a rectangle, being told it holds 0.000 mm, and trying
again. So the same room profile that answers "is this area enough" also answers
"where would you have put it": the stretches where a candidate has room, grown
by the excursion a meander there would make, clustered into rectangles and
ranked by what they are worth. On the demo board that is two regions holding
74.0 mm of the 75.9 mm the whole board offers. Clustering merges until it
settles rather than in one pass -- a cluster grows as it absorbs, so two that
were apart can end up touching, and a single pass would give an answer that
depended on the order the stretches arrived in. Regions worth less than a
hundredth of the requirement are dropped: the demo board proposes one worth
0.158 mm and 0.8 mm across, which nobody is going to draw or adjust.

**The spacing belongs to the area.** It was a global setting with a heuristic --
`-open-clearance`, holding to a wider figure "away from the components", with
the tool deciding what that meant by putting a margin round every courtyard.
Once the user is drawing regions anyway, the heuristic is unnecessary: "keep
0.25 mm apart in here" needs nothing guessed. It only ever tightens, because an
area is somewhere they chose to put copper and asking for more room between
traces there is their call, where undercutting a clearance the board demands
would be something else entirely.

Two more things about it were not obvious.

**An area is a statement about where copper may be, not where centrelines may
be.** The first version checked that each profiled step of a track was inside an
area, and it passed while 26 samples of new copper sat up to 0.05 mm beyond the
edge of the region somebody had drawn. A meander wanders sideways from the track
it is folded into by as much as its amplitude, so the room at each step is now
capped by the distance to the area's edge exactly as it is by a neighbouring
track.

**The conversion happens in one place.** The front end draws on the preview,
whose origin is the bottom-left corner of the board with Y upward; the board
file counts from the top of the page downward. Two implementations of that flip
is one too many and the wrong one is silent -- it mirrors the region, which
still looks like a rectangle. So `preview.Transform` does it in both directions
and the server converts on the way in, once.

What it costs is visible and that is the point: an area over the address bus
alone takes the gettable room from 75.9 mm to 44.3 mm, and applying it adds
35.155 mm where KiCad measures 35.1548.

**And the area answers back.** Drawing a region and being told nothing is only
half a tool: the drawing is asking "will this do?", and that is a measurement
rather than a guess -- about a second, and quicker with an area than without,
because there is less track to profile. So it runs whenever the areas change and
reports how much of what is needed is reachable, per group.

The part worth getting right is what the shortfall is made of. A net merely
short of room might be helped by a bigger area; a net asking for more length
than its own route could carry never will be, because the limit is the track and
not the space beside it. Both numbers are therefore shortfalls, and
have + reachable + unreachable comes to the requirement exactly -- which an
earlier version did not: it put a shortfall and a total need in the same
sentence, "40 mm on nets short of room, 192 mm on nets needing a reroute"
against a total shortfall of 191, and a reader trying to check it could not.
The arithmetic is a function with the invariant as its test.

## Buses, and the room they have to spare

Where a net is short of room, the question is whether the bus it runs in could be
spread out to make some. `FindBundles` recognises a bus -- parallel tracks of
different nets, side by side over a shared stretch -- and measures the clear space
beyond its outer edges.

That measurement is reported, and it is what a decision to re-space would rest
on. Byte lane 2 needs 2.0 mm, has no room beside its tracks, and runs in buses
with 9.24 mm of clear space next to them: re-spacing would fix it. The address and
command group has 1.19 mm of bus spare against a 206 mm requirement: it would
not.

## Spreading a bus

Re-spacing is `-expand`, and it is opt-in. Each trace steps sideways at 45
degrees inside its own extent, runs along its new line, and steps back: the
endpoints do not move, nothing is rerouted, and the traces keep their order so
none can cross another.

Three things about it are not obvious, and each was found by measuring rather
than by reasoning.

**The steps have to be staggered along the bus.** Two parallel tracks stepping
side by side over the same stretch are not the pitch apart any more: two
parallel 45 degree lines whose cross-bus gap is *g* sit only *g*/√2 apart, 29%
closer. On a bus already at its minimum clearance that is a violation, so a
design where every trace steps at once can never move more than one trace --
which is exactly what the clearance check reported before the stagger existed.
Staggered, each trace steps past the steps of everything outside it, so every
step happens beside a neighbour running straight and the closest the two ever
come is their final gap, which is wider than the pitch they started at.

**Moving fewer traces is usually better than moving more.** A gap holds no
meander at all until it is wide enough for a mitred excursion -- a threshold,
not a taper -- so several gaps below it are worth nothing where one above it is
worth a lot. And every trace that moves has its own straight run cut into, by
its steps and by the stagger of everything outside it. On byte lane 3, spreading
the travel across three traces opened three unusable gaps and cost more run
length than the steps added; moving only the outermost opened 0.77 mm beside its
neighbour and left every other trace whole. So the allocation searches over how
many traces to move as well as how far, scored by the length each candidate
would actually deliver.

**The figure worth reporting is the marginal one, and the trace it lands on is
usually one that did not move.** Room that opens beyond the outermost trace was
already there, and length a trace could have folded in anyway is not something
the spread delivered; scored against what each bus could already do, the address
and command bus's reported 2.486 mm becomes 0.086 mm, and skipping that bus
entirely changes the end result by nothing at all. The other half of the same
accounting is counting the traces that stayed put: the one that gains most from
a spread is the trace whose neighbour moved away from it, and leaving those out
reported 0.000 mm for a spread that finished a net.

**Traces finished, not millimetres.** Matching is pass or fail per trace, and
millimetres cannot express that: 1.3 mm on one trace and 0.65 mm on each of two
score the same and are not worth the same. So candidates are compared on how
many traces the spread would bring inside tolerance first, and on length only to
break the tie. On byte lane 3 that is the difference between four nets matched
and three, at a cost of 0.4 mm of total added length spread across nets that
stay far short either way.

What it is worth on this board: two of twelve buses, 0.69 mm of length from the
steps and 0.57 mm of room opened between traces, taking the total added from
58.835 mm to 61.093 mm and one more net inside tolerance. The estimate is worth
trusting -- the report's 2.258 mm for byte lane 3 is exactly what the run
delivered. Where it cannot help it says why, and the reason is usually specific
enough to act on: byte lane 2 has 5 mm of clear space, and it is behind DQ19,
the one trace on the lane that needs no length. Moving that would lengthen it
and raise the target for the whole lane, so reaching that space is a rerouting
decision rather than a re-spacing one.

## Clearance in the open

A fine-pitch BGA is the hardest part of a board to route, so the clearance on
the netlist is usually whatever got the escape out -- 0.1 mm is common -- and
every other rule on the board inherits it. The demo board is exactly this: its
board minimum and its `DDR_DIFF` class are both 0.1 mm. Between the components
there is normally far more room, and the signals are better off using it.

`-open-clearance` (0.2 mm by default) is a floor that applies away from the
components and nowhere else. "Around a component" is the area its own pads
occupy, grown by `-open-margin`; under a BGA that is the whole ball field plus
the escape. Inside it the board's own rules stand, because that is where the
tight figure was chosen deliberately. A differential pair is exempt in both
directions: its two halves are meant to run close, at the gap the impedance was
drawn for.

It lives in the clearance checker rather than in the writer, so it governs every
question asked of the geometry -- how much room there is beside a trace, how far
a bus could be spread, whether a meander fits -- and not only the copper written
out. It only ever tightens: a board whose own rule is stricter keeps it.

## The board's own design rules

A `.kicad_dru` is a small language and most of it is about things this tool has
no opinion on. What matters is the one kind of rule that changes where copper
may go: a clearance constraint with a condition.

For a long time these were reported as not interpreted, and the tool worked to
the net classes at their strictest. That is the right default and it was free
while the tool only lengthened existing traces: being stricter than the board
means declining to put a meander somewhere it would in fact have fitted.

It stopped being free the moment the tool could create copper. The demo board's
rules allow 0.1 mm inside the BGA courtyards where the DDR class asks 0.2, and
**a ball cannot be escaped at 0.2 mm** — the channel between two of them is not
wide enough. Run against the real corridor, the router refused 50 of 52
connections with "no way through: every path between these pads is blocked by
copper that is already there". It was not blocked by copper. It was blocked by a
rule nobody had read.

Two things had to change, and the second is the subtler one.

**The rules themselves.** A narrow recursive-descent parser for the condition
language: `&&`, `||`, `!`, brackets, `intersectsCourtyard`, `memberOfFootprint`,
and equality against a string. Anything else fails the whole condition and the
rule is reported as not applied. Reading a rule that *relaxes* a clearance is a
different risk from ignoring one — get it wrong and the tool produces copper
KiCad will reject, which is the one thing it must not do — so three guards, all
stricter than KiCad and all in the direction that declines rather than allows:
only fully understood conditions; the condition must hold for **both** pieces of
copper where KiCad asks about one; and "inside a courtyard" means wholly inside
where KiCad says intersects.

**The grid the router searches.** It stepped at the net class's clearance, so
even with the rules read there was no cell in the escape channel between two
balls: the router would still have reported no way out of a part the board
escapes perfectly well. It now steps at the tightest clearance any applied rule
permits. A finer grid loosens nothing — every path is still checked against the
real rules in the shape the copper will actually have.

And a prerequisite neither of those would have worked without: **courtyards were
only read from polygons**. On the demo board the library parts draw them as 317
rectangles, 860 lines, 89 circles and 6 polygons, so `Footprint.Courtyard` was
empty on every part that mattered and every courtyard condition was silently
false. Lines and circles become the bounding box of the set, which is the
conservative answer to "is this copper inside the part" — it can only say yes
where the true outline would say no.

## Drawing the board

The tool's output is copper, and nothing in a table shows copper. So the board
is served as geometry a browser can draw: a JSON description and a flat buffer
of triangles, in the EMI Analyzer's `board.json` + `geometry.bin` format.

Matching that format rather than inventing one is the whole point. The analyzer
has a WebGL viewer for exactly this -- layers, nets, pan, zoom, highlight -- and
it is framework-free, tested against real boards, and fast on dense ones. What
was missing was a producer: that geometry comes out of the analyzer's Python
worker, and this tool has no worker. `preview.Build` is that producer, and with
it the viewer is reused as it stands, linked in by path under the same `@emi`
alias embeddedci-server already uses.

The contract in one paragraph: coordinates are millimetres in board space,
origin at the bottom-left of the board extent, Y up -- KiCad's page coordinates
are Y down, and the flip happens once, here. `geometry.bin` is little-endian
float32 x,y pairs, three vertices to a triangle. Every triangle belongs to one
(layer, net) group, and the groups index the buffer by vertex offset and count,
which is what lets the viewer hide a layer or highlight a net by changing a draw
range instead of re-uploading two megabytes.

Two things about the geometry are worth knowing.

**Almost nothing needs a triangulation algorithm.** Every copper item except a
zone fill is convex -- a track is a capsule, a via is a disc, and KiCad's pad
shapes are rectangles, ovals, trapezoids and discs with an optional corner
radius -- and a convex outline fans from any one of its own vertices. Only the
pours are ear-clipped, and that is checked by area rather than by finishing the
loop: ear clipping is correct only on a simple polygon and does not notice when
it was handed something else, so a bow tie yields overlapping triangles and
looks like a success. The triangles have to come to the area the ring encloses.

**Resolution was measured, not guessed.** Arcs are flattened to a chord
tolerance of 5 micrometres. At 20 micrometres a via reads as a visible decagon
once a meander fills the view; at 5 it is an 18-gon and within 0.7% of its true
area, and the whole demo board still comes to two megabytes -- 85,000 triangles,
built in about 150 ms. Which is why it is built per request and not cached:
there is no cache state to get wrong and nothing the user could feel.

The demo board also settles a question the format does not: 557 of its pads
belong to parts parked outside the edge cut, waiting to be placed, while every
one of its 3120 tracks is inside the outline. The view frames the board, so
those parts sit off screen -- and the document says so, because a missing
decoupling capacitor otherwise reads as a hole in the drawing.

## The web front end

The control plane is a package, not a service. There is no worker and no queue,
because the engine is Go: reading a 2.4 MB board and measuring its whole DDR
interface takes about 150 ms, so the report comes back from the upload itself
rather than from polling something. That does mean board bytes pass through the
package, which is why there is an upload limit and why sessions expire.

Everything the host supplies is an interface in `server/deps.go`. The one that
matters is `Store`: a session has to outlive a single request, because the user
uploads, reads, chooses parameters, and only then applies. An in-memory map is
correct for the dev harness and wrong behind a load balancer, so the host owns
that decision.

Two things in there are correctness rather than plumbing.

**The net classes travel with the session.** They come from the uploaded
`.kicad_pro`, and if they were dropped between the report and the apply, every
clearance would fall back to the board minimum — the meanders written would be
checked against looser rules than the ones the user was shown. So `Session`
carries the whole `board.Project`, which is why that type has JSON tags and
builds its class lookup lazily.

**A session id is not an access token.** Every read also checks the owner, and
another user's session reports as missing rather than forbidden so that ids
cannot be probed. An anonymous request is refused outright: a session holds
somebody's board, and without an identity there is nothing to file it under.

### An apply that changes nothing has no download

On a dense board a whole selection can turn out to be unfittable. Handing the
untouched upload back as a result would produce a perfectly valid board file,
which is exactly the danger — it would be indistinguishable from a corrected
one. So nothing is stored, the download stays a 404, and the response says so.

### Which tracks a meander may go on

This is the bug the front end found, and it was in the engine rather than the
UI.

A net can have copper that no route runs over: a dangling stub, or a whole
island stranded because a leg was never finished. The clock pair on the demo
board has exactly that shape — the routed leg to the near device, plus a piece
at its termination resistor that nothing reaches. The tuner was considering
every track on the net, so it folded a meander into the stranded island and
reported 2.4 mm added to `DDR_CLK_N` while the leg being matched did not move at
all.

So `netlen` now records which tracks each route runs over, the plan carries them
per member, and `tune.Tune` takes them as an argument rather than an option. A
caller that wants the whole net has to pass nil deliberately. Across the whole
demo board this removed about 9 mm of length that was being added where it did
nothing: the CLI's total went from 45.4 mm over 16 nets to 36.2 mm over 13, and
KiCad agrees with the new figure to four decimal places.

## Milestones after this

In the order they were asked for, and not started:

1. **Plan and tune the other interfaces.** They are detected, measured and
   listed, and the user picks which to work on. What is not wired yet is
   turning a `proto.Group` into the same plan the DDR analyser produces, so
   that picking Ethernet lengthens Ethernet. The generic half of `ddr.Group`,
   `Member` and `finishGroup` is the seam.
2. **More of the `.kicad_dru`.** The clearance rules are read; `insideArea` and
   the rest of the condition language are not, and a rule using them is named
   in the report rather than guessed at.

Creating traces is built (`route`, opt-in) and parked: DDR is the hardest
interface to finish and the board is better judged once more of it is routed.

The no-room problem, which used to be listed here, is answered: the room is
profiled in steps along each track rather than asked for as one amplitude, and
where a bus is too tight for that it can be spread sideways with `-expand`.
