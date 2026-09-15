package ddr

import (
	"fmt"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

// A board built to put the fly-by chain in question.
//
// The demo board cannot ask this: its devices are U4 and U5, and U4 is both the
// nearer one and the earlier one alphabetically, so an order taken from the
// designators and an order taken from the placement agree and neither is
// tested. U9 and U10 disagree -- "U10" sorts before "U9" -- which is a
// perfectly ordinary way to number two DRAMs and the case that silently
// compared the wrong spans.
// rowPitch is how far apart the bus's rows sit. Wide enough that a track has
// room to meander between its neighbours, which is what makes this fixture
// usable for tuning and not only for measuring.
const rowPitch = 0.8

type chainBoard struct {
	items []string
	uid   int

	// at records where each pad landed, keyed by "ref/net", so a track can be
	// drawn between two pads rather than between two footprint origins. The
	// first version of this fixture drew to the origins, every net came out
	// unrouted, and the failure looked like a bug in the chain walk.
	at map[string]geom.Pt
}

func (s *chainBoard) next() string {
	s.uid++
	return fmt.Sprintf("%08x-0000-4000-8000-00000000000c", s.uid)
}

// part places a footprint whose pads sit on the given nets, spread down a
// column.
//
// A column rather than a row, so that a net's copper runs along its own line
// from one part to the next and the bus has rows the way a real one does. The
// first version of this spread the pads along a row, which put every net of
// the bus on the same line, on top of each other -- fine for measuring a
// length, useless for anything that needs room beside a track.
func (s *chainBoard) part(ref string, at geom.Pt, nets []string) {
	if s.at == nil {
		s.at = map[string]geom.Pt{}
	}
	var pads []string
	for i, net := range nets {
		s.at[ref+"/"+net] = geom.Pt{X: at.X, Y: at.Y + float64(i)*rowPitch}
		pads = append(pads, fmt.Sprintf(
			`		(pad "%d" smd rect (at 0 %g) (size 0.3 0.3) (layers "F.Cu") (net "%s") (uuid "%s"))`,
			i+1, float64(i)*rowPitch, net, s.next()))
	}
	s.items = append(s.items, fmt.Sprintf(`	(footprint "t:dev"
		(layer "F.Cu")
		(uuid "%s")
		(at %g %g)
		(attr smd)
		(property "Reference" "%s" (at 0 0) (layer "F.SilkS") (uuid "%s") (effects (font (size 1 1) (thickness 0.15))))
%s
	)`, s.next(), at.X, at.Y, ref, s.next(), strings.Join(pads, "\n")))
}

func (s *chainBoard) track(net string, a, b geom.Pt) {
	s.items = append(s.items, fmt.Sprintf(
		`	(segment (start %g %g) (end %g %g) (width 0.09) (layer "F.Cu") (net "%s") (uuid "%s"))`,
		a.X, a.Y, b.X, b.Y, net, s.next()))
}

// hop draws one net's copper from one part's pad to another's, the way a
// fly-by chain is routed: pad to pad, one hop at a time.
func (s *chainBoard) hop(net, from, to string) {
	a, ok := s.at[from+"/"+net]
	b, ok2 := s.at[to+"/"+net]
	if !ok || !ok2 {
		panic("chainBoard.hop: " + net + " is not on both " + from + " and " + to)
	}
	s.track(net, a, b)
}

func (s *chainBoard) build(t *testing.T) *board.Board {
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
	(gr_rect (start 0 0) (end 200 100) (stroke (width 0.05) (type default)) (fill none) (layer "Edge.Cuts") (uuid "ffffffff-0000-4000-8000-00000000000e"))
` + strings.Join(s.items, "\n") + "\n)\n"
	b, err := board.Parse([]byte(src), "chain.kicad_pcb")
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	return b
}

// laneNetsFor is one byte lane's worth of nets, plus the fly-by nets that every
// device sees.
func laneNetsFor(lane int) []string {
	var out []string
	for i := 0; i < 8; i++ {
		out = append(out, fmt.Sprintf("/d/DDR_DQ%d", lane*8+i))
	}
	return append(out,
		fmt.Sprintf("/d/DDR_DQS%d_P", lane), fmt.Sprintf("/d/DDR_DQS%d_N", lane),
		fmt.Sprintf("/d/DDR_DQM%d", lane))
}

var flyByNets = []string{"/d/DDR_A0", "/d/DDR_A1", "/d/DDR_CLK_P", "/d/DDR_CLK_N"}

// twoDeviceBoard places a controller and two DRAMs at chosen distances, with
// the near one named so that sorting the designators gets the chain backwards.
func twoDeviceBoard(t *testing.T, nearRef, farRef string, nearX, farX float64) *chainBoard {
	t.Helper()
	s := &chainBoard{}
	s.part("U1", geom.Pt{X: 10, Y: 50}, append(append(append([]string{}, laneNetsFor(0)...), laneNetsFor(1)...), flyByNets...))
	s.part(nearRef, geom.Pt{X: nearX, Y: 50}, append(append([]string{}, laneNetsFor(0)...), flyByNets...))
	s.part(farRef, geom.Pt{X: farX, Y: 50}, append(append([]string{}, laneNetsFor(1)...), flyByNets...))
	// A terminator per fly-by net, past the far device.
	for i, net := range flyByNets {
		s.part(fmt.Sprintf("R%d", i+1), geom.Pt{X: farX + 20, Y: 50 + float64(i)}, []string{net})
	}
	return s
}

func classifyChain(t *testing.T, s *chainBoard) (*Interface, *Plan) {
	t.Helper()
	b := s.build(t)
	iface, err := Classify(b, Options{NetPrefix: "/d/"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(iface, netlen.New(b), DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	return iface, plan
}

// The order the chain runs in decides which span every address length is
// compared over. Taking it from the reference designators is a guess, and this
// is the board where the guess is wrong.
func TestChainOrderComesFromThePlacementNotTheDesignators(t *testing.T) {
	// U9 is 20 mm from the controller, U10 is 60 mm. Sorting the names puts
	// U10 first.
	s := twoDeviceBoard(t, "U9", "U10", 30, 70)
	for _, net := range flyByNets {
		s.hop(net, "U1", "U9")
	}
	iface, plan := classifyChain(t, s)

	if want := []string{"U9", "U10"}; strings.Join(iface.Devices, ",") != strings.Join(want, ",") {
		t.Errorf("chain order %v, want %v: U9 is the nearer device", iface.Devices, want)
	}
	if plan.Chain == nil {
		t.Fatal("no chain in the plan")
	}
	if got := strings.Join(plan.Chain.Order, " -> "); got != "U1 -> U9 -> U10" {
		t.Errorf("chain %q", got)
	}
	if !strings.Contains(plan.Chain.OrderFrom, "how far each device sits from U1") {
		t.Errorf("the report does not say how the order was decided: %q", plan.Chain.OrderFrom)
	}
	// And the hops the plan measures over follow it.
	var legs []string
	for _, g := range plan.Groups {
		if g.Kind == AddressCommand {
			legs = append(legs, g.Leg)
		}
	}
	if len(legs) == 0 {
		t.Fatal("no address/command groups")
	}
	if legs[0] != "U1->U9" {
		t.Errorf("first hop measured is %q, want U1->U9", legs[0])
	}
}

// Where the copper joining the devices exists, it is the authority: a chain
// that runs somewhere the placement did not suggest is still the chain.
func TestRoutedCopperOverridesThePlacementOrder(t *testing.T) {
	// U10 is the nearer part, but the copper runs the chain through U9 first.
	s := twoDeviceBoard(t, "U10", "U9", 30, 70)
	for _, net := range flyByNets {
		// Controller to the far part, then back to the near one: a placement
		// nobody would choose, and a chain the copper states plainly.
		s.hop(net, "U1", "U9")
		s.hop(net, "U9", "U10")
	}
	_, plan := classifyChain(t, s)
	if plan.Chain == nil {
		t.Fatal("no chain in the plan")
	}
	if got := strings.Join(plan.Chain.Order, " -> "); got != "U1 -> U9 -> U10" {
		t.Errorf("chain %q, want U1 -> U9 -> U10: that is where the copper goes", got)
	}
	if !strings.Contains(plan.Chain.Note, "not the order their placement suggested") {
		t.Errorf("the disagreement was not reported: %q", plan.Chain.Note)
	}
}

// A hop the board does not have is the finding, not a detail. It has to be
// named, and counted, because KiCad reports nothing about it.
func TestAMissingHopIsReported(t *testing.T) {
	s := twoDeviceBoard(t, "U9", "U10", 30, 70)
	// Only the first hop is routed.
	for _, net := range flyByNets {
		s.hop(net, "U1", "U9")
	}
	_, plan := classifyChain(t, s)
	gap := plan.Chain.FirstGap()
	if gap == nil {
		t.Fatal("a chain with one hop routed of three reported no gap")
	}
	if gap.From != "U9" || gap.To != "U10" {
		t.Errorf("first gap is %s->%s, want U9->U10", gap.From, gap.To)
	}
	if gap.Nets != 0 {
		t.Errorf("the missing hop claims %d net(s)", gap.Nets)
	}
	first := plan.Chain.Hops[0]
	if !first.Routed() {
		t.Errorf("the routed hop %s->%s reports %d of %d nets", first.From, first.To, first.Nets, first.Of)
	}
}

// A fully routed chain says so, and says nothing alarming.
func TestACompleteChainIsReportedAsComplete(t *testing.T) {
	s := twoDeviceBoard(t, "U9", "U10", 30, 70)
	for i, net := range flyByNets {
		s.hop(net, "U1", "U9")
		s.hop(net, "U9", "U10")
		s.hop(net, "U10", fmt.Sprintf("R%d", i+1))
	}
	_, plan := classifyChain(t, s)
	if gap := plan.Chain.FirstGap(); gap != nil {
		t.Errorf("a fully routed chain reports %s->%s missing (%d of %d nets)",
			gap.From, gap.To, gap.Nets, gap.Of)
	}
	if plan.Chain.Note != "" {
		t.Errorf("a fully routed chain has a note: %q", plan.Chain.Note)
	}
	if n := len(plan.Chain.Hops); n != 3 {
		t.Errorf("%d hops, want controller->near, near->far, far->termination", n)
	}
}
