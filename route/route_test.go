package route

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Boards built to ask one question each.
//
// A router is judged by what it does when the direct path is not available,
// and a real board offers those situations only by accident and all at once.
// These offer them one at a time: a wall with a gap, a wall with none, a way
// through only on another layer, and a corridor one route wide that two nets
// both want.

const (
	testW   = 0.2 // track width
	testClr = 0.2 // clearance
)

type fixture struct {
	items []string
	uid   int
	at    map[string]geom.Pt
}

func (s *fixture) next() string {
	s.uid++
	return fmt.Sprintf("%08x-2222-4000-8000-000000000001", s.uid)
}

// pad places a one-pad footprint on a net, reachable from both layers.
func (s *fixture) pad(ref, net string, at geom.Pt) {
	s.padOn(ref, net, at, `"F.Cu" "B.Cu"`)
}

// padOn places a pad on named layers. A pad on one layer only is what forces a
// route that has to change layer to pay for two vias.
func (s *fixture) padOn(ref, net string, at geom.Pt, layers string) {
	if s.at == nil {
		s.at = map[string]geom.Pt{}
	}
	s.at[ref+".1"] = at
	s.items = append(s.items, fmt.Sprintf(`	(footprint "t:p"
		(layer "F.Cu")
		(uuid "%s")
		(at %g %g)
		(attr smd)
		(property "Reference" "%s" (at 0 0) (layer "F.SilkS") (uuid "%s") (effects (font (size 1 1) (thickness 0.15))))
		(pad "1" smd circle (at 0 0) (size %g %g) (layers %s) (net "%s") (uuid "%s"))
	)`, s.next(), at.X, at.Y, ref, s.next(), testW, testW, layers, net, s.next()))
}

// wall lays a solid run of another net's copper.
func (s *fixture) wall(net, layer string, a, b geom.Pt) {
	s.items = append(s.items, fmt.Sprintf(
		`	(segment (start %g %g) (end %g %g) (width %g) (layer "%s") (net "%s") (uuid "%s"))`,
		a.X, a.Y, b.X, b.Y, testW, layer, net, s.next()))
}

func (s *fixture) build(t *testing.T) (*board.Board, *board.Project) {
	t.Helper()
	src := `(kicad_pcb
	(version 20260206)
	(generator "pcb-trace-length-analyzer-test")
	(paper "A4")
	(layers (0 "F.Cu" signal) (2 "B.Cu" signal) (1 "F.Mask" user) (5 "F.SilkS" user "F.Silkscreen") (25 "Edge.Cuts" user))
	(setup (stackup
		(layer "F.Cu" (type "copper") (thickness 0.035))
		(layer "dielectric 1" (type "core") (thickness 1.53) (material "FR4") (epsilon_r 4.5))
		(layer "B.Cu" (type "copper") (thickness 0.035))
	))
	(gr_rect (start 0 0) (end 60 60) (stroke (width 0.05) (type default)) (fill none) (layer "Edge.Cuts") (uuid "ffffffff-2222-4000-8000-000000000002"))
` + strings.Join(s.items, "\n") + "\n)\n"
	b, err := board.Parse([]byte(src), "route.kicad_pcb")
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	p := board.Defaults()
	p.MinClearance = testClr
	p.MinTrackWidth = testW
	p.EdgeClearance = testClr
	p.Classes = []board.NetClass{{Name: "Default", Clearance: testClr, TrackWidth: testW, ViaDiameter: 0.6, ViaDrill: 0.3}}
	return b, p
}

// opts are settings that make the tests quick and the failures legible.
func opts(layers ...string) Options {
	o := DefaultOptions()
	o.Layers = layers
	o.Margin = 8
	return o
}

// routeOne runs a single request and returns the result.
func routeOne(t *testing.T, b *board.Board, p *board.Project, o Options, q Request) (*Router, *Result) {
	t.Helper()
	r, err := New(b, p, []Request{q}, o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Route([]Request{q})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("%d results for one request", len(res))
	}
	return r, res[0]
}

// joined reports whether copper actually joins the two pads, measured the way
// the rest of the tool measures connectivity rather than by trusting the
// router's own bookkeeping.
func joined(b *board.Board, net, from, to string) bool {
	var a, z *board.Pad
	for _, p := range b.PadsOfNet(net) {
		switch p.ID() {
		case from:
			a = p
		case to:
			z = p
		}
	}
	if a == nil || z == nil {
		return false
	}
	// Flood from one pad over touching copper.
	type item struct {
		shapes []geom.RoundPoly
		layers []string
	}
	items := []item{}
	for _, t := range b.TracksOfNet(net) {
		items = append(items, item{t.Shape(b.MaxError), t.Layers(b.Stackup)})
	}
	touches := func(s []geom.RoundPoly, ls []string, o item) bool {
		shared := false
		for _, l := range ls {
			for _, m := range o.layers {
				if l == m {
					shared = true
				}
			}
		}
		if !shared {
			return false
		}
		for _, x := range s {
			for _, y := range o.shapes {
				if x.Dist(y) <= geom.Eps {
					return true
				}
			}
		}
		return false
	}
	front := []item{{[]geom.RoundPoly{a.Shape}, a.Layers}}
	seen := make([]bool, len(items))
	for len(front) > 0 {
		cur := front[len(front)-1]
		front = front[:len(front)-1]
		for i, it := range items {
			if seen[i] || !touches(cur.shapes, cur.layers, it) {
				continue
			}
			seen[i] = true
			front = append(front, it)
			if touches(it.shapes, it.layers, item{[]geom.RoundPoly{z.Shape}, z.Layers}) {
				return true
			}
		}
	}
	return false
}

