package tune

import (
	"math"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/ddr"
	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

// TestHeadroomBoundsWhatTuningAchieves is the contract the report depends on.
//
// Headroom is shown to the user as "this is what the space beside this route
// can hold". If tuning could exceed it the figure would be misleading in the
// worst direction -- the user would be told to reroute when tuning would have
// done -- and if it were wildly above what tuning achieves it would be useless.
func TestHeadroomBoundsWhatTuningAchieves(t *testing.T) {
	for _, c := range []struct {
		name   string
		build  func(*synth) string
		demand float64
	}{
		{"open track", func(s *synth) string {
			_, net := straightTrackOn(s, 20)
			return net
		}, 100},
		{"open on one side", func(s *synth) string {
			_, net := straightTrackOn(s, 20)
			s.blocker("L", 5, 35, synthClr)
			return net
		}, 100},
		{"open in the middle only", func(s *synth) string {
			_, net := straightTrackOn(s, 20)
			s.blocker("L1", 10, 16, synthClr)
			s.blocker("L2", 24, 30, synthClr)
			s.track("R1", "F.Cu", geom.Pt{X: 10, Y: 50 - synthClr - synthW}, geom.Pt{X: 16, Y: 50 - synthClr - synthW}, synthW)
			s.track("R2", "F.Cu", geom.Pt{X: 24, Y: 50 - synthClr - synthW}, geom.Pt{X: 30, Y: 50 - synthClr - synthW}, synthW)
			return net
		}, 100},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newSynth()
			net := c.build(s)
			_, _, tu := s.build(t, synthClr, synthW)

			room := tu.Headroom(net, nil)
			if room <= 0 {
				t.Fatalf("no headroom reported")
			}
			res, err := tu.Tune(net, c.demand, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Added > room+1e-6 {
				t.Errorf("tuning added %.4f mm, more than the %.4f mm of headroom reported", res.Added, room)
			}
			// And not so pessimistic as to be useless.
			if res.Added < room*0.5 {
				t.Errorf("tuning added %.4f mm of %.4f mm reported; the figure is too optimistic to act on",
					res.Added, room)
			}
			t.Logf("headroom %.3f mm, tuning achieved %.3f mm", room, res.Added)
		})
	}
}

func TestHeadroomIsZeroWhenBoxedIn(t *testing.T) {
	s := newSynth()
	_, net := straightTrackOn(s, 20)
	s.blocker("L", 5, 35, synthClr)
	s.track("R", "F.Cu", geom.Pt{X: 5, Y: 50 - synthClr - synthW}, geom.Pt{X: 35, Y: 50 - synthClr - synthW}, synthW)
	_, _, tu := s.build(t, synthClr, synthW)

	if room := tu.Headroom(net, nil); room != 0 {
		t.Errorf("headroom %.4f beside a track boxed in at minimum clearance", room)
	}
	if room := tu.Headroom("does-not-exist", nil); room != 0 {
		t.Errorf("headroom %.4f for a net that is not on the board", room)
	}
}

func TestHeadroomRespectsTheRouteRestriction(t *testing.T) {
	s := newSynth()
	_, net := straightTrackOn(s, 20)
	_, _, tu := s.build(t, synthClr, synthW)

	all := tu.Headroom(net, nil)
	if all <= 0 {
		t.Fatal("expected headroom on an open track")
	}
	// Restricted to copper that is not on the board, there is nowhere to go.
	if got := tu.Headroom(net, map[string]bool{"nope": true}); got != 0 {
		t.Errorf("headroom %.4f when restricted to a track that does not exist", got)
	}
}

