package ddr

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
	"github.com/embeddedci-com/pcb-autorouter/route"
	"github.com/embeddedci-com/pcb-autorouter/tune"
)

// A fly-by chain that is actually finished.
//
// The demo board cannot ask any of these questions: its second hop does not
// exist on a single net, so everything below -- measuring a hop, matching it
// against the clock over that same hop, and not confusing one hop with another
// -- has never run on real copper. The whole point of per-leg matching is what
// happens on the second leg, and until now the only thing tested about it was
// that it was missing.
//
// So the chain is built here, complete and deliberately mismatched, and the
// plan is asked what it makes of it.

// routedChain lays a complete fly-by chain: controller to the near device, on
// to the far one, and out to a terminator, with a detour on each net so the
// legs come out at chosen lengths.
//
// detour is extra millimetres to add to that net on that hop, drawn as a
// square-cornered excursion the way a hand-routed board would.
func routedChain(t *testing.T, legOne, legTwo map[string]float64) *chainBoard {
	t.Helper()
	s := twoDeviceBoard(t, "U9", "U10", 40, 90)
	for _, net := range flyByNets {
		hopWithDetour(s, net, "U1", "U9", legOne[net])
		hopWithDetour(s, net, "U9", "U10", legTwo[net])
		s.hop(net, "U10", terminatorFor(net))
	}
	return s
}

func terminatorFor(net string) string {
	for i, n := range flyByNets {
		if n == net {
			return fmt.Sprintf("R%d", i+1)
		}
	}
	return ""
}

// hopWithDetour draws one hop, with `extra` millimetres of detour folded in as
// a rectangular excursion.
func hopWithDetour(s *chainBoard, net, from, to string, extra float64) {
	a, ok := s.at[from+"/"+net]
	b, ok2 := s.at[to+"/"+net]
	if !ok || !ok2 {
		panic("hopWithDetour: " + net + " is not on both " + from + " and " + to)
	}
	if extra <= 0 {
		s.track(net, a, b)
		return
	}
	// A bump on the straight line between the pads, perpendicular to it: out
	// by h, along by g, back by h. That adds exactly 2h whatever direction the
	// hop runs in, which the first version did not -- it assumed both pads sat
	// at the same height, and once the bus had rows the "4 mm detour" measured
	// 11.5.
	//
	// The two out-and-back legs are a millimetre apart. Copper is wider than a
	// line and connectivity here is by overlap, so legs a micrometre apart are
	// one piece of copper and the detour is shorted out across them.
	const legGap = 1.0
	h := extra / 2
	d := b.Sub(a)
	l := d.Len()
	if l <= legGap {
		panic("hopWithDetour: the hop is shorter than the detour needs")
	}
	dir := d.Norm()
	perp := dir.Perp()
	// Away from the bus. Its rows run in +y and the fly-by nets are the
	// highest of them, so an excursion further out crosses nothing.
	if perp.Y < 0 {
		perp = perp.Mul(-1)
	}
	m1 := a.Add(dir.Mul(l/2 - legGap/2))
	m2 := a.Add(dir.Mul(l/2 + legGap/2))
	s.track(net, a, m1)
	s.track(net, m1, m1.Add(perp.Mul(h)))
	s.track(net, m1.Add(perp.Mul(h)), m2.Add(perp.Mul(h)))
	s.track(net, m2.Add(perp.Mul(h)), m2)
	s.track(net, m2, b)
}

func legOf(plan *Plan, leg string) *Group {
	for _, g := range plan.Groups {
		if g.Kind == AddressCommand && g.Leg == leg {
			return g
		}
	}
	return nil
}

