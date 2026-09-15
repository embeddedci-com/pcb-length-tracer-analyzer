package drc

import (
	"fmt"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// A board with two things on it: a component, and copper out in the open.
//
// The open-field clearance is a rule about place, so the fixture has to have
// two places in it. The demo board has both, but not at distances chosen to sit
// either side of a threshold, which is the only thing worth testing here.
type tiny struct {
	items []string
	uid   int
}

func (s *tiny) next() string {
	s.uid++
	return fmt.Sprintf("%08x-0000-4000-8000-00000000000a", s.uid)
}

func (s *tiny) track(net string, a, b geom.Pt) {
	s.items = append(s.items, fmt.Sprintf(`	(segment (start %g %g) (end %g %g) (width 0.09) (layer "F.Cu") (net "%s") (uuid "%s"))`,
		a.X, a.Y, b.X, b.Y, net, s.next()))
}

// component puts a 2x2 pad field centred on `at`, 1 mm across.
func (s *tiny) component(at geom.Pt) {
	var pads []string
	for i, d := range []geom.Pt{{X: -0.5, Y: -0.5}, {X: 0.5, Y: -0.5}, {X: -0.5, Y: 0.5}, {X: 0.5, Y: 0.5}} {
		pads = append(pads, fmt.Sprintf(`		(pad "%d" smd circle (at %g %g) (size 0.3 0.3) (layers "F.Cu") (net "P%d") (uuid "%s"))`,
			i+1, d.X, d.Y, i+1, s.next()))
	}
	s.items = append(s.items, fmt.Sprintf(`	(footprint "t:bga"
		(layer "F.Cu")
		(uuid "%s")
		(at %g %g)
		(attr smd)
		(property "Reference" "U1" (at 0 0) (layer "F.SilkS") (uuid "%s") (effects (font (size 1 1) (thickness 0.15))))
%s
	)`, s.next(), at.X, at.Y, s.next(), strings.Join(pads, "\n")))
}

func (s *tiny) build(t *testing.T, clearance float64) (*board.Board, *Checker) {
	t.Helper()
	src := `(kicad_pcb
	(version 20260206)
	(generator "pcb-trace-length-analyzer-test")
	(paper "A4")
	(layers (0 "F.Cu" signal) (2 "B.Cu" signal) (1 "F.Mask" user) (5 "F.SilkS" user "F.Silkscreen") (25 "Edge.Cuts" user))
	(setup
		(stackup
			(layer "F.Cu" (type "copper") (thickness 0.035))
			(layer "dielectric 1" (type "core") (thickness 1.53) (material "FR4") (epsilon_r 4.5))
			(layer "B.Cu" (type "copper") (thickness 0.035))
		)
	)
	(gr_rect (start 0 0) (end 100 100) (stroke (width 0.05) (type default)) (fill none) (layer "Edge.Cuts") (uuid "ffffffff-0000-4000-8000-00000000000f"))
` + strings.Join(s.items, "\n") + "\n)\n"
	b, err := board.Parse([]byte(src), "tiny.kicad_pcb")
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	p := board.Defaults()
	p.MinClearance = clearance
	p.MinTrackWidth = 0.09
	p.EdgeClearance = clearance
	p.Classes = []board.NetClass{{Name: "Default", Clearance: clearance, TrackWidth: 0.09}}
	return b, New(b, p)
}

// probe offers a track of net `net` alongside y, and reports whether it clears.
func probe(c *Checker, net string, a, b geom.Pt) bool {
	return c.Clear(Candidate{Shapes: []geom.RoundPoly{geom.Capsule(a, b, 0.09)}, Layer: "F.Cu", Net: net})
}

// The board says 0.1 mm because that is what got the BGA escaped. Out in the
// open, 0.15 mm between two unrelated nets is legal by the board's rules and
// tighter than the user asked for.
func TestTheOpenFieldClearanceIsHeldToInTheOpen(t *testing.T) {
	s := &tiny{}
	s.track("A", geom.Pt{X: 20, Y: 50}, geom.Pt{X: 40, Y: 50})
	b, c := s.build(t, 0.1)
	_ = b

	near := func() bool {
		// 0.15 mm of copper-to-copper gap: 0.09 of width plus 0.15.
		return probe(c, "B", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24})
	}
	if !near() {
		t.Fatal("0.15 mm apart is clear under a 0.1 mm board rule; the fixture is wrong")
	}
	c.SetOpenSpacing(OpenSpacing{Clearance: 0.2, Margin: 1.0})
	if near() {
		t.Error("0.15 mm apart in the open was allowed with a 0.2 mm open-field clearance")
	}
	// And what does clear it, clears.
	if !probe(c, "B", geom.Pt{X: 20, Y: 50.31}, geom.Pt{X: 40, Y: 50.31}) {
		t.Error("0.22 mm apart in the open was refused with a 0.2 mm open-field clearance")
	}
}