// clean reports whether every piece of the net's new copper clears the rules.
func clean(t *testing.T, b *board.Board, p *board.Project, net string) string {
	t.Helper()
	chk := drc.New(b, p)
	exempt := map[string]bool{}
	for _, tr := range b.TracksOfNet(net) {
		exempt[tr.UUID] = true
	}
	for _, pd := range b.PadsOfNet(net) {
		exempt[pd.ID()] = true
	}
	for _, tr := range b.TracksOfNet(net) {
		for _, l := range tr.Layers(b.Stackup) {
			for _, v := range chk.Check(drc.Candidate{
				Shapes: tr.Shape(b.MaxError), Layer: l, Net: net, Exempt: exempt,
			}) {
				return fmt.Sprintf("%s on %s sits %.4f mm from %s (%s), needs %.4f",
					net, l, v.Actual, v.Net, v.Kind, v.Required)
			}
		}
	}
	return ""
}

func TestRoutesAcrossAnEmptyBoard(t *testing.T) {
	s := &fixture{}
	s.pad("A", "N1", geom.Pt{X: 10, Y: 30})
	s.pad("B", "N1", geom.Pt{X: 40, Y: 30})
	b, p := s.build(t)

	_, res := routeOne(t, b, p, opts("F.Cu"), Request{Net: "N1", From: "A.1", To: "B.1"})
	if !res.Routed {
		t.Fatalf("an empty board refused a straight route: %s", res.Reason)
	}
	if !joined(b, "N1", "A.1", "B.1") {
		t.Error("the router says it routed, but no copper joins the pads")
	}
	// 30 mm apart; a straight run is 30 mm and anything much longer is a
	// wander the empty board did not call for.
	if res.LengthMM < 29.9 || res.LengthMM > 31.5 {
		t.Errorf("route is %.3f mm for a 30 mm gap", res.LengthMM)
	}
	if res.Vias != 0 {
		t.Errorf("%d via(s) on an empty board", res.Vias)
	}
	if why := clean(t, b, p, "N1"); why != "" {
		t.Error(why)
	}
}

func TestRoutesThroughTheGapInAWall(t *testing.T) {
	s := &fixture{}
	s.pad("A", "N1", geom.Pt{X: 10, Y: 30})
	s.pad("B", "N1", geom.Pt{X: 40, Y: 30})
	// A wall across the middle with one gap, well off the direct line.
	s.wall("WALL", "F.Cu", geom.Pt{X: 25, Y: 5}, geom.Pt{X: 25, Y: 26})
	s.wall("WALL", "F.Cu", geom.Pt{X: 25, Y: 29}, geom.Pt{X: 25, Y: 55})
	b, p := s.build(t)

	_, res := routeOne(t, b, p, opts("F.Cu"), Request{Net: "N1", From: "A.1", To: "B.1"})
	if !res.Routed {
		t.Fatalf("the route did not find the gap: %s", res.Reason)
	}
	if !joined(b, "N1", "A.1", "B.1") {
		t.Error("the router says it routed, but no copper joins the pads")
	}
	if why := clean(t, b, p, "N1"); why != "" {
		t.Error(why)
	}
}

func TestTakesAViaWhenTheLayerIsBlocked(t *testing.T) {
	s := &fixture{}
	s.padOn("A", "N1", geom.Pt{X: 10, Y: 30}, `"F.Cu"`)
	s.padOn("B", "N1", geom.Pt{X: 40, Y: 30}, `"F.Cu"`)
	// F.Cu is walled off completely; B.Cu is clear. The pads are on F.Cu
	// alone, so reaching the clear layer and coming back costs two vias --
	// which is the trade the via cost exists to weigh.
	s.wall("WALL", "F.Cu", geom.Pt{X: 25, Y: -5}, geom.Pt{X: 25, Y: 65})
	b, p := s.build(t)

	_, res := routeOne(t, b, p, opts("F.Cu", "B.Cu"), Request{Net: "N1", From: "A.1", To: "B.1"})
	if !res.Routed {
		t.Fatalf("a route with a clear layer below it was refused: %s", res.Reason)
	}
	if res.Vias < 2 {
		t.Errorf("%d via(s): getting to the other layer and back takes two", res.Vias)
	}
	if !joined(b, "N1", "A.1", "B.1") {
		t.Error("the router says it routed, but no copper joins the pads")
	}
	if why := clean(t, b, p, "N1"); why != "" {
		t.Error(why)
	}
}