// The chain is complete, so there is a group at each device, each measured
// from the controller the way ST's sheet does: the controller to the first
// memory, and the controller to the second.
func TestACompleteChainIsPlannedPerDevice(t *testing.T) {
	// A0 takes a 4 mm detour on the first hop; A1 takes 6 mm on the second.
	s := routedChain(t,
		map[string]float64{"/d/DDR_A0": 4},
		map[string]float64{"/d/DDR_A1": 6},
	)
	_, plan := classifyChain(t, s)

	if plan.Chain == nil || plan.Chain.FirstGap() != nil {
		t.Fatalf("the chain is complete but the plan says otherwise: %+v", plan.Chain)
	}
	one, two := legOf(plan, "U1->U9"), legOf(plan, "U1->U10")
	if one == nil || two == nil {
		var legs []string
		for _, g := range plan.Groups {
			legs = append(legs, g.Leg)
		}
		t.Fatalf("a group per device should be planned; got %v", legs)
	}
	// The first device is about 30 mm out and the second about 80.
	if one.ReferenceLength > 45 || two.ReferenceLength < one.ReferenceLength+30 {
		t.Errorf("clock to U9 %.3f mm, to U10 %.3f mm: the second is measured from the controller",
			one.ReferenceLength, two.ReferenceLength)
	}

	dev := func(g *Group, net string) float64 {
		for _, m := range g.Members {
			if m.Net == net {
				if !m.Routed {
					t.Fatalf("%s is not routed on %s", net, g.Leg)
				}
				return m.Length - g.ReferenceLength
			}
		}
		t.Fatalf("%s is not a member of %s", net, g.Leg)
		return 0
	}
	// A detour before the first device is still there at the second; one
	// after it shows at the second device only.
	for _, c := range []struct {
		g    *Group
		net  string
		want float64
	}{{one, "/d/DDR_A0", 4}, {two, "/d/DDR_A0", 4}, {one, "/d/DDR_A1", 0}, {two, "/d/DDR_A1", 6}} {
		if got := dev(c.g, c.net); math.Abs(got-c.want) > 0.05 {
			t.Errorf("%s at %s deviates %.3f mm, want %.3f", c.net, c.g.Leg, got, c.want)
		}
	}
}

// A line short before the first device is fixed there, and that length reaches
// the second device too, so it is not asked for twice.
func TestLengthAddedForTheFirstDeviceCountsAtTheSecond(t *testing.T) {
	s := routedChain(t, map[string]float64{"/d/DDR_CLK_P": 4, "/d/DDR_CLK_N": 4}, nil)
	_, plan := classifyChain(t, s)
	one, two := legOf(plan, "U1->U9"), legOf(plan, "U1->U10")
	if one == nil || two == nil {
		t.Fatal("a group per device should be planned")
	}
	for i, m := range two.Members {
		if !m.Routed || m.Reference {
			continue
		}
		if up := one.Members[i]; math.Abs(up.Need-4) > 0.05 {
			t.Errorf("%s needs %.3f mm at U9, want 4", up.Net, up.Need)
		}
		if m.Need > 0.05 {
			t.Errorf("%s is asked for %.3f mm more at U10, where the 4 mm added before U9 already arrives", m.Net, m.Need)
		}
	}
}

// The reference on each hop is the clock, measured over that same hop. Matching
// a hop's data against the clock's whole length is the mistake per-leg matching
// exists to prevent.
func TestEachHopIsMatchedToTheClockOverThatHop(t *testing.T) {
	s := routedChain(t, nil, map[string]float64{"/d/DDR_CLK_P": 8, "/d/DDR_CLK_N": 8})
	_, plan := classifyChain(t, s)

	for _, leg := range []string{"U1->U9", "U1->U10"} {
		g := legOf(plan, leg)
		if g == nil {
			t.Fatalf("no group for %s", leg)
		}
		if !strings.Contains(g.Reference, "CLK") {
			t.Errorf("%s is matched to %q, not the clock", leg, g.Reference)
		}
		// The clock detours by 8 mm on the second hop, so on that hop
		// everything else is short by 8 and on the first hop nothing is.
		for _, m := range g.Members {
			if !m.Routed || strings.Contains(m.Net, "CLK") {
				continue
			}
			want := 0.0
			if leg == "U1->U10" {
				want = 8
			}
			if math.Abs(m.Need-want) > 0.05 {
				t.Errorf("%s on %s needs %.3f mm, want %.3f", m.Net, leg, m.Need, want)
			}
		}
	}
}

