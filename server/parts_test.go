package server

import (
	"math"
	"testing"
)

// Every length the report shows is shown taken apart, and the page says the
// parts add up. They must, everywhere a length appears: group members, the
// reference halves, each candidate and each of its legs, and every other
// interface's candidates.
func TestEveryReportedLengthAddsUpToItsParts(t *testing.T) {
	h := newHarness(t)
	a := decode[SessionResponse](t, h.upload(true, true)).Analysis

	checked := 0
	check := func(where string, length float64, p *LengthParts) {
		t.Helper()
		if p == nil {
			t.Errorf("%s: a routed length with no parts", where)
			return
		}
		sum := p.TrackMM + p.ViaMM + p.PadMM + p.PackageMM
		if math.Abs(sum-length) > 1e-9 {
			t.Errorf("%s: parts %+v sum to %.6f, length %.6f", where, *p, sum, length)
		}
		if p.Vias > 0 && a.Board.ViaLengthUsed && a.Board.ViaBarrelMM > 0 {
			// The demo board's vias all run the full stack.
			if math.Abs(p.ViaMM-float64(p.Vias)*a.Board.ViaBarrelMM) > 1e-9 {
				t.Errorf("%s: %d vias but %.4f mm of barrel", where, p.Vias, p.ViaMM)
			}
		}
		checked++
	}

	vias := 0
	for _, g := range a.Groups {
		for _, m := range g.Members {
			if !m.Routed {
				if m.Parts != nil {
					t.Errorf("%s/%s: unrouted, but has parts", g.Name, m.Label)
				}
				continue
			}
			check(g.Name+"/"+m.Label, m.LengthMM, m.Parts)
			if m.Parts != nil {
				vias += m.Parts.Vias
			}
		}
		for _, r := range g.ReferenceMembers {
			check(g.Name+"/ref "+r.Label, r.LengthMM, r.Parts)
		}
	}
	for _, c := range a.Candidates {
		check("candidate "+c.Label, c.LengthMM, c.Parts)
		for _, l := range c.Legs {
			check("candidate "+c.Label+" "+l.Group, l.LengthMM, l.Parts)
		}
	}
	for _, i := range a.Interfaces {
		if i.Planner != "" {
			continue
		}
		for _, c := range i.Candidates {
			check(i.Name+" "+c.Label, c.LengthMM, c.Parts)
		}
	}
	if checked < 50 {
		t.Errorf("only %d lengths checked", checked)
	}
	if vias == 0 {
		t.Error("no matched route on the demo board crosses a via; the via part is untested")
	}
	if !a.Board.ViaLengthUsed || math.Abs(a.Board.ViaBarrelMM-1.594) > 1e-6 {
		t.Errorf("via barrel %.4f counted=%v, want 1.594 counted", a.Board.ViaBarrelMM, a.Board.ViaLengthUsed)
	}
}
