package server

import (
	"math"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/ddr"
)

// A fly-by net is matched over each leg of the chain separately, so one net can
// be short twice, in different copper. These check that both halves of that are
// true: the report adds up what the board actually needs, and the apply puts
// length into every leg rather than only the worse one.
//
// It was neither. Keeping the worst leg per net understated the second demo
// board by 28.673 mm and left 8 nets short on U4->U5 with the room to fix them
// sitting unused beside their tracks.

func twoLegPlan() *ddr.Plan {
	member := func(net string, length, need, headroom float64, tracks ...string) ddr.Member {
		return ddr.Member{
			Net: net, Role: ddr.RoleAddress, Routed: true,
			Length: length, Need: need, Deviation: -need, Headroom: headroom,
			PathTracks: tracks,
		}
	}
	return &ddr.Plan{Groups: []*ddr.Group{
		{
			Name: "address/command U3->U4", Leg: "U3->U4", Kind: ddr.AddressCommand,
			Target: 30, ToleranceMM: 1.27,
			Members: []ddr.Member{
				member("/ddr4/DDR_A8", 20, 10, 2, "t1", "t2"),
				member("/ddr4/DDR_A9", 29.5, 0.5, 9, "t5"),
			},
		},
		{
			Name: "address/command U4->U5", Leg: "U4->U5", Kind: ddr.AddressCommand,
			Target: 32, ToleranceMM: 1.27,
			Members: []ddr.Member{
				member("/ddr4/DDR_A8", 29, 3, 4, "t3", "t4"),
			},
		},
	}}
}

func TestACandidateNeedsEveryLegItIsShortOn(t *testing.T) {
	got := candidates(twoLegPlan())
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want one per net", len(got))
	}
	a8 := got[0]
	if a8.Net != "/ddr4/DDR_A8" {
		t.Fatalf("worst candidate is %s, want A8", a8.Label)
	}
	// 10 mm on the first leg and 3 on the second is 13 mm of copper to add,
	// not 10: fixing the worse leg leaves the other as short as it was.
	if math.Abs(a8.NeedMM-13) > 1e-9 {
		t.Errorf("A8 needs %.3f mm, want 13.000 over both legs", a8.NeedMM)
	}
	// Room beside one leg is no use to the other, so what can be had is each
	// leg's room capped at that leg's need: 2 of 10, then 3 of 4.
	if math.Abs(a8.HeadroomMM-5) > 1e-9 {
		t.Errorf("A8 can use %.3f mm of room, want 5.000 (2 on one leg, 3 on the other)", a8.HeadroomMM)
	}
	if len(a8.Legs) != 2 {
		t.Fatalf("A8 lists %d legs, want both", len(a8.Legs))
	}
	if a8.Legs[0].Leg != "U3->U4" || a8.Legs[1].Leg != "U4->U5" {
		t.Errorf("legs are %q and %q", a8.Legs[0].Leg, a8.Legs[1].Leg)
	}
	// The uncapped room stays visible per leg, because "there is 9 mm of space
	// here" is worth knowing even when only 3 of it can be used.
	if math.Abs(a8.Legs[1].HeadroomMM-4) > 1e-9 {
		t.Errorf("the second leg reports %.3f mm of room, want 4.000 uncapped", a8.Legs[1].HeadroomMM)
	}

	// A net short on one leg still says which group that was: without it, a
	// net that appears in two groups has its whole requirement charged to both.
	if len(got[1].Legs) != 1 || got[1].Legs[0].Group != "address/command U3->U4" {
		t.Errorf("A9 is short in one group and reports %+v", got[1].Legs)
	}
}

func TestApplyTargetsEveryLegOfAChosenNet(t *testing.T) {
	plan := twoLegPlan()
	a := &Analysis{Candidates: candidates(plan)}

	targets, err := selectTargets(plan, a, ApplyRequest{Nets: []string{"/ddr4/DDR_A8"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets for a net short on two legs, want 2", len(targets))
	}
	byLeg := map[string]target{}
	for _, tg := range targets {
		byLeg[tg.leg] = tg
	}
	first, ok := byLeg["U3->U4"]
	if !ok {
		t.Fatal("no target for the first leg")
	}
	second, ok := byLeg["U4->U5"]
	if !ok {
		t.Fatal("no target for the second leg")
	}
	if math.Abs(first.need-10) > 1e-9 || math.Abs(second.need-3) > 1e-9 {
		t.Errorf("asked for %.3f and %.3f mm, want 10 and 3", first.need, second.need)
	}
	// And each into its own copper: a meander for the second leg written into
	// the first would lengthen the wrong span.
	if !first.tracks["t1"] || first.tracks["t3"] {
		t.Errorf("the first leg may write to %v", first.tracks)
	}
	if !second.tracks["t3"] || second.tracks["t1"] {
		t.Errorf("the second leg may write to %v", second.tracks)
	}
}