// Around the component the board's own rules stand: that is where the tight
// figure was chosen on purpose, and nothing here knows better.
func TestTheBoardsOwnRulesStandAroundAComponent(t *testing.T) {
	s := &tiny{}
	s.component(geom.Pt{X: 30, Y: 50})
	s.track("A", geom.Pt{X: 29, Y: 51.5}, geom.Pt{X: 31, Y: 51.5})
	_, c := s.build(t, 0.1)
	c.SetOpenSpacing(OpenSpacing{Clearance: 0.2, Margin: 1.0})

	// The component's pads span 50.5 at the top, so with a 1 mm margin
	// everything below 51.5 is "around" it.
	if !probe(c, "B", geom.Pt{X: 29, Y: 51.26}, geom.Pt{X: 31, Y: 51.26}) {
		t.Error("0.15 mm apart beside a component was refused; the board allows 0.1 mm there")
	}
	// Far enough away, the open-field clearance applies again.
	s2 := &tiny{}
	s2.component(geom.Pt{X: 30, Y: 50})
	s2.track("A", geom.Pt{X: 29, Y: 56}, geom.Pt{X: 31, Y: 56})
	_, c2 := s2.build(t, 0.1)
	c2.SetOpenSpacing(OpenSpacing{Clearance: 0.2, Margin: 1.0})
	if probe(c2, "B", geom.Pt{X: 29, Y: 56.24}, geom.Pt{X: 31, Y: 56.24}) {
		t.Error("0.15 mm apart well clear of the component was allowed")
	}
}

// A differential pair is exempt in both directions. Its two halves are meant to
// run close together, and the gap they run at was chosen for the impedance they
// were drawn to.
func TestADifferentialPairIsExemptFromTheOpenFieldClearance(t *testing.T) {
	s := &tiny{}
	s.track("D_P", geom.Pt{X: 20, Y: 50}, geom.Pt{X: 40, Y: 50})
	_, c := s.build(t, 0.1)
	c.SetOpenSpacing(OpenSpacing{
		Clearance: 0.2, Margin: 1.0,
		Coupled: map[string]string{"D_P": "D_N", "D_N": "D_P"},
	})
	if !probe(c, "D_N", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24}) {
		t.Error("the halves of a pair were held apart by the open-field clearance")
	}
	// Its partner is exempt; a third net is not.
	if probe(c, "E", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24}) {
		t.Error("an unrelated net beside a pair was let inside the open-field clearance")
	}
	// Named only one way round, it still works: the map is read in both
	// directions, because which half is the candidate is an accident of
	// whichever one is being drawn.
	s2 := &tiny{}
	s2.track("D_N", geom.Pt{X: 20, Y: 50}, geom.Pt{X: 40, Y: 50})
	_, c2 := s2.build(t, 0.1)
	c2.SetOpenSpacing(OpenSpacing{
		Clearance: 0.2, Margin: 1.0,
		Coupled: map[string]string{"D_P": "D_N"},
	})
	if !probe(c2, "D_P", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24}) {
		t.Error("a pair named one way round was not recognised the other way round")
	}
}

// The override only ever tightens. A board whose own rule is stricter keeps it.
func TestTheOpenFieldClearanceNeverRelaxesTheBoardsRules(t *testing.T) {
	s := &tiny{}
	s.track("A", geom.Pt{X: 20, Y: 50}, geom.Pt{X: 40, Y: 50})
	_, c := s.build(t, 0.3)
	c.SetOpenSpacing(OpenSpacing{Clearance: 0.2, Margin: 1.0})
	if probe(c, "B", geom.Pt{X: 20, Y: 50.34}, geom.Pt{X: 40, Y: 50.34}) {
		t.Error("a 0.2 mm open-field clearance relaxed a 0.3 mm board rule")
	}
}

// Zero means the board's rules, everywhere. Setting it and clearing it again
// must leave the checker exactly as it was.
func TestZeroLeavesTheBoardsRulesAlone(t *testing.T) {
	s := &tiny{}
	s.track("A", geom.Pt{X: 20, Y: 50}, geom.Pt{X: 40, Y: 50})
	_, c := s.build(t, 0.1)
	at := func() bool { return probe(c, "B", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24}) }
	if !at() {
		t.Fatal("the fixture is wrong")
	}
	c.SetOpenSpacing(OpenSpacing{Clearance: 0.2, Margin: 1.0})
	if at() {
		t.Fatal("the open-field clearance did not apply")
	}
	c.SetOpenSpacing(OpenSpacing{})
	if !at() {
		t.Error("clearing the open-field clearance left it in force")
	}
	if c.OpenClearance() != 0 {
		t.Errorf("OpenClearance reports %.3f after being cleared", c.OpenClearance())
	}
}

