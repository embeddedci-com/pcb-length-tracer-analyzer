package tune

import (
	"math"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Situations for bus expansion.
//
// Expanding a bus is the one operation here that moves copper the user did not
// select, so what it must never do matters as much as what it does: no
// endpoint moves, no trace crosses another, nothing ends up inside a
// clearance, and a trace gains exactly the length its two steps account for.
// Each test below is one of those, or one of the reasons a bus is refused.

// busDrawn is `bus`, but with control over which way round each track is
// drawn. Half the tracks in a real bus run the other way, and the waypoints of
// a step-aside have to come out in the track's own direction: emitting them in
// the bus direction doubles the track back along its whole length, which on
// the demo board added 4.44 mm where 0.41 mm was intended.
func busDrawn(s *synth, n int, pitch, length float64, reversed func(int) bool) []string {
	var nets []string
	for i := range n {
		net := string(rune('A' + i))
		y := 50 + float64(i)*pitch
		a := geom.Pt{X: 10, Y: y}
		b := geom.Pt{X: 10 + length, Y: y}
		s.pad(net, a, synthW)
		s.pad(net, b, synthW)
		if reversed != nil && reversed(i) {
			a, b = b, a
		}
		s.track(net, "F.Cu", a, b, synthW)
		nets = append(nets, net)
	}
	return nets
}

// copperOf is the total length of a net's copper.
func copperOf(b *board.Board, net string) float64 {
	total := 0.0
	for _, t := range b.TracksOfNet(net) {
		total += t.Length(b.Stackup)
	}
	return total
}

// endsOf is the set of points where a net's copper stops -- points touched by
// exactly one track. Expansion must leave these alone, because they are what
// the net is joined to the rest of the board by.
func endsOf(b *board.Board, net string) []geom.Pt {
	count := map[geom.Pt]int{}
	for _, t := range b.TracksOfNet(net) {
		count[t.Start.Snap()]++
		count[t.End.Snap()]++
	}
	var out []geom.Pt
	for p, n := range count {
		if n == 1 {
			out = append(out, p)
		}
	}
	return out
}

func hasPoint(pts []geom.Pt, p geom.Pt) bool {
	for _, q := range pts {
		if q.Dist(p) < 1e-6 {
			return true
		}
	}
	return false
}

// closestBetweenNets is the smallest copper-to-copper distance between any two
// tracks of different nets on the same layer.
func closestBetweenNets(b *board.Board, err float64) float64 {
	best := math.Inf(1)
	ts := b.Tracks
	for i, x := range ts {
		for _, y := range ts[i+1:] {
			if x.Net == y.Net || x.Layer != y.Layer {
				continue
			}
			for _, sx := range x.Shape(err) {
				for _, sy := range y.Shape(err) {
					if d := sx.Dist(sy); d < best {
						best = d
					}
				}
			}
		}
	}
	return best
}

// needAll gives every net the same requirement.
func needAll(nets []string, mm float64) map[string]float64 {
	out := map[string]float64{}
	for _, n := range nets {
		out[n] = mm
	}
	return out
}

func TestSpreadingABusOpensRoomBetweenItsTraces(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	b, _, tu := s.build(t, synthClr, synthW)

	// At a minimum-clearance pitch there is nowhere for an inner trace to fold
	// a meander, which is the whole problem expansion exists for.
	if runs := tu.findRuns("B", synthW, nil); len(runs) != 0 {
		t.Fatalf("an inner trace of a tight bus already has %d stretches of room", len(runs))
	}

	before := map[string]float64{}
	ends := map[string][]geom.Pt{}
	for _, n := range nets {
		before[n] = copperOf(b, n)
		ends[n] = endsOf(b, n)
	}

	res, err := tu.Expand(nets, needAll(nets, 1.0), DefaultExpandOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("%d results for one bus", len(res))
	}
	r := res[0]
	if r.Skipped != "" {
		t.Fatalf("a bus with 5 mm of clear space beside it was refused: %s", r.Skipped)
	}
	if r.OpenedMM <= 0 {
		t.Errorf("the spread opened %.3f mm of room", r.OpenedMM)
	}

	// The traces move further the further out they are, which is what opens a
	// gap between each one and the next.
	disp := map[string]float64{}
	for _, m := range r.Members {
		disp[m.Net] = m.DisplacementMM
	}
	last := 0.0
	for _, n := range nets {
		if disp[n] < last-1e-9 {
			t.Errorf("%s moved %.3f after %s moved %.3f: the traces are being squeezed together, not spread",
				n, disp[n], nets[0], last)
		}
		last = disp[n]
	}
	if disp[nets[4]] <= disp[nets[1]] {
		t.Errorf("the outermost trace moved %.3f and an inner one %.3f", disp[nets[4]], disp[nets[1]])
	}

	// And now there is room where there was none.
	if runs := tu.findRuns("B", synthW, nil); len(runs) == 0 {
		t.Error("after spreading the bus an inner trace still has no room beside it")
	}

	// Nothing was disconnected: each net still stops where it stopped.
	for _, n := range nets {
		after := endsOf(b, n)
		for _, p := range ends[n] {
			if !hasPoint(after, p) {
				t.Errorf("%s no longer ends at %v, so whatever it joined there is disconnected", n, p)
			}
		}
	}

	// Nothing ended up too close to anything.
	if d := closestBetweenNets(b, b.MaxError); d < synthClr-1e-6 {
		t.Errorf("two nets ended up %.4f mm apart, inside the %.3f mm clearance", d, synthClr)
	}
}

func TestStepsAddExactlyTheLengthTheyAccountFor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reversed func(int) bool
	}{
		{"drawn along the bus", nil},
		{"drawn against the bus", func(int) bool { return true }},
		{"drawn alternately", func(i int) bool { return i%2 == 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSynth()
			nets := busDrawn(s, 5, synthClr+synthW, 20, tc.reversed)
			b, _, tu := s.build(t, synthClr, synthW)

			before := map[string]float64{}
			for _, n := range nets {
				before[n] = copperOf(b, n)
			}
			res, err := tu.Expand(nets, needAll(nets, 1.0), DefaultExpandOptions())
			if err != nil {
				t.Fatal(err)
			}
			if len(res) != 1 || res[0].Skipped != "" {
				t.Fatalf("the bus was not spread: %+v", res)
			}
			for _, m := range res[0].Members {
				want := m.DisplacementMM * rampGainPerMM
				got := copperOf(b, m.Net) - before[m.Net]
				// A micrometre of slack for the nanometre grid the waypoints
				// are snapped onto.
				if math.Abs(got-want) > 1e-3 {
					t.Errorf("%s moved %.4f mm aside and grew %.4f mm; two 45 degree steps account for %.4f mm",
						m.Net, m.DisplacementMM, got, want)
				}
				if math.Abs(m.AddedMM-want) > 1e-3 {
					t.Errorf("%s reported %.4f mm added, drew %.4f mm", m.Net, m.AddedMM, want)
				}
			}
		})
	}
}

