package tune

import (
	"fmt"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// A synthetic board, for testing the room-finding and meander placement against
// geometry chosen on purpose.
//
// The demo board is the right fixture for "does this work on a real design",
// and it is used for that throughout. It is the wrong fixture for "what does
// this do when the room runs out halfway along a track", because a real board
// does not come with a knob for that. These boards are tiny and every obstacle
// in them is there to answer one question.
type synth struct {
	items []string
	pads  []string
	uid   int
}

func newSynth() *synth { return &synth{} }

func (s *synth) next() string {
	s.uid++
	return fmt.Sprintf("%08x-0000-4000-8000-000000000001", s.uid)
}

// track adds a segment and returns its uuid.
func (s *synth) track(net, layer string, a, b geom.Pt, width float64) string {
	id := s.next()
	s.items = append(s.items, fmt.Sprintf(`	(segment
		(start %g %g)
		(end %g %g)
		(width %g)
		(layer "%s")
		(net "%s")
		(uuid "%s")
	)`, a.X, a.Y, b.X, b.Y, width, layer, net, id))
	return id
}

// pad adds a one-pad footprint, so a net can have endpoints.
//
// Tests size these the same as the track that lands on them. A pad wider than
// its track is realistic but it changes what is being measured: at a
// minimum-clearance pitch a wide pad is already inside its neighbour's
// clearance before the tuner does anything, and the test then fails on the
// fixture rather than on the code.
func (s *synth) pad(net string, at geom.Pt, size float64) {
	s.pads = append(s.pads, fmt.Sprintf(`	(footprint "t:p"
		(layer "F.Cu")
		(uuid "%s")
		(at %g %g)
		(attr smd)
		(property "Reference" "P%d" (at 0 0) (layer "F.SilkS") (uuid "%s") (effects (font (size 1 1) (thickness 0.15))))
		(pad "1" smd circle
			(at 0 0)
			(size %g %g)
			(layers "F.Cu" "F.Mask" "F.Paste")
			(net "%s")
			(uuid "%s")
		)
	)`, s.next(), at.X, at.Y, len(s.pads)+1, s.next(), size, size, net, s.next()))
}

// bytes renders the board file.
func (s *synth) bytes() []byte {
	return []byte(`(kicad_pcb
	(version 20260206)
	(generator "pcb-trace-length-analyzer-test")
	(generator_version "10.0")
	(general
		(thickness 1.6)
		(legacy_teardrops no)
	)
	(paper "A4")
	(layers
		(0 "F.Cu" signal)
		(2 "B.Cu" signal)
		(1 "F.Mask" user)
		(3 "B.Mask" user)
		(5 "F.SilkS" user "F.Silkscreen")
		(13 "F.Paste" user)
		(15 "B.Paste" user)
		(25 "Edge.Cuts" user)
	)
	(setup
		(stackup
			(layer "F.Cu"
				(type "copper")
				(thickness 0.035)
			)
			(layer "dielectric 1"
				(type "core")
				(thickness 1.53)
				(material "FR4")
				(epsilon_r 4.5)
				(loss_tangent 0.02)
			)
			(layer "B.Cu"
				(type "copper")
				(thickness 0.035)
			)
			(copper_finish "None")
			(dielectric_constraints no)
		)
		(pad_to_mask_clearance 0)
	)
	(gr_rect
		(start 0 0)
		(end 100 100)
		(stroke (width 0.05) (type default))
		(fill none)
		(layer "Edge.Cuts")
		(uuid "ffffffff-0000-4000-8000-000000000009")
	)
` + strings.Join(s.pads, "\n") + "\n" + strings.Join(s.items, "\n") + "\n)\n")
}

// build parses the board and returns a tuner for it.
//
// The project is built in code rather than parsed, so a test states the
// clearance it means instead of encoding it in JSON.
func (s *synth) build(t *testing.T, clearance, trackWidth float64) (*board.Board, *board.Project, *Tuner) {
	t.Helper()
	b, err := board.Parse(s.bytes(), "synth.kicad_pcb")
	if err != nil {
		t.Fatalf("the synthetic board does not parse: %v", err)
	}
	proj := board.Defaults()
	proj.MinClearance = clearance
	proj.MinTrackWidth = trackWidth
	proj.EdgeClearance = clearance
	proj.Classes = []board.NetClass{{Name: "Default", Clearance: clearance, TrackWidth: trackWidth}}
	b.UseHeightForLength = proj.UseHeightForLength
	return b, proj, NewTuner(b, proj, DefaultStyle())
}

// ---- the situations ----

const (
	synthW   = 0.09 // track width
	synthClr = 0.2  // clearance
)

// straightTrack lays one horizontal track of the given length on F.Cu at y=50,
// with a pad at each end, and returns the board.
func straightTrack(length float64) (*synth, string) {
	s := newSynth()
	const net = "N1"
	s.pad(net, geom.Pt{X: 10, Y: 50}, synthW)
	s.pad(net, geom.Pt{X: 10 + length, Y: 50}, synthW)
	s.track(net, "F.Cu", geom.Pt{X: 10, Y: 50}, geom.Pt{X: 10 + length, Y: 50}, synthW)
	return s, net
}

// blocker puts a long track of another net parallel to the victim, `gap` of
// copper away, covering the axial interval [x0, x1].
func (s *synth) blocker(name string, x0, x1, gap float64) {
	y := 50 + gap + synthW
	s.track(name, "F.Cu", geom.Pt{X: x0, Y: y}, geom.Pt{X: x1, Y: y}, synthW)
}

func TestRoomWithNothingInTheWay(t *testing.T) {
	s, net := straightTrack(20)
	_, _, tu := s.build(t, synthClr, synthW)

	runs := tu.findRuns(net, synthW, nil)
	if len(runs) == 0 {
		t.Fatal("an empty board should offer room")
	}
	// One stretch covering nearly the whole track, at the style's maximum.
	best := runs[0]
	if best.amp < tu.style.MaxAmplitude-1e-9 {
		t.Errorf("amplitude %.3f, want the style maximum %.3f", best.amp, tu.style.MaxAmplitude)
	}
	if best.length < 18 {
		t.Errorf("usable stretch %.3f mm of a 20 mm track", best.length)
	}

	// And the requirement is met exactly.
	res, err := tu.Tune(net, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A micrometre of slack: the meander's points are snapped to KiCad's
	// nanometre grid, so the length drawn is the length in the file rather than
	// the length the arithmetic asked for.
	if res.Shortfall > 1e-3 {
		t.Errorf("5 mm on an empty 20 mm track fell short by %.6f", res.Shortfall)
	}
}

func TestRoomWithNeighboursOnBothSidesAtMinimumClearance(t *testing.T) {
	s, net := straightTrack(20)
	// Exactly the clearance away on both sides, the whole length: the case that
	// defeats per-track tuning on a real bus.
	s.blocker("L", 5, 35, synthClr)
	s.track("R", "F.Cu", geom.Pt{X: 5, Y: 50 - synthClr - synthW}, geom.Pt{X: 35, Y: 50 - synthClr - synthW}, synthW)
	_, _, tu := s.build(t, synthClr, synthW)

	if runs := tu.findRuns(net, synthW, nil); len(runs) != 0 {
		t.Errorf("found %d stretches beside a track boxed in at minimum clearance", len(runs))
	}
	res, err := tu.Tune(net, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Shortfall != 5 {
		t.Errorf("got %+v, want no progress", res)
	}
	if len(res.Notes) == 0 {
		t.Error("a net with nowhere to go must say so")
	}
}

// TestRoomOnlyInTheMiddle is the situation that whole-track probing gets wrong.
// A track pinched at both ends but open in the middle measures as having no
// room at all if the probe insists on one amplitude over the whole length.
func TestRoomOnlyInTheMiddle(t *testing.T) {
	s, net := straightTrack(20)
	// Pinched over [10,16] and [24,30]; open over [16,24].
	s.blocker("L1", 10, 16, synthClr)
	s.blocker("L2", 24, 30, synthClr)
	s.track("R1", "F.Cu", geom.Pt{X: 10, Y: 50 - synthClr - synthW}, geom.Pt{X: 16, Y: 50 - synthClr - synthW}, synthW)
	s.track("R2", "F.Cu", geom.Pt{X: 24, Y: 50 - synthClr - synthW}, geom.Pt{X: 30, Y: 50 - synthClr - synthW}, synthW)
	_, _, tu := s.build(t, synthClr, synthW)

	runs := tu.findRuns(net, synthW, nil)
	if len(runs) == 0 {
		t.Fatal("no stretch found; the open middle should be usable")
	}
	// Every stretch must lie inside the open window, give or take a step.
	for _, r := range runs {
		lo := minOf(r.from.X, r.to.X)
		hi := maxOf(r.from.X, r.to.X)
		if lo < 15.4 || hi > 24.6 {
			t.Errorf("stretch %.2f..%.2f strays outside the open window 16..24", lo, hi)
		}
		if r.amp < tu.style.MinAmplitude {
			t.Errorf("stretch %.2f..%.2f has amplitude %.3f", lo, hi, r.amp)
		}
	}
	res, err := tu.Tune(net, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added <= 0 {
		t.Errorf("nothing fitted in an 8 mm open window: %+v", res)
	}
	t.Logf("open window of 8 mm delivered %.3f mm", res.Added)
}

// TestRoomInTwoSeparateStretches checks that both are used, not just the first.
func TestRoomInTwoSeparateStretches(t *testing.T) {
	s, net := straightTrack(30)
	// Pinched only in the middle, over [18,22].
	s.blocker("L", 18, 22, synthClr)
	s.track("R", "F.Cu", geom.Pt{X: 18, Y: 50 - synthClr - synthW}, geom.Pt{X: 22, Y: 50 - synthClr - synthW}, synthW)
	_, _, tu := s.build(t, synthClr, synthW)
	// A shallow meander, so that one stretch alone cannot supply the
	// requirement and both have to be used.
	tu.style.MaxAmplitude = 0.3

	runs := tu.findRuns(net, synthW, nil)
	if len(runs) < 2 {
		t.Fatalf("found %d stretches; a track pinched only in the middle has two", len(runs))
	}
	// They must not overlap: two meanders cannot share track.
	for i := range runs {
		for j := i + 1; j < len(runs); j++ {
			a0, a1 := minOf(runs[i].from.X, runs[i].to.X), maxOf(runs[i].from.X, runs[i].to.X)
			b0, b1 := minOf(runs[j].from.X, runs[j].to.X), maxOf(runs[j].from.X, runs[j].to.X)
			if a0 < b1-1e-9 && b0 < a1-1e-9 {
				t.Errorf("stretches %.2f..%.2f and %.2f..%.2f overlap", a0, a1, b0, b1)
			}
		}
	}
	// What one stretch can hold, so the requirement has to spill into the other.
	one := tu.capacity(runs[0].length, runs[0].amp, synthW)
	res, err := tu.Tune(net, one*1.5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Meanders < 2 {
		t.Errorf("asked for %.3f mm when one stretch holds %.3f, but used %d meander(s)",
			one*1.5, one, res.Meanders)
	}
	if res.Added <= one+1e-6 {
		t.Errorf("added %.3f mm, no more than the %.3f a single stretch holds", res.Added, one)
	}
	t.Logf("one stretch holds %.3f mm; two delivered %.3f across %d meanders", one, res.Added, res.Meanders)
}

// TestRoomOnOneSideOnly checks that the meander goes to the side that is free.
func TestRoomOnOneSideOnly(t *testing.T) {
	s, net := straightTrack(20)
	// Blocked above, open below.
	s.blocker("L", 5, 35, synthClr)
	b, _, tu := s.build(t, synthClr, synthW)

	runs := tu.findRuns(net, synthW, nil)
	if len(runs) == 0 {
		t.Fatal("the open side should be usable")
	}
	for _, r := range runs {
		if r.side.Y >= 0 {
			t.Errorf("stretch meanders towards the blocked side: side %v", r.side)
		}
	}
	res, err := tu.Tune(net, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added <= 0 {
		t.Fatal("nothing fitted on the open side")
	}
	// Every new track must stay on the open side of the original line.
	for _, tr := range b.TracksOfNet(net) {
		for _, p := range []geom.Pt{tr.Start, tr.End} {
			if p.Y > 50+1e-9 {
				t.Errorf("copper at %v crossed into the blocked side", p)
			}
		}
	}
}

// TestAmplitudeIsLimitedByTheTightestPoint checks that a stretch takes the
// smallest amplitude along it, not the average or the largest.
func TestAmplitudeIsLimitedByTheTightestPoint(t *testing.T) {
	s, net := straightTrack(20)
	// A single short obstacle 0.5 mm away, in the middle of an otherwise open
	// run, and the other side blocked so the choice is forced.
	s.track("R", "F.Cu", geom.Pt{X: 5, Y: 50 - synthClr - synthW}, geom.Pt{X: 35, Y: 50 - synthClr - synthW}, synthW)
	s.blocker("L", 19, 21, 0.5)
	_, _, tu := s.build(t, synthClr, synthW)

	for _, r := range tu.findRuns(net, synthW, nil) {
		lo, hi := minOf(r.from.X, r.to.X), maxOf(r.from.X, r.to.X)
		if lo > 21 || hi < 19 {
			continue // not over the obstacle
		}
		// Copper gap 0.5, so the centreline may stray 0.5 + w/2 - clearance -
		// w/2 = 0.3 before it is inside the clearance.
		if r.amp > 0.5-synthClr+1e-6 {
			t.Errorf("stretch %.2f..%.2f took amplitude %.3f past an obstacle 0.5 mm away", lo, hi, r.amp)
		}
	}
}

// TestBoardEdgeLimitsTheMeander checks that the outline is respected: copper
// pushed off the board is not a clearance problem between nets, and nothing
// else would catch it.
func TestBoardEdgeLimitsTheMeander(t *testing.T) {
	s := newSynth()
	const net = "N1"
	// A track 0.4 mm inside the board's bottom edge, which is at y=0.
	s.pad(net, geom.Pt{X: 10, Y: 0.4}, synthW)
	s.pad(net, geom.Pt{X: 30, Y: 0.4}, synthW)
	s.track(net, "F.Cu", geom.Pt{X: 10, Y: 0.4}, geom.Pt{X: 30, Y: 0.4}, synthW)
	// Blocked on the inward side, so the only way out is over the edge.
	s.track("R", "F.Cu", geom.Pt{X: 5, Y: 0.4 + synthClr + synthW}, geom.Pt{X: 35, Y: 0.4 + synthClr + synthW}, synthW)
	b, _, tu := s.build(t, synthClr, synthW)

	res, err := tu.Tune(net, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Whatever it managed, no copper may end up within the edge clearance.
	for _, tr := range b.TracksOfNet(net) {
		for _, p := range []geom.Pt{tr.Start, tr.End} {
			if p.Y < synthClr {
				t.Errorf("copper at %v is inside the %.2f mm edge clearance", p, synthClr)
			}
		}
	}
	t.Logf("fitted %.3f mm of 5 mm against the board edge", res.Added)
}

// TestMeanderIsCheckedAgainstCopperAddedForAnotherNet checks the incremental
// case: two nets sharing the same open space cannot both claim it.
func TestMeanderIsCheckedAgainstCopperAddedForAnotherNet(t *testing.T) {
	s := newSynth()
	// A and B run side by side at minimum clearance. The only free space is
	// above B; below A is a wall. So whichever net is tuned first takes the
	// corridor, and the second must find it gone.
	for i, net := range []string{"A", "B"} {
		y := 50 + float64(i)*(synthClr+synthW)
		s.pad(net, geom.Pt{X: 10, Y: y}, synthW)
		s.pad(net, geom.Pt{X: 30, Y: y}, synthW)
		s.track(net, "F.Cu", geom.Pt{X: 10, Y: y}, geom.Pt{X: 30, Y: y}, synthW)
	}
	s.track("WALL", "F.Cu", geom.Pt{X: 5, Y: 50 - synthClr - synthW}, geom.Pt{X: 35, Y: 50 - synthClr - synthW}, synthW)
	b, p, tu := s.build(t, synthClr, synthW)
	tu.style.MaxAmplitude = 0.6

	// B is the upper one and has the open side.
	first, err := tu.Tune("B", 6, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Added <= 0 {
		t.Fatal("B should have room above it")
	}
	// A is walled in below and now has B's meander above it as well.
	second, err := tu.Tune("A", 6, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Added > 1e-6 {
		t.Errorf("A added %.4f mm, but the only corridor was already taken by B", second.Added)
	}
	t.Logf("B took %.3f mm; A, walled in and with B above it, managed %.3f mm", first.Added, second.Added)

	// Whatever both took, nothing may be inside the clearance.
	chk := drc.New(b, p)
	for _, net := range []string{"A", "B"} {
		for k, v := range worstForNet(b, chk, net) {
			if v < p.ClearanceOf(net)-1e-9 {
				t.Errorf("%s ends up %.4f mm from %s, inside %.4f", net, v, k, p.ClearanceOf(net))
			}
		}
	}
}

func minOf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxOf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
