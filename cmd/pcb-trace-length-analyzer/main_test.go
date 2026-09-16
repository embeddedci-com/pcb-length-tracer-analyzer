package main

import (
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/ddr"
)

// The -area flag is the command line's half of "where copper may go", and it
// parses text a person typed. Everything it accepts becomes a restriction on
// what the tool will write, so what it refuses matters as much as what it takes.
func TestAreaFlagParsing(t *testing.T) {
	t.Run("takes four numbers in either order", func(t *testing.T) {
		var a areaList
		if err := a.Set("10,20,30,45"); err != nil {
			t.Fatal(err)
		}
		// Corners the other way round mean the same rectangle: nobody drags
		// consistently, and a flipped one would be empty and silently match
		// nothing.
		if err := a.Set("30,45,10,20"); err != nil {
			t.Fatal(err)
		}
		if len(a) != 2 {
			t.Fatalf("%d areas", len(a))
		}
		for i, r := range a {
			if r.MinX != 10 || r.MinY != 20 || r.MaxX != 30 || r.MaxY != 45 {
				t.Errorf("area %d = %+v", i, r.Rect)
			}
		}
	})

	t.Run("refuses what is not a rectangle", func(t *testing.T) {
		for _, in := range []string{
			"", "10,20,30", "10,20,30,40,50", "a,b,c,d", "10 20 30 40",
			"10,20,10,40", // no width
			"10,20,30,20", // no height
		} {
			var a areaList
			if err := a.Set(in); err == nil {
				t.Errorf("%q was accepted as %+v", in, a)
			}
		}
	})

	t.Run("says what an area is worth in the report", func(t *testing.T) {
		var a areaList
		if err := a.Set("0,0,10,10"); err != nil {
			t.Fatal(err)
		}
		if got := spacingNote(a[0]); got != "" {
			t.Errorf("an area with no spacing of its own said %q", got)
		}
		a[0].MinClearance = 0.25
		if got := spacingNote(a[0]); !strings.Contains(got, "0.250") {
			t.Errorf("spacing note %q does not give the figure", got)
		}
	})
}

// A net short on two legs of the fly-by chain is two pieces of work, not one.
//
// Address, command, control and clock are matched against the clock over each
// span separately, and the length for a span has to go into that span's own
// copper. Keeping only a net's worst leg -- which this did -- fixed that leg
// and left the next one exactly as short as it was. On the second demo board
// that was 8 nets and 14.9 mm of length the board had the room for.
func TestEveryLegOfANetIsItsOwnTarget(t *testing.T) {
	member := func(net string, need float64, tracks ...string) ddr.Member {
		return ddr.Member{
			Net: net, Role: ddr.RoleAddress, Routed: true,
			Length: 20, Need: need, Deviation: -need, PathTracks: tracks,
		}
	}
	plan := &ddr.Plan{Groups: []*ddr.Group{
		{Name: "address/command U3->U4", Leg: "U3->U4", Kind: ddr.AddressCommand,
			Members: []ddr.Member{member("/ddr4/DDR_A8", 10, "t1")}},
		{Name: "address/command U4->U5", Leg: "U4->U5", Kind: ddr.AddressCommand,
			Members: []ddr.Member{member("/ddr4/DDR_A8", 3, "t2")}},
	}}

	got := selectTargets(plan, options{})
	if len(got) != 2 {
		t.Fatalf("got %d targets for a net short on two legs, want 2", len(got))
	}
	byGroup := map[string]target{}
	for _, tg := range got {
		byGroup[tg.group] = tg
	}
	first, second := byGroup["address/command U3->U4"], byGroup["address/command U4->U5"]
	if first.need != 10 || second.need != 3 {
		t.Errorf("asked for %.3f and %.3f mm, want 10 and 3", first.need, second.need)
	}
	// Each into its own copper: a meander for one leg written into the other
	// would lengthen the wrong span.
	if !first.tracks["t1"] || first.tracks["t2"] || !second.tracks["t2"] {
		t.Errorf("targets may write to %v and %v", first.tracks, second.tracks)
	}
}