// TestNeedsRerouteSeparatesTheTwoKindsOfShortfall checks the judgement the
// report hangs on: a net short of space is a different problem from one asking
// for more length than its route could ever carry.
func TestNeedsRerouteSeparatesTheTwoKindsOfShortfall(t *testing.T) {
	for _, c := range []struct {
		name         string
		length, need float64
		routed       bool
		want         bool
	}{
		{"a tenth of its length", 20, 2, true, false},
		{"a quarter exactly", 20, 5, true, false},
		{"a third", 20, 7, true, true},
		{"most of its length", 16.888, 14.251, true, true},
		{"nothing needed", 20, 0, true, false},
		{"not routed", 20, 10, false, false},
		{"zero length", 0, 1, true, false},
	} {
		m := ddr.Member{Routed: c.routed, Length: c.length, Need: c.need}
		if got := m.NeedsReroute(); got != c.want {
			t.Errorf("%s: NeedsReroute = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestMeasureHeadroomFillsTheMembersThatNeedIt checks that the plan only pays
// for the measurement where it means something, and caches per net and leg.
func TestMeasureHeadroomFillsTheMembersThatNeedIt(t *testing.T) {
	b, proj, _ := fixture(t)
	iface, err := ddr.Classify(b, ddr.Options{NetPrefix: "/ddr4/"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ddr.BuildPlan(iface, netlen.New(b), ddr.DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	counted := &countingHeadroomer{inner: NewTuner(b, proj, DefaultStyle())}
	plan.MeasureHeadroom(counted)

	var withRoom, needing, inTolerance int
	for _, g := range plan.Groups {
		for _, m := range g.Members {
			switch {
			case !m.Routed || m.InTolerance || m.Need <= 1e-6:
				inTolerance++
				if m.Headroom != 0 {
					t.Errorf("%s does not need length but has headroom %.4f", m.Net, m.Headroom)
				}
			default:
				needing++
				if m.Headroom > 0 {
					withRoom++
				}
			}
		}
	}
	if needing == 0 {
		t.Fatal("no member needs length on this board")
	}
	if withRoom == 0 {
		t.Error("no member has any headroom; the measurement is not working")
	}
	// One measurement per net per leg, not one per member.
	if counted.calls > needing {
		t.Errorf("%d measurements for %d members needing length; the cache is not working",
			counted.calls, needing)
	}
	t.Logf("%d members need length, %d have room; %d measurements taken (%d in tolerance, skipped)",
		needing, withRoom, counted.calls, inTolerance)
}

type countingHeadroomer struct {
	inner *Tuner
	calls int
}

func (c *countingHeadroomer) Headroom(net string, on map[string]bool) float64 {
	c.calls++
	return c.inner.Headroom(net, on)
}

// TestHeadroomOnTheDemoBoardTellsTheTwoProblemsApart is the case that motivated
// all of this: on one lane the shortfall is a lack of space, and on the fly-by
// bus it is a route that is far too short for what it is matched against.
func TestHeadroomOnTheDemoBoardTellsTheTwoProblemsApart(t *testing.T) {
	b, proj, _ := fixture(t)
	iface, _ := ddr.Classify(b, ddr.Options{NetPrefix: "/ddr4/"})
	plan, _ := ddr.BuildPlan(iface, netlen.New(b), ddr.DefaultRules())
	tu := NewTuner(b, proj, DefaultStyle())
	plan.MeasureHeadroom(tu)

	find := func(group, net string) (ddr.Member, bool) {
		for _, g := range plan.Groups {
			if g.Name != group {
				continue
			}
			for _, m := range g.Members {
				if m.Net == net {
					return m, true
				}
			}
		}
		return ddr.Member{}, false
	}

	// DQ24 is short of nothing: it has room for its whole requirement.
	dq24, ok := find("byte lane 3", "/ddr4/DDR_DQ24")
	if !ok {
		t.Fatal("DQ24 not in byte lane 3")
	}
	if dq24.NeedsReroute() {
		t.Errorf("DQ24 needs %.3f of %.3f mm; that is a tuning job, not a reroute", dq24.Need, dq24.Length)
	}
	if dq24.Headroom < dq24.Need {
		t.Errorf("DQ24 needs %.3f mm and has %.3f mm of room; profiling should find it all",
			dq24.Need, dq24.Headroom)
	}

	// A11 is not short of space at all: it is asked to grow by most of its own
	// length, which no meander supplies.
	a11, ok := find("address/command U3->U4", "/ddr4/DDR_A11")
	if !ok {
		t.Fatal("A11 not in the address group")
	}
	if !a11.NeedsReroute() {
		t.Errorf("A11 needs %.3f mm on a %.3f mm route (%.0f%%) and should be called a reroute",
			a11.Need, a11.Length, 100*a11.Need/a11.Length)
	}
	t.Logf("DQ24: needs %.3f, room %.3f, reroute=%v", dq24.Need, dq24.Headroom, dq24.NeedsReroute())
	t.Logf("A11:  needs %.3f of a %.3f mm route, reroute=%v", a11.Need, a11.Length, a11.NeedsReroute())

	// And there is at least one of each kind, so the report is never all one
	// colour by accident.
	var reroutes, tight int
	for _, g := range plan.Groups {
		for _, m := range g.Members {
			if !m.Routed || m.InTolerance || m.Need <= 1e-6 {
				continue
			}
			if m.NeedsReroute() {
				reroutes++
			} else if m.Headroom+1e-6 < m.Need {
				tight++
			}
		}
	}
	if reroutes == 0 || tight == 0 {
		t.Errorf("%d reroute cases and %d short-of-room cases; expected both on this board", reroutes, tight)
	}
	_ = math.Abs
}

// straightTrackOn is straightTrack against an existing synth, so a test can add
// obstacles around it.
func straightTrackOn(s *synth, length float64) (*synth, string) {
	const net = "N1"
	s.pad(net, geom.Pt{X: 10, Y: 50}, synthW)
	s.pad(net, geom.Pt{X: 10 + length, Y: 50}, synthW)
	s.track(net, "F.Cu", geom.Pt{X: 10, Y: 50}, geom.Pt{X: 10 + length, Y: 50}, synthW)
	return s, net
}
