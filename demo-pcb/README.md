# Boards to test against

Two of them, and the difference between them is the point.

| Directory | The same board, at | DDR nets joined end to end | Fly-by |
| --- | --- | --- | --- |
| `demo-pcb/` | part way through layout | 44 of 71 | no net makes the second hop |
| `demo-pcb-2/` | further on | 69 of 71 | chain complete on 25 of 26 |

The end-to-end checks run over both (`boards` in `integration_test.go`), because
a tool that behaves on one and not the other has been tested on one. The first
is what the tool says about unfinished work; the second is the first board on
which `address/command U4->U5` -- the second fly-by leg -- exists as real copper
to match, which is why tuning gains 116.8 mm there against 61.1 mm here.

`ai-vision.kicad_pcb` is the pinned fixture. The tests do not merely run against
it, they encode facts about it that were expensive to establish: that its
address bus has 27 nets of which none makes the second fly-by hop, that KiCad
reports 687 violations and 499 unconnected items before the tool touches it,
that its DQS3 pair is 1.479 mm out. Those numbers are the regression test.

**So do not replace it.** Add a board alongside it instead.

## Running against another board

Everything takes a path:

```bash
make report BOARD=demo-pcb/your-board.kicad_pcb
make tune   BOARD=demo-pcb/your-board.kicad_pcb
```

or upload it in the web front end, which needs no setup at all:

```bash
make serve
```

The `.kicad_pro` should travel with the board wherever it goes. Without it every
clearance falls back to the board-wide minimum rather than the net classes the
designer set, and the tool says so rather than quietly working to looser rules.
A `.kicad_dru` beside it is noticed too, and reported as not interpreted.

## What a third board would be good for

`demo-pcb-2` covered the gap that mattered most: a fly-by chain routed far
enough to match per leg. Two things are still untested on real copper.

A board **fully wired** -- every DDR net joined, and the other interfaces too --
would let the result be judged the way a layout engineer would judge it, with
KiCad's unconnected count at zero before and after. Everything the tool reports
today is read against a board that is partly holes, and "499 unconnected" is a
number the user has to hold in their head while reading every other one.

A board with **no DDR at all** is also worth having, and is not a special case:
the tool reads every interface it recognises on any board, and only the DDR half
is skipped. Ethernet, USB and MIPI are detected and measured on both fixtures
here, but neither board has one of them far enough along to be lengthened.

Add either alongside, under `demo-pcb-3/`; the fixture rules in `.gitignore`
already match `demo-pcb*/`, so only the design files are committed, not KiCad's
101 MB of vendored libraries or its autosave archives.