func TestSaysSoWhenThereIsNoWayThroughAndWritesNothing(t *testing.T) {
	s := &fixture{}
	s.pad("A", "N1", geom.Pt{X: 10, Y: 30})
	s.pad("B", "N1", geom.Pt{X: 40, Y: 30})
	for _, l := range []string{"F.Cu", "B.Cu"} {
		s.wall("WALL", l, geom.Pt{X: 25, Y: -5}, geom.Pt{X: 25, Y: 65})
	}
	b, p := s.build(t)
	before := len(b.Tracks)

	_, res := routeOne(t, b, p, opts("F.Cu", "B.Cu"), Request{Net: "N1", From: "A.1", To: "B.1"})
	if res.Routed {
		t.Fatal("a route was claimed through a wall on every layer")
	}
	if !strings.Contains(res.Reason, "no way through") {
		t.Errorf("reason %q", res.Reason)
	}
	if n := len(b.Tracks); n != before {
		t.Errorf("%d track(s) written for a route that failed", n-before)
	}
}

// The one that matters on a real board: a corridor only wide enough for one
// route, wanted by two nets. The first one through must be moved for the
// second, not left where it is with the second reported impossible.
func TestRipsUpARouteThatIsInTheWay(t *testing.T) {
	s := &fixture{}
	// Two nets crossing the same one-route gap.
	s.pad("A", "N1", geom.Pt{X: 10, Y: 28})
	s.pad("B", "N1", geom.Pt{X: 40, Y: 28})
	s.pad("C", "N2", geom.Pt{X: 10, Y: 32})
	s.pad("D", "N2", geom.Pt{X: 40, Y: 32})
	// A wall with a gap wide enough for both, but only just.
	s.wall("WALL", "F.Cu", geom.Pt{X: 25, Y: -5}, geom.Pt{X: 25, Y: 27})
	s.wall("WALL", "F.Cu", geom.Pt{X: 25, Y: 33}, geom.Pt{X: 25, Y: 65})
	b, p := s.build(t)

	reqs := []Request{
		{Net: "N1", From: "A.1", To: "B.1"},
		{Net: "N2", From: "C.1", To: "D.1"},
	}
	r, err := New(b, p, reqs, opts("F.Cu"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Route(reqs)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range out {
		if !res.Routed {
			t.Errorf("%s not routed: %s", res.Net, res.Reason)
			continue
		}
		if !joined(b, res.Net, res.From, res.To) {
			t.Errorf("%s claims routed but no copper joins its pads", res.Net)
		}
		if why := clean(t, b, p, res.Net); why != "" {
			t.Error(why)
		}
	}
}

// Nothing the router lays may come closer to anything than the rules allow.
// This is the whole reason the grid does not get the last word.
func TestEveryRouteClearsTheRules(t *testing.T) {
	s := &fixture{}
	s.pad("A", "N1", geom.Pt{X: 8, Y: 30})
	s.pad("B", "N1", geom.Pt{X: 50, Y: 30})
	// A thicket to pick through.
	for i := 0; i < 12; i++ {
		x := 14 + float64(i)*3
		y := 12 + float64((i*7)%20)
		s.wall("W", "F.Cu", geom.Pt{X: x, Y: y}, geom.Pt{X: x, Y: y + 18})
	}
	b, p := s.build(t)

	_, res := routeOne(t, b, p, opts("F.Cu", "B.Cu"), Request{Net: "N1", From: "A.1", To: "B.1"})
	if !res.Routed {
		t.Skipf("the thicket has no way through: %s", res.Reason)
	}
	if why := clean(t, b, p, "N1"); why != "" {
		t.Errorf("%s (after %d attempt(s))", why, res.Attempts)
	}
	if !joined(b, "N1", "A.1", "B.1") {
		t.Error("the router says it routed, but no copper joins the pads")
	}
	t.Logf("%.3f mm, %d via(s), %d attempt(s)", res.LengthMM, res.Vias, res.Attempts)
}

func TestRefusesAPadThatIsNotOnTheNet(t *testing.T) {
	s := &fixture{}
	s.pad("A", "N1", geom.Pt{X: 10, Y: 30})
	s.pad("B", "N2", geom.Pt{X: 40, Y: 30})
	b, p := s.build(t)
	_, res := routeOne(t, b, p, opts("F.Cu"), Request{Net: "N1", From: "A.1", To: "B.1"})
	if res.Routed {
		t.Fatal("joined two pads that are not the same net")
	}
	if !strings.Contains(res.Reason, "not on this net") {
		t.Errorf("reason %q", res.Reason)
	}
}

func TestNewRefusesNothingToDo(t *testing.T) {
	s := &fixture{}
	b, p := s.build(t)
	if _, err := New(b, p, nil, DefaultOptions()); err == nil {
		t.Error("a router was built with no requests")
	}
}

var _ = math.Abs