func TestABusWithNothingBesideItIsLeftAlone(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	// A wall hard against each side, at exactly the clearance.
	pitch := synthClr + synthW
	s.track("WALL_LO", "F.Cu", geom.Pt{X: 8, Y: 50 - pitch}, geom.Pt{X: 32, Y: 50 - pitch}, synthW)
	s.track("WALL_HI", "F.Cu", geom.Pt{X: 8, Y: 50 + 5*pitch}, geom.Pt{X: 32, Y: 50 + 5*pitch}, synthW)
	_, _, tu := s.build(t, synthClr, synthW)

	res, err := tu.Expand(nets, needAll(nets, 1.0), DefaultExpandOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("%d results for one bus", len(res))
	}
	if !strings.Contains(res[0].Skipped, "no clear space") {
		t.Errorf("a walled-in bus was refused with %q", res[0].Skipped)
	}
	if len(res[0].Members) != 0 {
		t.Errorf("%d traces moved in a bus that was refused", len(res[0].Members))
	}
}

func TestSpaceBehindATraceThatNeedsNothingIsReportedNotTaken(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	// Closed on the low side, open on the high side, so the outermost trace
	// on the side with the room is the last one.
	pitch := synthClr + synthW
	s.track("WALL_LO", "F.Cu", geom.Pt{X: 8, Y: 50 - pitch}, geom.Pt{X: 32, Y: 50 - pitch}, synthW)
	b, _, tu := s.build(t, synthClr, synthW)

	need := needAll(nets, 1.0)
	need[nets[4]] = 0 // already at its group's target

	before := copperOf(b, nets[4])
	res, err := tu.Expand(nets, need, DefaultExpandOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("%d results for one bus", len(res))
	}
	// The room is real and unreachable, and the reason is a specific trace, so
	// the report has to name it: reaching that space is a rerouting decision
	// for whoever drew the board.
	if !strings.Contains(res[0].Skipped, nets[4]) {
		t.Errorf("the trace blocking the space was not named: %q", res[0].Skipped)
	}
	if !strings.Contains(res[0].Skipped, "needs no length") {
		t.Errorf("unexpected reason: %q", res[0].Skipped)
	}
	if got := copperOf(b, nets[4]); math.Abs(got-before) > 1e-9 {
		t.Errorf("a trace that needed no length grew by %.4f mm, which raises the target for its whole group", got-before)
	}
}

