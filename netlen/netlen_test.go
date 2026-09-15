package netlen

import (
	"encoding/json"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

type golden struct {
	Nets map[string]struct {
		LengthMM      float64  `json:"length_mm"`
		LengthNoViaMM *float64 `json:"length_no_via_mm"`
		Pads          []string `json:"pads"`
	} `json:"nets"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	b, err := os.ReadFile("../testdata/golden-lengths.json")
	if err != nil {
		t.Skipf("golden fixture missing: %v (run scripts/golden-lengths.sh)", err)
	}
	var g golden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func loadBoard(t *testing.T) *board.Board {
	t.Helper()
	b, err := board.Load("../demo-pcb/ai-vision.kicad_pcb")
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	return b
}

func ddrNets(b *board.Board) []string {
	var out []string
	for _, n := range b.Nets() {
		if strings.HasPrefix(n, "/ddr4/DDR_") {
			out = append(out, n)
		}
	}
	return out
}

// TestKiCadLengthMatchesPcbnew is the acceptance test for the measurement
// engine. KiCadLength has to reproduce the number pcbnew reports for every one
// of the 71 DDR nets on the demo board, taken from kicad-cli itself and stored
// in testdata/golden-lengths.json.
//
// It comes out exact -- to the four decimal places KiCad prints -- on every net
// whose copper stays clear of its pads. On the rest it reads slightly high,
// because pcbnew shortens the part of a track lying inside a pad by an amount
// that depends on how it polygonises the pad outline; that residual is bounded
// here so a regression in the geometry cannot hide inside it.
func TestKiCadLengthMatchesPcbnew(t *testing.T) {
	b := loadBoard(t)
	g := loadGolden(t)
	e := New(b)

	if len(g.Nets) != 71 {
		t.Fatalf("fixture has %d nets, want 71", len(g.Nets))
	}
	exact, worst := 0, 0.0
	var worstNet string
	for net, want := range g.Nets {
		m := e.Measure(net)
		d := math.Abs(m.KiCadLength - want.LengthMM)
		if d < 1e-4 {
			exact++
		}
		if d > worst {
			worst, worstNet = d, net
		}
		// The in-pad residual is at most a couple of tenths of a millimetre on
		// this board. Anything beyond that is a real disagreement.
		if d > 0.25 {
			t.Errorf("%s: KiCadLength %.4f mm, pcbnew says %.4f mm (off by %.4f)",
				net, m.KiCadLength, want.LengthMM, d)
		}
	}
	// 33 of the 71 nets have no copper crossing a pad, and those must agree to
	// the last printed digit.
	if exact < 33 {
		t.Errorf("only %d of %d nets agree exactly with pcbnew, want at least 33", exact, len(g.Nets))
	}
	t.Logf("exact on %d/%d nets; worst residual %.4f mm on %s", exact, len(g.Nets), worst, worstNet)
}

// TestViaBarrelRule pins down the one rule that is easy to get wrong: pcbnew
// charges a via's barrel toward net length only when the net has copper on at
// least two of the layers that via spans. Every via on this board is declared
// F.Cu to B.Cu, so a naive count charges three barrels on an address net where
// pcbnew charges two -- a 1.594 mm error, larger than any DDR matching budget.
func TestViaBarrelRule(t *testing.T) {
	b := loadBoard(t)
	g := loadGolden(t)
	e := New(b)
	noVia := New(b)
	noVia.CountViaLength = false
	barrel := b.Stackup.ViaLength("F.Cu", "B.Cu")

	charged := map[int]int{}
	for net, want := range g.Nets {
		if want.LengthNoViaMM == nil {
			continue
		}
		full := e.Measure(net)
		bare := noVia.Measure(net)

		// The engine's own split must be a whole number of barrels.
		n := (full.KiCadLength - bare.KiCadLength) / barrel
		if math.Abs(n-math.Round(n)) > 1e-9 {
			t.Errorf("%s: via contribution is %.3f barrels, not a whole number", net, n)
		}
		charged[int(math.Round(n))]++

		// And it must be the same number pcbnew charged.
		kn := (want.LengthMM - *want.LengthNoViaMM) / barrel
		if math.Abs(n-kn) > 1e-3 {
			t.Errorf("%s: charged %.0f barrels, pcbnew charged %.0f", net, n, kn)
		}
	}
	// 44 point-to-point nets have no vias; the 27 fly-by nets have three each
	// but only two carry copper on both sides.
	if charged[0] != 44 || charged[2] != 25 || charged[3] != 2 {
		t.Errorf("barrels charged per net = %v, want 44 nets with 0, 25 with 2, 2 with 3", charged)
	}
}

// TestRoutingCompletenessOnTheDemoBoard records what this board actually is,
// because every later stage depends on it: a net can only be length matched
// where it is routed.
//
// All 44 point-to-point nets -- the DQ bits, the strobes and the data masks --
// are complete. None of the 27 fly-by nets is: every one has its first leg from
// the MPU to the near device routed, and not one has the second leg on to the
// far device. Only the clock pair has any copper at its termination resistor,
// and that copper is a stranded island.
//
// pcbnew's DRC reports none of this: its unconnected-items list is empty for
// every DDR net, and its length figure adds up copper across the islands as if
// they were one route. The tool therefore works this out from the geometry.
func TestRoutingCompletenessOnTheDemoBoard(t *testing.T) {
	b := loadBoard(t)
	e := New(b)
	var complete, incomplete []string
	legs := map[string]int{}
	for _, net := range ddrNets(b) {
		m := e.Measure(net)
		short := strings.TrimPrefix(net, "/ddr4/DDR_")
		if m.Complete {
			complete = append(complete, short)
			continue
		}
		incomplete = append(incomplete, short)
		if len(m.Pads) != 4 {
			continue
		}
		for _, leg := range []struct{ a, b string }{{"U3", "U4"}, {"U4", "U5"}, {"U3", "U5"}} {
			if findPath(m, leg.a, leg.b).Found {
				legs[leg.a+"-"+leg.b]++
			}
		}
	}
	if len(complete) != 44 {
		t.Errorf("%d complete nets, want 44: %v", len(complete), complete)
	}
	if len(incomplete) != 27 {
		t.Errorf("%d incomplete nets, want 27", len(incomplete))
	}
	if legs["U3-U4"] != 27 {
		t.Errorf("U3-U4 leg routed on %d fly-by nets, want 27", legs["U3-U4"])
	}
	if legs["U4-U5"] != 0 || legs["U3-U5"] != 0 {
		t.Errorf("far leg unexpectedly routed: U4-U5 on %d, U3-U5 on %d", legs["U4-U5"], legs["U3-U5"])
	}
	// Every incomplete net must be a fly-by one; a stranded point-to-point net
	// would mean the connectivity search is broken, not that the board is.
	for _, n := range incomplete {
		if strings.HasPrefix(n, "DQ") {
			t.Errorf("point-to-point net %s reported incomplete", n)
		}
	}
}

// TestPathMatchesKiCadOnCleanCompleteNets checks the path metric itself. Where a
// net is complete and its copper is a single chain with no stub, no loop and
// nothing crossing a pad, the route and pcbnew's sum are the same quantity and
// must agree to the last digit pcbnew prints.
func TestPathMatchesKiCadOnCleanCompleteNets(t *testing.T) {
	b := loadBoard(t)
	g := loadGolden(t)
	e := New(b)
	checked := 0
	for net, want := range g.Nets {
		m := e.Measure(net)
		if !m.Complete {
			continue
		}
		// A clean chain is one where the route uses every millimetre of copper.
		if math.Abs(m.Longest.Length-m.TotalCopper) > 1e-6 {
			continue
		}
		if math.Abs(m.KiCadLength-want.LengthMM) > 1e-4 {
			continue // pcbnew clipped in-pad copper here
		}
		checked++
		// pcbnew prints four decimals, so agreement is bounded by its own
		// rounding rather than by anything this engine does.
		if d := math.Abs(m.Longest.Length - want.LengthMM); d > 5e-5 {
			t.Errorf("%s: route %.6f mm, pcbnew %.4f mm (off by %.6f)", net, m.Longest.Length, want.LengthMM, d)
		}
	}
	// The clean subset is small -- seven nets -- because most of this board's
	// DDR routing carries router leftovers: a short stub where a track was
	// redrawn, or a pair of segments that overlap around a corner and form a
	// tiny loop. On those, the route and pcbnew's sum are genuinely different
	// quantities and neither is wrong. The subset is asserted non-trivial so
	// that a regression cannot quietly empty it.
	if checked < 7 {
		t.Fatalf("only %d clean complete nets checked; expected at least 7", checked)
	}
	t.Logf("route agrees with pcbnew to its printed precision on %d clean nets", checked)
}

// TestPathNeverMateriallyExceedsCopper is a sanity invariant. A route is
// copper plus, at each end, the straight line across the pad from its anchor to
// where the track lands -- so it can exceed the track copper only by about a
// pad radius per end, and never by more.
func TestPathNeverMateriallyExceedsCopper(t *testing.T) {
	b := loadBoard(t)
	e := New(b)
	var maxPad float64
	for _, p := range b.Pads {
		bx := p.Shape.Box()
		if r := math.Hypot(bx.MaxX-bx.MinX, bx.MaxY-bx.MinY) / 2; r > maxPad {
			maxPad = r
		}
	}
	for _, net := range ddrNets(b) {
		m := e.Measure(net)
		for _, p := range m.Paths {
			if p.Found && p.Length > m.TotalCopper+2*maxPad+1e-9 {
				t.Errorf("%s: route %s..%s is %.4f mm but the net has only %.4f mm of copper",
					net, p.From, p.To, p.Length, m.TotalCopper)
			}
		}
	}
}

// TestOverlapConnectivity checks the property that makes any of this work.
// pcbnew's router leaves endpoints up to a few tens of micrometres apart at
// some corners, and copper joins where it overlaps, not where coordinates
// match. Keying the graph on exact coordinates finds several DDR nets in a
// dozen pieces; here every point-to-point net must come out as one.
func TestOverlapConnectivity(t *testing.T) {
	b := loadBoard(t)
	e := New(b)
	for _, net := range ddrNets(b) {
		m := e.Measure(net)
		if len(m.Pads) != 2 {
			continue
		}
		if len(m.Islands) != 1 {
			t.Errorf("%s: copper found in %d islands, want 1: %v", net, len(m.Islands), m.Islands)
		}
	}
}

// TestFlyByFirstLegIsMeasurable is what per-leg matching rests on: the MPU to
// near-device leg exists on all 27 fly-by nets, including the clock, so the
// address and command group can be matched against the clock on that leg even
// though the far leg is unrouted.
func TestFlyByFirstLegIsMeasurable(t *testing.T) {
	b := loadBoard(t)
	e := New(b)
	n := 0
	for _, net := range ddrNets(b) {
		m := e.Measure(net)
		if len(m.Pads) != 4 {
			continue
		}
		leg := findPath(m, "U3", "U4")
		if !leg.Found {
			t.Errorf("%s: first leg not measurable", net)
			continue
		}
		n++
		if leg.Length < 5 || leg.Length > 60 {
			t.Errorf("%s: first leg %.3f mm is implausible", net, leg.Length)
		}
		if leg.Vias != 2 {
			t.Errorf("%s: first leg crosses %d vias, want 2", net, leg.Vias)
		}
		if leg.Delay <= 0 {
			t.Errorf("%s: first leg has no delay", net)
		}
	}
	if n != 27 {
		t.Errorf("measured %d first legs, want 27", n)
	}
}

func findPath(m *Measure, refA, refB string) Path {
	for _, p := range m.Paths {
		a, b := strings.Split(p.From, ".")[0], strings.Split(p.To, ".")[0]
		if (a == refA && b == refB) || (a == refB && b == refA) {
			return p
		}
	}
	return Path{}
}

// TestDelayModel checks the propagation model: a route that stays on an outer
// layer runs at the microstrip rate, and one that dives through vias into the
// stack is slower per millimetre.
func TestDelayModel(t *testing.T) {
	b := loadBoard(t)
	e := New(b)
	microstrip := b.Stackup.DelayPerMM("F.Cu", 0.09)

	dq := e.Measure("/ddr4/DDR_DQ0")
	if !dq.Longest.Found {
		t.Fatal("no DQ0 route")
	}
	if got := dq.Longest.Delay / dq.Longest.Length; math.Abs(got-microstrip) > 1e-9 {
		t.Errorf("DQ0 runs at %.4f ps/mm, want the F.Cu rate %.4f", got, microstrip)
	}
	for _, net := range []string{"/ddr4/DDR_A0", "/ddr4/DDR_CLK_P"} {
		m := e.Measure(net)
		leg := findPath(m, "U3", "U4")
		if !leg.Found {
			t.Fatalf("%s: no first leg", net)
		}
		if got := leg.Delay / leg.Length; got <= microstrip {
			t.Errorf("%s: %.4f ps/mm should exceed the microstrip rate %.4f", net, got, microstrip)
		}
	}
}

// TestPathBetweenIsOrderIndependent checks the pair lookup.
func TestPathBetweenIsOrderIndependent(t *testing.T) {
	b := loadBoard(t)
	m := New(b).Measure("/ddr4/DDR_DQ0")
	if len(m.Pads) != 2 {
		t.Fatalf("pads = %v", m.Pads)
	}
	a, c := m.PathBetween(m.Pads[0], m.Pads[1]), m.PathBetween(m.Pads[1], m.Pads[0])
	if !a.Found || a.Length != c.Length {
		t.Errorf("%+v vs %+v", a, c)
	}
	if got := m.PathBetween("nope", "nah"); got.Found {
		t.Error("unknown pair reported found")
	}
}

func TestMeasureUnknownNet(t *testing.T) {
	b := loadBoard(t)
	m := New(b).Measure("/nope")
	if m.Longest.Found || len(m.Pads) != 0 || m.TotalCopper != 0 || m.KiCadLength != 0 {
		t.Errorf("unknown net measured as %+v", m)
	}
	if !m.Complete {
		t.Error("a net with no pads should not be reported incomplete")
	}
}

func TestMeasureAllFilters(t *testing.T) {
	b := loadBoard(t)
	all := New(b).MeasureAll(func(n string) bool { return strings.HasPrefix(n, "/ddr4/DDR_") })
	if len(all) != 71 {
		t.Errorf("measured %d nets, want 71", len(all))
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if keys[0] != "/ddr4/DDR_A0" {
		t.Errorf("first net = %s", keys[0])
	}
}

func BenchmarkMeasureAllDDR(bench *testing.B) {
	b, err := board.Load("../demo-pcb/ai-vision.kicad_pcb")
	if err != nil {
		bench.Skip(err)
	}
	e := New(b)
	for bench.Loop() {
		e.MeasureAll(func(n string) bool { return strings.HasPrefix(n, "/ddr4/DDR_") })
	}
}

// TestSplittingATrackNeverShortensARoute is the test that caught the worst bug
// in this package.
//
// An earlier graph joined nearby copper with zero-length bridges between
// separate nodes. That let a route cut the corner where two tracks meet through
// a short connector segment: legal-looking, quietly wrong, and worth 0.52 mm on
// DDR_DQ13 -- a net with a dozen tuning corners, each leaking a little. It made
// the lanes that had already been tuned by hand look mismatched, and would have
// led the tuner to add half a millimetre of meander that was never needed.
//
// Dividing a track where another piece of copper lands on it is necessary for a
// branching net, and on a net that does not branch it must make no difference
// at all. So every two-pad net is measured both ways here. Any disagreement is
// a route taking a shortcut through geometry rather than following copper.
func TestSplittingATrackNeverShortensARoute(t *testing.T) {
	b := loadBoard(t)
	split := New(b)
	whole := New(b)
	whole.NoTJunctionSplits = true

	checked, worst := 0, 0.0
	var worstNet string
	for _, net := range ddrNets(b) {
		a := split.Measure(net)
		if len(a.Pads) != 2 || !a.Longest.Found {
			continue
		}
		c := whole.Measure(net)
		if !c.Longest.Found {
			t.Errorf("%s: measurable only with splits, so the net does branch", net)
			continue
		}
		checked++
		d := math.Abs(a.Longest.Length - c.Longest.Length)
		if d > worst {
			worst, worstNet = d, net
		}
		if d > 1e-6 {
			t.Errorf("%s: route is %.6f mm with track splitting and %.6f mm without (%.6f mm apart)",
				net, a.Longest.Length, c.Longest.Length, d)
		}
	}
	if checked != 44 {
		t.Fatalf("checked %d point-to-point nets, want 44", checked)
	}
	t.Logf("worst difference over %d nets: %.9f mm on %s", checked, worst, worstNet)
}

// TestRouteAccountsForAllCopperOrADeadEnd checks the other half of the same
// property. Where a route is shorter than the net's copper, the difference has
// to be copper the signal genuinely cannot travel down: a dead-end stub the
// router left behind. Those exist on this board -- a 0.07 mm spur off the DQ3
// pad, a 0.04 mm one off DQS0_N -- and excluding them is right. What would be
// wrong is a difference that is not accounted for that way.
func TestRouteAccountsForAllCopperOrADeadEnd(t *testing.T) {
	b := loadBoard(t)
	e := New(b)
	withStub, exact := 0, 0
	for _, net := range ddrNets(b) {
		m := e.Measure(net)
		if len(m.Pads) != 2 || !m.Longest.Found {
			continue
		}
		gap := m.TotalCopper - m.Longest.Length
		if gap < 1e-9 {
			exact++
			continue
		}
		withStub++
		// The unused copper must be made of dead ends, so it cannot exceed the
		// total length of the net's degree-one branches. Approximate that
		// generously by the sum of the tracks shorter than a millimetre, which
		// is what a router leaves behind; anything larger than that is a route
		// dodging real copper.
		var small float64
		for _, tr := range b.TracksOfNet(net) {
			if tr.Kind != board.KindVia && tr.Length(b.Stackup) < 1.0 {
				small += tr.Length(b.Stackup)
			}
		}
		if gap > small+1e-9 {
			t.Errorf("%s: route leaves %.4f mm of copper unused, more than the %.4f mm of short spurs on the net",
				net, gap, small)
		}
	}
	if exact+withStub != 44 {
		t.Fatalf("accounted for %d of 44 nets", exact+withStub)
	}
	t.Logf("%d nets use every millimetre; %d leave a router spur behind", exact, withStub)
}

// TestArcsAreMeasuredAlongTheCurve checks that the heavily tuned nets measure
// longer than their straight-line skeleton, i.e. that meander arcs contribute
// their curve length rather than their chord.
func TestArcsAreMeasuredAlongTheCurve(t *testing.T) {
	b := loadBoard(t)
	e := New(b)
	tuned := 0
	for _, net := range ddrNets(b) {
		m := e.Measure(net)
		if m.Arcs == 0 {
			continue
		}
		tuned++
		var chord, curve float64
		for _, tr := range b.TracksOfNet(net) {
			if tr.Kind != board.KindArc {
				continue
			}
			chord += tr.Start.Dist(tr.End)
			curve += tr.Length(b.Stackup)
		}
		if curve <= chord {
			t.Errorf("%s: arcs measure %.4f along the curve but %.4f as chords", net, curve, chord)
		}
	}
	if tuned < 10 {
		t.Fatalf("only %d nets carry tuning arcs", tuned)
	}
}
