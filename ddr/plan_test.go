package ddr

import (
	"math"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

func plan(t *testing.T, r Rules) *Plan {
	t.Helper()
	b := loadBoard(t)
	iface, err := Classify(b, Options{NetPrefix: "/ddr4/"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(iface, netlen.New(b), r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func group(p *Plan, name string) *Group {
	for _, g := range p.Groups {
		if g.Name == name {
			return g
		}
	}
	return nil
}

func TestPlanGroupsTheDemoBoard(t *testing.T) {
	p := plan(t, DefaultRules())
	// Four byte lanes, plus the address and command group on the one fly-by
	// leg that is routed. The far leg produces no group at all because no net
	// on it has copper.
	var lanes, ac int
	for _, g := range p.Groups {
		switch g.Kind {
		case ByteLane:
			lanes++
		case AddressCommand:
			ac++
		}
	}
	if lanes != 4 {
		t.Errorf("%d byte-lane groups, want 4", lanes)
	}
	if ac != 1 {
		t.Errorf("%d address/command groups, want 1 (only the U3->U4 leg is routed)", ac)
	}
	if g := group(p, "address/command U3->U4"); g == nil {
		t.Error("no group for the routed first leg")
	}
	if g := group(p, "address/command U4->U5"); g != nil {
		t.Error("built a group for the unrouted far leg")
	}
	// RESETN is a control line and stays out by default.
	if r, ok := p.Skipped["/ddr4/DDR_RESETN"]; !ok || !strings.Contains(r, "control") {
		t.Errorf("RESETN skip reason = %q", r)
	}
}

func TestByteLaneGroupMembership(t *testing.T) {
	p := plan(t, DefaultRules())
	for lane := range 4 {
		g := group(p, laneName(lane))
		if g == nil {
			t.Fatalf("no group for lane %d", lane)
		}
		// Eight data bits, one mask, two strobe halves.
		if len(g.Members) != 11 {
			t.Errorf("lane %d has %d members, want 11", lane, len(g.Members))
		}
		var data, mask, strobe int
		for _, m := range g.Members {
			switch m.Role {
			case RoleData:
				data++
			case RoleDataMask:
				mask++
			case RoleStrobe:
				strobe++
			}
			if !m.Routed {
				t.Errorf("lane %d: %s is unrouted; all byte lanes on this board are complete", lane, m.Net)
			}
		}
		if data != 8 || mask != 1 || strobe != 2 {
			t.Errorf("lane %d: %d data, %d mask, %d strobe", lane, data, mask, strobe)
		}
		if g.Reference == "" {
			t.Errorf("lane %d has no strobe reference", lane)
		}
		if g.ToleranceMM <= 0 || math.IsInf(g.ToleranceMM, 1) {
			t.Errorf("lane %d tolerance resolved to %v", lane, g.ToleranceMM)
		}
	}
}

func laneName(l int) string { return "byte lane " + string(rune('0'+l)) }

// TestTheTargetIsTheReferenceMean is the rule every offset rests on: a group is
// matched to its strobe or clock pair -- the mean of the two halves -- and to
// nothing else. The target used to be raised to whichever member was longest,
// which matched byte lane 2 of the second demo board to DQ19 instead of its
// strobe and made every offset in the table a distance from DQ19.
func TestTheTargetIsTheReferenceMean(t *testing.T) {
	p := plan(t, DefaultRules())
	for _, g := range p.Groups {
		if len(g.ReferenceMembers) == 0 {
			continue
		}
		var sum float64
		for _, m := range g.ReferenceMembers {
			sum += m.Length
		}
		mean := sum / float64(len(g.ReferenceMembers))
		if math.Abs(g.ReferenceLength-mean) > 1e-9 {
			t.Errorf("%s: reference length %.4f, want the pair's mean %.4f", g.Name, g.ReferenceLength, mean)
		}
		if math.Abs(g.Target-mean) > 1e-9 {
			t.Errorf("%s: target %.4f, want the reference mean %.4f", g.Name, g.Target, mean)
		}
		for _, m := range g.Members {
			if !m.Routed {
				continue
			}
			if m.Need < 0 {
				t.Errorf("%s/%s: negative need %.4f", g.Name, m.Net, m.Need)
			}
			if math.Abs(m.Deviation-(m.Length-g.Target)) > 1e-12 {
				t.Errorf("%s/%s: offset %.4f is not against the target", g.Name, m.Net, m.Deviation)
			}
			if m.Reference {
				if m.Need != 0 || m.Excess != 0 {
					t.Errorf("%s/%s: half of the reference asked to move towards its own mean", g.Name, m.Net)
				}
				continue
			}
			if math.Abs(m.Need-math.Max(0, g.Target-m.Length)) > 1e-12 {
				t.Errorf("%s/%s: need %.6f does not match target minus length", g.Name, m.Net, m.Need)
			}
			// Longer than the reference past the tolerance: a reroute, and
			// never a reason to move the target.
			if over := m.Deviation - g.ToleranceMM; over > 1e-9 {
				if math.Abs(m.Excess-over) > 1e-9 || !m.NeedsReroute() {
					t.Errorf("%s/%s: %.4f mm too long, excess %.4f, reroute %v",
						g.Name, m.Net, over, m.Excess, m.NeedsReroute())
				}
			} else if m.Excess != 0 {
				t.Errorf("%s/%s: excess %.4f on a member within the band", g.Name, m.Net, m.Excess)
			}
		}
	}
}

// TestLaneThreeIsTheUntunedOne checks the plan against what the board actually
// looks like: lanes 0 to 2 were tuned by hand and are nearly matched, lane 3
// was not touched.
func TestLaneThreeIsTheUntunedOne(t *testing.T) {
	p := plan(t, DefaultRules())
	spread := map[int]float64{}
	for lane := range 4 {
		spread[lane] = group(p, laneName(lane)).SpreadBefore()
	}
	for _, lane := range []int{0, 1, 2} {
		if spread[lane] > 1.0 {
			t.Errorf("lane %d spread %.3f mm; expected it to be nearly matched already", lane, spread[lane])
		}
	}
	if spread[3] < 3.0 {
		t.Errorf("lane 3 spread %.3f mm; expected it to be clearly untuned", spread[3])
	}
	// And the untuned lane must need the most work.
	g3 := group(p, laneName(3))
	for _, lane := range []int{0, 1, 2} {
		if group(p, laneName(lane)).TotalNeed() >= g3.TotalNeed() {
			t.Errorf("lane %d needs more than lane 3", lane)
		}
	}
}

// TestClockOffsetScalesTheAddressTarget checks the offset knob: asking for the
// command group to run a few percent longer than the clock must move the
// target by that proportion of the clock's length, not of anything else.
func TestClockOffsetScalesTheAddressTarget(t *testing.T) {
	base := plan(t, DefaultRules())
	g0 := group(base, "address/command U3->U4")
	if g0 == nil {
		t.Fatal("no address/command group")
	}
	if math.Abs(g0.TargetFromReference-g0.ReferenceLength) > 1e-9 {
		t.Errorf("with no offset the target should be the clock length: %.4f vs %.4f",
			g0.TargetFromReference, g0.ReferenceLength)
	}
	for _, pct := range []float64{2, 5, 10} {
		r := DefaultRules()
		r.ClockOffsetPercent = pct
		g := group(plan(t, r), "address/command U3->U4")
		want := g.ReferenceLength * (1 + pct/100)
		if math.Abs(g.TargetFromReference-want) > 1e-9 {
			t.Errorf("offset %.0f%%: target from reference %.4f, want %.4f", pct, g.TargetFromReference, want)
		}
		// And that is the target, not merely a floor under it.
		if math.Abs(g.Target-want) > 1e-9 {
			t.Errorf("offset %.0f%%: target %.4f, want %.4f", pct, g.Target, want)
		}
	}
	// A negative offset asks for a shorter group. The target goes where it was
	// asked, and the members now too long for it say so, rather than the
	// target quietly staying where the longest member is.
	r := DefaultRules()
	r.ClockOffsetPercent = -20
	g := group(plan(t, r), "address/command U3->U4")
	if math.Abs(g.Target-g.ReferenceLength*0.8) > 1e-9 {
		t.Errorf("a -20%% offset left the target at %.4f, want %.4f", g.Target, g.ReferenceLength*0.8)
	}
	long := 0
	for _, m := range g.Members {
		if m.Excess > 0 {
			long++
		}
	}
	if long == 0 || len(g.Notes) == 0 {
		t.Errorf("a target 20%% under the clock and %d members reported too long, notes %v", long, g.Notes)
	}
}

// TestAddressGroupIsMeasuredPerLeg checks that every member of the fly-by group
// is measured over the same span. Comparing a controller-to-near-device length
// against a controller-to-far-device length would be meaningless, and is the
// mistake per-leg planning exists to prevent.
func TestAddressGroupIsMeasuredPerLeg(t *testing.T) {
	p := plan(t, DefaultRules())
	g := group(p, "address/command U3->U4")
	if g == nil {
		t.Fatal("no group")
	}
	n := 0
	for _, m := range g.Members {
		if !m.Routed {
			continue
		}
		n++
		a, b := refOf(m.From), refOf(m.To)
		if !(a == "U3" && b == "U4") && !(a == "U4" && b == "U3") {
			t.Errorf("%s measured between %s and %s, not over U3->U4", m.Net, a, b)
		}
	}
	// Two clock halves, 17 address, 7 command.
	if n != 26 {
		t.Errorf("%d routed members on the first leg, want 26", n)
	}
}

func TestIntraPairSkewReported(t *testing.T) {
	p := plan(t, DefaultRules())
	skew := p.IntraPairSkew()
	if len(skew) < 5 {
		t.Fatalf("only %d pairs reported: %v", len(skew), skew)
	}
	// Lane 1's strobe pair was tuned to within a couple of micrometres; lane 3's
	// was not tuned at all.
	var dqs1, dqs3 float64
	for k, v := range skew {
		switch {
		case strings.Contains(k, "DQS1_P"):
			dqs1 = v
		case strings.Contains(k, "DQS3_P"):
			dqs3 = v
		}
	}
	// Lane 1's strobe pair was tuned by hand to within a few tens of
	// micrometres of centreline length.
	if dqs1 > 0.05 {
		t.Errorf("DQS1 pair skew %.4f mm, expected it to be nearly zero", dqs1)
	}
	if dqs3 < 0.5 {
		t.Errorf("DQS3 pair skew %.4f mm, expected it to be clearly out", dqs3)
	}
}

// A DDR pair's own skew is not a requirement: AN5724 sets no limit on it,
// so no group asks for a pair to be rerouted because of it.
func TestDDRPairSkewIsNotARequirement(t *testing.T) {
	r := DefaultRules()
	r.MaxIntraPairFix = 0.1
	p := plan(t, r)
	for _, g := range p.Groups {
		for _, n := range g.Notes {
			if strings.Contains(n, "pair") {
				t.Errorf("%s: %q", g.Name, n)
			}
		}
	}
}

func TestPlanRejectsBadRules(t *testing.T) {
	b := loadBoard(t)
	iface, _ := Classify(b, Options{NetPrefix: "/ddr4/"})
	e := netlen.New(b)
	for name, r := range map[string]Rules{
		"no data tolerance":  {IntraPair: Tolerance{MM: 1}, AddressToClock: Tolerance{MM: 1}},
		"no intra-pair":      {DataToStrobe: Tolerance{MM: 1}, AddressToClock: Tolerance{MM: 1}},
		"no address":         {DataToStrobe: Tolerance{MM: 1}, IntraPair: Tolerance{MM: 1}},
		"absurd offset":      {DataToStrobe: Tolerance{MM: 1}, IntraPair: Tolerance{MM: 1}, AddressToClock: Tolerance{MM: 1}, ClockOffsetPercent: 200},
		"offset below -100%": {DataToStrobe: Tolerance{MM: 1}, IntraPair: Tolerance{MM: 1}, AddressToClock: Tolerance{MM: 1}, ClockOffsetPercent: -150},
	} {
		if _, err := BuildPlan(iface, e, r); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestToleranceTighterUnitWins(t *testing.T) {
	// 6.5 ps/mm is about right for FR4 microstrip.
	const rate = 6.5
	cases := []struct {
		tol  Tolerance
		want float64
	}{
		{Tolerance{MM: 1}, 1},
		{Tolerance{PS: 6.5}, 1},
		{Tolerance{MM: 1, PS: 3.25}, 0.5},  // delay is tighter
		{Tolerance{MM: 0.4, PS: 6.5}, 0.4}, // length is tighter
	}
	for _, c := range cases {
		if got := c.tol.LimitMM(rate); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%v.LimitMM(%v) = %v, want %v", c.tol, rate, got, c.want)
		}
	}
	if !(Tolerance{}).Zero() {
		t.Error("empty tolerance not reported zero")
	}
	if math.IsInf((Tolerance{MM: 1}).LimitMM(0), 1) {
		t.Error("a length tolerance should survive a zero propagation rate")
	}
}

// A tolerance set for one group applies to that group alone.
func TestAGroupToleranceAppliesToThatGroupOnly(t *testing.T) {
	base := plan(t, DefaultRules())
	r := DefaultRules()
	r.GroupToleranceMM = map[string]float64{"byte lane 2": 0.1}
	got := plan(t, r)

	if g := group(got, "byte lane 2"); math.Abs(g.ToleranceMM-0.1) > 1e-9 {
		t.Errorf("byte lane 2 tolerance %.4f, want 0.1", g.ToleranceMM)
	}
	for _, name := range []string{"byte lane 0", "byte lane 1", "byte lane 3"} {
		if a, b := group(base, name).ToleranceMM, group(got, name).ToleranceMM; math.Abs(a-b) > 1e-12 {
			t.Errorf("%s changed from %.4f to %.4f", name, a, b)
		}
	}
	// And its members are judged against it: tighter means at least as many out.
	if group(got, "byte lane 2").OutOfTolerance() < group(base, "byte lane 2").OutOfTolerance() {
		t.Error("a tighter tolerance put fewer members out of it")
	}
}

// The requirements across groups: each byte lane's strobe against the clock at
// its device, and one device's lanes against another's.
func TestChecksAcrossGroups(t *testing.T) {
	p := plan(t, DefaultRules())
	cs := p.Checks()
	strobe, chip := 0, 0
	for _, c := range cs {
		switch c.Kind {
		case "strobe-to-clock":
			strobe++
			if c.LimitMM <= 0 {
				t.Errorf("%s: limit %.3f", c.Name, c.LimitMM)
			}
			if c.OK != (math.Abs(c.ValueMM) <= c.LimitMM) {
				t.Errorf("%s: verdict %v does not follow %.3f against %.3f", c.Name, c.OK, c.ValueMM, c.LimitMM)
			}
		case "chip-delta":
			chip++
			if c.ValueMM < 0 || c.OK != (c.ValueMM <= c.LimitMM) {
				t.Errorf("%s: %.3f against %.3f, ok %v", c.Name, c.ValueMM, c.LimitMM, c.OK)
			}
		}
		if c.Detail == "" {
			t.Errorf("%s has no arithmetic to check it by", c.Name)
		}
	}
	if chip == 0 {
		t.Error("two devices on the bus and no check between their byte lanes")
	}
	_ = strobe // the first demo board is missing the second clock leg, so not every lane has a clock to compare with

	// A zero limit skips the check rather than failing everything.
	r := DefaultRules()
	r.MaxChipDeltaMM = 0
	r.StrobeToClock = Tolerance{}
	if got := plan(t, r).Checks(); len(got) != 0 {
		t.Errorf("%d checks with both limits off", len(got))
	}
}

// The clock reaching the second device is measured along the path from the
// controller, not as the two legs added up: a leg ends at the first device's
// ball, and the clock to the second never walks down to it and back.
func TestClockToTheSecondDeviceIsTheDirectPath(t *testing.T) {
	b, err := board.Load("../demo-pcb-2/ai-vision.kicad_pcb")
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	iface, err := Classify(b, Options{NetPrefix: "/ddr4/"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(iface, netlen.New(b), DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	legs := 0.0
	for _, g := range p.Groups {
		if g.Kind == AddressCommand {
			legs += g.ReferenceLength
		}
	}
	last := p.Chain.Order[len(p.Chain.Order)-1]
	clk, ok := p.clockTo[last]
	if !ok {
		t.Fatalf("no clock length to %s", last)
	}
	if clk.length >= legs-0.5 || !strings.Contains(clk.how, p.Chain.Order[0]+"->"+last) {
		t.Errorf("clock to %s is %.3f mm (%s); the legs add up to %.3f and should be longer by the stub to the first device",
			last, clk.length, clk.how, legs)
	}
}