func TestADifferentialPairIsMovedAsOneUnit(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	b, _, tu := s.build(t, synthClr, synthW)

	gapBefore := math.Abs(b.TracksOfNet("C")[0].Start.Y - b.TracksOfNet("D")[0].Start.Y)

	opt := DefaultExpandOptions()
	opt.Coupled = map[string]string{"C": "D", "D": "C"}
	res, err := tu.Expand(nets, needAll(nets, 1.0), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Skipped != "" {
		t.Fatalf("the bus was not spread: %+v", res)
	}
	disp := map[string]float64{}
	for _, m := range res[0].Members {
		disp[m.Net] = m.DisplacementMM
	}
	if math.Abs(disp["C"]-disp["D"]) > 1e-9 {
		t.Errorf("the halves of a pair moved %.4f and %.4f: the pair's spacing is what it exists for",
			disp["C"], disp["D"])
	}
	// And the spacing they are coupled at is unchanged along the moved stretch.
	gapAfter := math.Inf(1)
	for _, x := range b.TracksOfNet("C") {
		for _, y := range b.TracksOfNet("D") {
			if math.Abs(x.Start.X-y.Start.X) < 1e-6 && math.Abs(x.End.X-y.End.X) < 1e-6 {
				gapAfter = math.Min(gapAfter, math.Abs(x.Start.Y-y.Start.Y))
			}
		}
	}
	if math.Abs(gapAfter-gapBefore) > 1e-6 {
		t.Errorf("the pair's spacing went from %.4f to %.4f", gapBefore, gapAfter)
	}
}

func TestAPairSplitApartInTheBusStopsTheSpread(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	_, _, tu := s.build(t, synthClr, synthW)

	opt := DefaultExpandOptions()
	// B and D are a pair with C between them: moving either half alone spoils
	// the coupling, and moving all three as one is not a spread.
	opt.Coupled = map[string]string{"B": "D", "D": "B"}
	res, err := tu.Expand(nets, needAll(nets, 1.0), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("%d results for one bus", len(res))
	}
	if !strings.Contains(res[0].Skipped, "not neighbors") {
		t.Errorf("a split pair was refused with %q", res[0].Skipped)
	}
}

func TestANetWithTwoTracksInTheBusIsNotMoved(t *testing.T) {
	s := newSynth()
	var nets []string
	pitch := synthClr + synthW
	for i := range 5 {
		net := string(rune('A' + i))
		y := 50 + float64(i)*pitch
		s.pad(net, geom.Pt{X: 10, Y: y}, synthW)
		s.pad(net, geom.Pt{X: 30, Y: y}, synthW)
		s.track(net, "F.Cu", geom.Pt{X: 10, Y: y}, geom.Pt{X: 30, Y: y}, synthW)
		nets = append(nets, net)
	}
	// C comes back through the same bus, so it has two tracks in it. Which one
	// to move, and whether moving one leaves the other inside a clearance, is
	// not worth guessing at.
	s.track("C", "F.Cu", geom.Pt{X: 10, Y: 50 + 5*pitch}, geom.Pt{X: 30, Y: 50 + 5*pitch}, synthW)
	b, _, tu := s.build(t, synthClr, synthW)

	before := copperOf(b, "C")
	res, err := tu.Expand(nets, needAll(nets, 1.0), DefaultExpandOptions())
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, r := range res {
		if strings.Contains(r.Skipped, "two tracks") {
			saw = true
		}
	}
	if !saw {
		for _, r := range res {
			t.Logf("bus %s span %.2f: skipped=%q members=%d", r.Layer, r.SpanMM, r.Skipped, len(r.Members))
		}
		t.Error("a bus containing the same net twice was spread anyway")
	}
	if got := copperOf(b, "C"); math.Abs(got-before) > 1e-9 {
		t.Errorf("the doubled net grew by %.4f mm", got-before)
	}
}

func TestABusWhoseTracesBarelyNeedAnythingIsLeftAlone(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	b, _, tu := s.build(t, synthClr, synthW)

	before := map[string]float64{}
	for _, n := range nets {
		before[n] = copperOf(b, n)
	}
	// Ten micrometres each. Disturbing somebody's layout for that is not a
	// trade worth making.
	res, err := tu.Expand(nets, needAll(nets, 0.01), DefaultExpandOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("%d results for one bus", len(res))
	}
	if res[0].Skipped == "" {
		t.Errorf("a bus needing 0.01 mm per trace was spread anyway: opened %.4f", res[0].OpenedMM)
	}
	for _, n := range nets {
		if got := copperOf(b, n); math.Abs(got-before[n]) > 1e-9 {
			t.Errorf("%s grew %.4f mm in a bus that was refused", n, got-before[n])
		}
	}
}

func TestSpreadingIsCappedByTheRequirementAndByTheOption(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	_, _, tu := s.build(t, synthClr, synthW)

	// A trace's travel is capped by its own requirement, because the length the
	// steps add counts against it: overshooting makes it the longest in its
	// group and raises the target for everything else.
	res, err := tu.Expand(nets, needAll(nets, 0.4), DefaultExpandOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Skipped != "" {
		t.Fatalf("the bus was not spread: %+v", res)
	}
	for _, m := range res[0].Members {
		if got := m.DisplacementMM * rampGainPerMM; got > 0.4+1e-6 {
			t.Errorf("%s moved far enough to add %.4f mm, more than the %.3f mm it needed", m.Net, got, 0.4)
		}
	}

	// And by the option, whatever the requirement.
	s2 := newSynth()
	nets2 := bus(s2, 5, synthClr+synthW, 20)
	_, _, tu2 := s2.build(t, synthClr, synthW)
	opt := DefaultExpandOptions()
	opt.MaxDisplacement = 0.25
	res2, err := tu2.Expand(nets2, needAll(nets2, 50), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2) != 1 || res2[0].Skipped != "" {
		t.Fatalf("the bus was not spread: %+v", res2)
	}
	for _, m := range res2[0].Members {
		if m.DisplacementMM > 0.25+1e-9 {
			t.Errorf("%s moved %.4f mm with a 0.25 mm cap", m.Net, m.DisplacementMM)
		}
	}
}