func TestGroupToleranceFlag(t *testing.T) {
	var g groupTolList
	if err := g.Set("address/command U3->U4=0.9"); err != nil {
		t.Fatal(err)
	}
	if err := g.Set("byte lane 0 = 0.2"); err != nil {
		t.Fatal(err)
	}
	if g.m["address/command U3->U4"] != 0.9 || g.m["byte lane 0"] != 0.2 {
		t.Errorf("parsed %v", g.m)
	}
	for _, bad := range []string{"byte lane 0", "=0.2", "byte lane 0=-1", "byte lane 0=wide"} {
		if err := g.Set(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// -preset loads a vendor's table. A flag beside it is the more specific
// instruction and has to survive, or somebody tightening one limit by hand
// would silently get the guide's value instead.
func TestPresetFlag(t *testing.T) {
	t.Run("loads the vendor's limits", func(t *testing.T) {
		o := options{}
		if err := usePreset(&o, "rk3588-lpddr4-hdi", map[string]bool{}); err != nil {
			t.Fatal(err)
		}
		if d := o.dataTolMM - 0.635; d > 1e-9 || d < -1e-9 {
			t.Errorf("data tolerance %.4f mm, want 0.635", o.dataTolMM)
		}
		if o.addrTolMM != 1.016 || o.strobeClockMM != 6.35 {
			t.Errorf("address %.4f, strobe to clock %.4f", o.addrTolMM, o.strobeClockMM)
		}
		if o.dataTolPS != 0 {
			t.Errorf("a length preset left a delay behind: %.1f ps", o.dataTolPS)
		}
	})

	t.Run("a delay preset clears the lengths it replaces", func(t *testing.T) {
		o := options{dataTolMM: 1.42, strobeClockMM: 12.07}
		if err := usePreset(&o, "rk3588-lpddr4-8layer", map[string]bool{}); err != nil {
			t.Fatal(err)
		}
		if o.dataTolPS != 16 || o.dataTolMM != 0 {
			t.Errorf("data %.3f mm / %.1f ps, want 16 ps alone", o.dataTolMM, o.dataTolPS)
		}
		if o.strobeClockMM != 0 {
			t.Errorf("strobe to clock kept %.3f mm, which the guide states as 40 ps", o.strobeClockMM)
		}
	})

	t.Run("keeps what the command line set itself", func(t *testing.T) {
		o := options{dataTolMM: 0.2, addrTolMM: 9}
		if err := usePreset(&o, "rk3588-lpddr4-hdi", map[string]bool{"data-tol-mm": true}); err != nil {
			t.Fatal(err)
		}
		if o.dataTolMM != 0.2 {
			t.Errorf("the flag's 0.2 mm was overwritten with %.3f", o.dataTolMM)
		}
		if o.addrTolMM != 1.016 {
			t.Errorf("address %.4f, want the preset's 1.016", o.addrTolMM)
		}
	})

	t.Run("refuses an unknown one", func(t *testing.T) {
		if err := usePreset(&options{}, "rk9999", map[string]bool{}); err == nil {
			t.Error("an unknown preset was accepted")
		} else if !strings.Contains(err.Error(), "-preset list") {
			t.Errorf("error does not say how to find the right name: %v", err)
		}
	})

	t.Run("every preset the list prints can be used", func(t *testing.T) {
		for _, p := range ddr.Presets() {
			o := options{}
			if err := usePreset(&o, p.ID, map[string]bool{}); err != nil {
				t.Errorf("%s: %v", p.ID, err)
			}
		}
	})
}

// `-preset list` is where somebody checks a preset against the guide it cites,
// so the numbers it prints have to be the guide's, not rounded to what a
// measured board is quoted to.
func TestPresetListQuotesTheGuidesNumbers(t *testing.T) {
	for _, c := range []struct {
		tol  ddr.Tolerance
		want string
	}{
		{ddr.Tolerance{PS: 0.75}, "0.75 ps"},
		{ddr.Tolerance{PS: 50}, "50 ps"},
		{ddr.Tolerance{PS: 312.5}, "312.5 ps"},
		{ddr.Tolerance{PS: 3750}, "3750 ps"},
		{ddr.Tolerance{MM: 12 * 0.0254}, "0.3048 mm"},
		{ddr.Tolerance{MM: 1.42}, "1.42 mm"},
	} {
		if got := quoted(c.tol); got != c.want {
			t.Errorf("quoted(%v) = %q, want %q", c.tol, got, c.want)
		}
	}
}
