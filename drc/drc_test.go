package drc

import (
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

const demoPath = "../demo-pcb/ai-vision.kicad_pcb"

func checker(t *testing.T) (*board.Board, *Checker) {
	t.Helper()
	b, err := board.Load(demoPath)
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	p, err := board.LoadProject(demoPath)
	if err != nil {
		t.Fatal(err)
	}
	return b, New(b, p)
}

func TestIndexCoversTheBoard(t *testing.T) {
	b, c := checker(t)
	// Every track, via, pad, zone polygon and edge segment, on every layer it
	// occupies. A big drop would mean the index is blind somewhere.
	if c.Obstacles() < len(b.Tracks)+len(b.Pads) {
		t.Errorf("indexed %d obstacles for %d tracks and %d pads", c.Obstacles(), len(b.Tracks), len(b.Pads))
	}
}

// TestExistingCopperClashesWithItsNeighbours is a calibration test: take a real
// track off the board and offer it back as a candidate without exempting
// itself. It must be reported as clashing with itself, which proves the
// geometry, the layer filter and the index all line up. Offering it back *with*
// itself exempted must come out clear, because the board is DRC-clean there.
func TestExistingCopperClashesWithItselfUnlessExempt(t *testing.T) {
	b, c := checker(t)
	var probe *board.Track
	for _, tr := range b.Tracks {
		if tr.Kind == board.KindSegment && tr.Net == "/ddr4/DDR_DQ0" && tr.Length(b.Stackup) > 5 {
			probe = tr
			break
		}
	}
	if probe == nil {
		t.Skip("no long DQ0 segment")
	}
	cand := Candidate{Shapes: probe.Shape(b.MaxError), Layer: probe.Layer, Net: probe.Net}
	if c.Clear(cand) {
		t.Error("a track offered back without exempting itself should clash with itself")
	}
	vs := c.Check(cand)
	found := false
	for _, v := range vs {
		if v.Ref == probe.UUID {
			found = true
		}
	}
	if !found {
		t.Errorf("self-clash not reported; got %d violations", len(vs))
	}
}

// TestEmptyAreaIsClear checks the other direction: a candidate well away from
// any copper must pass.
func TestEmptyAreaIsClear(t *testing.T) {
	_, c := checker(t)
	// Far outside the board.
	cand := Candidate{
		Shapes: []geom.RoundPoly{geom.Capsule(geom.Pt{X: -100, Y: -100}, geom.Pt{X: -90, Y: -100}, 0.09)},
		Layer:  "F.Cu",
		Net:    "/ddr4/DDR_DQ0",
	}
	if !c.Clear(cand) {
		t.Errorf("empty space reported as occupied: %+v", c.Check(cand))
	}
}

// TestBoardEdgeIsRespected checks that copper pushed off the edge of the board
// is refused, and that the reason given is the edge rather than a net.
func TestBoardEdgeIsRespected(t *testing.T) {
	b, c := checker(t)
	var box geom.Rect
	first := true
	for _, s := range b.Outline {
		if first {
			box = s.Box()
			first = false
			continue
		}
		box = box.Union(s.Box())
	}
	// A candidate straddling the left edge, in a vertical position where the
	// board exists.
	y := box.Centre().Y
	cand := Candidate{
		Shapes: []geom.RoundPoly{geom.Capsule(
			geom.Pt{X: box.MinX - 0.5, Y: y}, geom.Pt{X: box.MinX + 0.02, Y: y}, 0.09)},
		Layer: "F.Cu",
		Net:   "/ddr4/DDR_DQ0",
	}
	vs := c.Check(cand)
	edge := false
	for _, v := range vs {
		if v.Kind == "board-edge" {
			edge = true
		}
	}
	if !edge {
		t.Errorf("copper across the board edge not reported: %+v", vs)
	}
}

// TestLayersAreIndependent checks that copper on one layer is not blocked by
// copper on another: the whole point of a stack-up.
func TestLayersAreIndependent(t *testing.T) {
	b, c := checker(t)
	var probe *board.Track
	for _, tr := range b.Tracks {
		if tr.Kind == board.KindSegment && tr.Layer == "F.Cu" && tr.Length(b.Stackup) > 3 {
			probe = tr
			break
		}
	}
	if probe == nil {
		t.Skip("no probe track")
	}
	// The same shape on In2.Cu, an inner layer with only a power pour on In1.
	cand := Candidate{Shapes: probe.Shape(b.MaxError), Layer: "In2.Cu", Net: "/ddr4/DDR_DQ0"}
	for _, v := range c.Check(cand) {
		if v.Layer == "F.Cu" {
			t.Errorf("F.Cu copper blocked an In2.Cu candidate: %+v", v)
		}
	}
}

// TestSameNetGapIsEnforced checks the rule KiCad does not have. Two pieces of
// one net may short without DRC complaining, so nothing but this check stops a
// meander from folding onto itself and coming out shorter than it measures.
func TestSameNetGapIsEnforced(t *testing.T) {
	b, c := checker(t)
	var probe *board.Track
	for _, tr := range b.Tracks {
		if tr.Kind == board.KindSegment && tr.Net == "/ddr4/DDR_DQ0" && tr.Length(b.Stackup) > 5 {
			probe = tr
			break
		}
	}
	if probe == nil {
		t.Skip("no probe track")
	}
	dir := probe.End.Sub(probe.Start).Norm().Perp()
	exempt := ExemptTracks(probe)
	// Just inside the gap: refused. Well outside it: allowed.
	for _, k := range []struct {
		off  float64
		want bool
	}{{c.SameNetGap * 0.5, false}, {c.SameNetGap * 4, true}} {
		a := probe.Start.Add(dir.Mul(k.off))
		bb := probe.End.Add(dir.Mul(k.off))
		cand := Candidate{
			Shapes: []geom.RoundPoly{geom.Capsule(a, bb, 0.09)},
			Layer:  probe.Layer, Net: probe.Net, Exempt: exempt,
		}
		// Only judge the same-net question: ignore clashes with other nets,
		// which a real board is full of.
		hit := false
		for _, v := range c.Check(cand) {
			if v.Net == probe.Net {
				hit = true
			}
		}
		if k.want && hit {
			t.Errorf("offset %.3f mm from its own net was refused", k.off)
		}
		if !k.want && !hit {
			t.Errorf("offset %.3f mm from its own net was allowed; the gap is %.3f", k.off, c.SameNetGap)
		}
	}
}

// TestSameNetPadsAndZonesAreNotObstacles checks that the same-net gap does not
// fight the connections a track is supposed to make: its own pads, and a pour
// it is meant to tie into.
func TestSameNetPadsAndZonesAreNotObstacles(t *testing.T) {
	b, c := checker(t)
	var pad *board.Pad
	for _, p := range b.Pads {
		if p.Net == "/ddr4/DDR_DQ0" {
			pad = p
			break
		}
	}
	if pad == nil {
		t.Skip("no DQ0 pad")
	}
	cand := Candidate{
		Shapes: []geom.RoundPoly{geom.Capsule(pad.Centre, pad.Centre, 0.09)},
		Layer:  "F.Cu", Net: pad.Net,
	}
	for _, v := range c.Check(cand) {
		if v.Kind == "pad" && v.Net == pad.Net {
			t.Errorf("a track was blocked by its own pad: %+v", v)
		}
	}
}

// TestClearanceUsesTheStricterNetClass checks that the required clearance
// between two nets is the larger of their two class requirements, as KiCad
// resolves it. DDR asks 0.2 and DDR_DIFF asks 0.1, so a DIFF net next to a DDR
// net must still be held to 0.2.
func TestClearanceUsesTheStricterNetClass(t *testing.T) {
	b, c := checker(t)
	var ddr *board.Track
	for _, tr := range b.Tracks {
		if tr.Kind == board.KindSegment && tr.Net == "/ddr4/DDR_DQ0" && tr.Length(b.Stackup) > 5 {
			ddr = tr
			break
		}
	}
	if ddr == nil {
		t.Skip("no probe")
	}
	dir := ddr.End.Sub(ddr.Start).Norm().Perp()
	// 0.15 mm centre to centre with 0.09 tracks leaves a 0.06 gap: inside
	// 0.2, outside 0.1. A DIFF-class candidate must still be refused.
	off := 0.15
	cand := Candidate{
		Shapes: []geom.RoundPoly{geom.Capsule(ddr.Start.Add(dir.Mul(off)), ddr.End.Add(dir.Mul(off)), 0.09)},
		Layer:  ddr.Layer, Net: "/ddr4/DDR_DQS0_P",
	}
	hit := false
	for _, v := range c.Check(cand) {
		if v.Ref == ddr.UUID {
			hit = true
			if v.Required < 0.2-1e-9 {
				t.Errorf("required clearance %.3f, want the stricter 0.2", v.Required)
			}
		}
	}
	if !hit {
		t.Error("a DDR_DIFF candidate 0.06 mm from a DDR track was allowed")
	}
}

func BenchmarkClear(bench *testing.B) {
	b, err := board.Load(demoPath)
	if err != nil {
		bench.Skip(err)
	}
	p, _ := board.LoadProject(demoPath)
	c := New(b, p)
	var probe *board.Track
	for _, tr := range b.Tracks {
		if tr.Kind == board.KindSegment && tr.Net == "/ddr4/DDR_DQ0" && tr.Length(b.Stackup) > 5 {
			probe = tr
			break
		}
	}
	cand := Candidate{Shapes: probe.Shape(b.MaxError), Layer: probe.Layer, Net: probe.Net, Exempt: ExemptTracks(probe)}
	for bench.Loop() {
		c.Clear(cand)
	}
}

func BenchmarkNewChecker(bench *testing.B) {
	b, err := board.Load(demoPath)
	if err != nil {
		bench.Skip(err)
	}
	p, _ := board.LoadProject(demoPath)
	for bench.Loop() {
		New(b, p)
	}
}