// ---- clearance zones ----
//
// A zone is the user asking for more space inside a region they drew. Two rules
// hold it down, and both are about not doing more than they asked: it applies
// only between two pieces of copper that are both inside, and it only ever
// tightens.

func TestAZoneWidensTheSpacingInsideIt(t *testing.T) {
	s := &tiny{}
	s.track("A", geom.Pt{X: 20, Y: 50}, geom.Pt{X: 40, Y: 50})
	_, c := s.build(t, 0.1)

	// 0.15 mm apart: legal at the board's 0.1.
	near := func() bool {
		return probe(c, "B", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24})
	}
	if !near() {
		t.Fatal("0.15 mm apart is clear at 0.1 mm; the fixture is wrong")
	}
	c.SetClearanceZones([]ClearanceZone{{
		Area: geom.Rect{MinX: 0, MinY: 0, MaxX: 60, MaxY: 60}, MinClearance: 0.3,
	}})
	if near() {
		t.Error("0.15 mm apart was allowed inside a zone asking for 0.3")
	}
	if !probe(c, "B", geom.Pt{X: 20, Y: 50.45}, geom.Pt{X: 40, Y: 50.45}) {
		t.Error("0.36 mm apart was refused inside a zone asking for 0.3")
	}
}

// Both pieces, not one. A zone is a statement about the copper inside it, and
// applying it to something half outside would be widening a clearance the user
// never asked about.
func TestAZoneNeedsBothPiecesInsideIt(t *testing.T) {
	s := &tiny{}
	// A runs across the board; the zone covers only its left half.
	s.track("A", geom.Pt{X: 5, Y: 50}, geom.Pt{X: 55, Y: 50})
	_, c := s.build(t, 0.1)
	c.SetClearanceZones([]ClearanceZone{{
		Area: geom.Rect{MinX: 0, MinY: 0, MaxX: 20, MaxY: 60}, MinClearance: 0.4,
	}})
	// Well outside the zone, 0.15 mm away: the board's own rule applies.
	if !probe(c, "B", geom.Pt{X: 40, Y: 50.24}, geom.Pt{X: 50, Y: 50.24}) {
		t.Error("copper outside the zone was held to the zone's clearance")
	}
	// Inside it, the same spacing is refused.
	if probe(c, "B", geom.Pt{X: 5, Y: 50.24}, geom.Pt{X: 15, Y: 50.24}) {
		t.Error("copper inside the zone was not held to the zone's clearance")
	}
}

// It only ever tightens. An area is somewhere the user chose to put copper, not
// a licence to crowd it past what the board demands.
func TestAZoneNeverLoosensTheBoardsClearance(t *testing.T) {
	s := &tiny{}
	s.track("A", geom.Pt{X: 20, Y: 50}, geom.Pt{X: 40, Y: 50})
	_, c := s.build(t, 0.3)
	c.SetClearanceZones([]ClearanceZone{{
		Area: geom.Rect{MinX: 0, MinY: 0, MaxX: 60, MaxY: 60}, MinClearance: 0.1,
	}})
	if probe(c, "B", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24}) {
		t.Error("a zone asking for 0.1 mm undercut the board's own 0.3")
	}
}

// A pair is exempt, the same way it is from the open-field clearance: the two
// halves are meant to run close, at the gap their impedance was drawn for.
func TestAZoneLeavesADifferentialPairAlone(t *testing.T) {
	s := &tiny{}
	s.track("D_P", geom.Pt{X: 20, Y: 50}, geom.Pt{X: 40, Y: 50})
	_, c := s.build(t, 0.1)
	c.SetOpenSpacing(OpenSpacing{
		Clearance: 0.2, Margin: 1.0,
		Coupled: map[string]string{"D_P": "D_N", "D_N": "D_P"},
	})
	c.SetClearanceZones([]ClearanceZone{{
		Area: geom.Rect{MinX: 0, MinY: 0, MaxX: 60, MaxY: 60}, MinClearance: 0.5,
	}})
	if !probe(c, "D_N", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24}) {
		t.Error("the halves of a pair were held apart by a zone")
	}
	if probe(c, "E", geom.Pt{X: 20, Y: 50.24}, geom.Pt{X: 40, Y: 50.24}) {
		t.Error("an unrelated net was let inside the zone's clearance")
	}
}