// Tuning a hop adds copper to that hop and leaves the other one alone. A net
// that is right on one hop and short on the other is the case the whole
// per-leg decision was made for, and nothing until now has tested it.
func TestTuningOneHopLeavesTheOtherAlone(t *testing.T) {
	s := routedChain(t, nil, map[string]float64{"/d/DDR_CLK_P": 5, "/d/DDR_CLK_N": 5})
	b := s.build(t)
	iface, err := Classify(b, Options{NetPrefix: "/d/"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := BuildPlan(iface, netlen.New(b), DefaultRules())
	if err != nil {
		t.Fatal(err)
	}

	one, two := legOf(before, "U1->U9"), legOf(before, "U1->U10")
	if one == nil || two == nil {
		t.Fatal("both hops should be planned")
	}
	// Hop one is already matched; hop two is 5 mm short on the address lines.
	if n := one.OutOfTolerance(); n != 0 {
		t.Errorf("%d net(s) out of tolerance on the matched hop", n)
	}
	if n := two.OutOfTolerance(); n == 0 {
		t.Fatal("the short hop reports nothing out of tolerance")
	}

	// The tracks a fix would go on belong to that hop and no other.
	for _, m := range two.Members {
		if m.Need <= 0 || len(m.PathTracks) == 0 {
			continue
		}
		on := map[string]bool{}
		for _, id := range m.PathTracks {
			on[id] = true
		}
		for _, om := range one.Members {
			if om.Net != m.Net {
				continue
			}
			for _, id := range om.PathTracks {
				if on[id] {
					t.Errorf("%s: track %s is on both hops, so lengthening one would move the other",
						m.Net, id)
				}
			}
		}
	}
}

// The whole point, end to end: a chain whose second hop is short, tuned, and
// re-measured from the copper afterwards.
//
// Every piece of this has been tested on its own. None of it has been tested
// together on a finished chain, because no board available to this project has
// one -- and "the second hop is matched by lengthening only the second hop" is
// the claim the entire per-leg design rests on.
func TestAShortHopIsTunedAndTheOtherHopDoesNotMove(t *testing.T) {
	// The clock takes a 5 mm detour on the second hop, so every address line
	// on that hop is 5 mm short of it. The first hop is already matched.
	s := routedChain(t, nil, map[string]float64{"/d/DDR_CLK_P": 5, "/d/DDR_CLK_N": 5})
	b := s.build(t)
	proj := board.Defaults()
	proj.MinClearance = 0.2
	proj.MinTrackWidth = 0.09
	proj.Classes = []board.NetClass{{Name: "Default", Clearance: 0.2, TrackWidth: 0.09}}

	iface, err := Classify(b, Options{NetPrefix: "/d/"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := BuildPlan(iface, netlen.New(b), DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	hopOneBefore := lengthsOn(t, before, "U1->U9")

	two := legOf(before, "U1->U10")
	if two == nil {
		t.Fatal("no second hop")
	}
	tuner := tune.NewTuner(b, proj, tune.DefaultStyle())
	var tuned int
	for _, m := range two.Members {
		if m.Need <= 0 || !m.Routed {
			continue
		}
		on := map[string]bool{}
		for _, id := range m.PathTracks {
			on[id] = true
		}
		res, err := tuner.Tune(m.Net, m.Need, on)
		if err != nil {
			t.Fatalf("tuning %s: %v", m.Net, err)
		}
		if res.Shortfall > 1e-3 {
			t.Errorf("%s fell %.4f mm short of its %.3f mm on an empty board",
				m.Net, res.Shortfall, m.Need)
		}
		tuned++
	}
	if tuned == 0 {
		t.Fatal("nothing was tuned, so this proves nothing")
	}

	// Re-measured from the edited copper, not from the tuner's bookkeeping.
	after, err := BuildPlan(iface, netlen.New(b), DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if n := legOf(after, "U1->U10").OutOfTolerance(); n != 0 {
		for _, m := range legOf(after, "U1->U10").Members {
			if m.Routed && !m.InTolerance {
				t.Logf("  %s deviates %.4f mm", m.Net, m.Deviation)
			}
		}
		t.Errorf("%d net(s) still out of tolerance on the tuned hop", n)
	}

	// And the hop nobody touched measures exactly what it did before. This is
	// the one that would fail if a meander had been folded into shared copper,
	// or into the wrong leg of the chain.
	for net, was := range hopOneBefore {
		now := lengthsOn(t, after, "U1->U9")[net]
		if math.Abs(now-was) > 1e-6 {
			t.Errorf("%s on the untouched hop went from %.6f to %.6f mm", net, was, now)
		}
	}
	if n := legOf(after, "U1->U9").OutOfTolerance(); n != 0 {
		t.Errorf("%d net(s) out of tolerance on the hop that was already matched", n)
	}
}

func lengthsOn(t *testing.T, plan *Plan, leg string) map[string]float64 {
	t.Helper()
	g := legOf(plan, leg)
	if g == nil {
		t.Fatalf("no group for %s", leg)
	}
	out := map[string]float64{}
	for _, m := range g.Members {
		if m.Routed {
			out[m.Net] = m.Length
		}
	}
	return out
}

// Making the hop that is missing, and then matching it.
//
// This is the whole fly-by story in one test, and it is the one thing no board
// available to this project can exercise: create the copper for a hop that does
// not exist, confirm the chain is then complete, and match it against the clock
// over that hop. Until the demo board's second hop is drawn, this is the only
// evidence that the two halves fit together.
func TestTheMissingHopCanBeRoutedAndThenMatched(t *testing.T) {
	s := twoDeviceBoard(t, "U9", "U10", 40, 90)
	// Only the first hop, and the clock takes a detour on it so there is
	// something to match afterwards.
	for _, net := range flyByNets {
		extra := 0.0
		if strings.Contains(net, "CLK") {
			extra = 3
		}
		hopWithDetour(s, net, "U1", "U9", extra)
	}
	b := s.build(t)
	proj := board.Defaults()
	proj.MinClearance = 0.2
	proj.MinTrackWidth = 0.09
	proj.Classes = []board.NetClass{{Name: "Default", Clearance: 0.2, TrackWidth: 0.09}}

	iface, err := Classify(b, Options{NetPrefix: "/d/"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := BuildPlan(iface, netlen.New(b), DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	gap := before.Chain.FirstGap()
	if gap == nil {
		t.Fatal("the second hop is not drawn, so the chain has a gap")
	}
	if gap.From != "U9" || gap.To != "U10" {
		t.Fatalf("the gap is %s->%s", gap.From, gap.To)
	}
	if legOf(before, "U1->U10") != nil {
		t.Error("a hop with no copper on it should not be planned")
	}

	// Route it.
	// Both missing hops: the device-to-device one and the stub out to the
	// terminator, which is the rest of the chain and is just as absent.
	var reqs []route.Request
	for _, net := range flyByNets {
		from, to := padOnPart(b, net, "U9"), padOnPart(b, net, "U10")
		term := padOnPart(b, net, terminatorFor(net))
		if from == "" || to == "" || term == "" {
			t.Fatalf("%s is not on both devices and a terminator", net)
		}
		reqs = append(reqs,
			route.Request{Net: net, From: from, To: to},
			route.Request{Net: net, From: to, To: term})
	}
	r, err := route.New(b, proj, reqs, route.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	results, err := r.Route(reqs)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if !res.Routed {
			t.Fatalf("%s could not be routed: %s", res.Net, res.Reason)
		}
	}

	// The chain is now complete, and the hop that did not exist is planned.
	after, err := BuildPlan(iface, netlen.New(b), DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if g := after.Chain.FirstGap(); g != nil {
		t.Fatalf("still a gap at %s->%s after routing it", g.From, g.To)
	}
	two := legOf(after, "U1->U10")
	if two == nil {
		t.Fatal("the new hop is not planned")
	}
	if !strings.Contains(two.Reference, "CLK") {
		t.Errorf("the new hop is matched to %q", two.Reference)
	}
	for _, m := range two.Members {
		if !m.Routed {
			t.Errorf("%s is not routed on the hop that was just routed", m.Net)
		}
	}

	// And the first hop, which nobody touched, still says what it said.
	if len(lengthsOn(t, after, "U1->U9")) != len(lengthsOn(t, before, "U1->U9")) {
		t.Error("the first hop lost or gained a member")
	}
	for net, was := range lengthsOn(t, before, "U1->U9") {
		if now := lengthsOn(t, after, "U1->U9")[net]; math.Abs(now-was) > 1e-6 {
			t.Errorf("%s on the first hop went from %.6f to %.6f mm", net, was, now)
		}
	}
	t.Logf("routed %d connections; the new leg measures %.3f mm against the clock",
		len(results), two.ReferenceLength)
}

// padOnPart is the net's pad on a named component.
func padOnPart(b *board.Board, net, ref string) string {
	for _, p := range b.PadsOfNet(net) {
		if p.Ref == ref {
			return p.ID()
		}
	}
	return ""
}
