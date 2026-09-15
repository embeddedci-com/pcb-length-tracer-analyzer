package tune

import (
	"math"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// TestGainFormulaMatchesTheDrawnPolyline is the arithmetic the whole tuner
// rests on. The closed-form gain per excursion has to equal what the generated
// centreline actually measures, or the tuner will aim at a target and land
// somewhere else.
func TestGainFormulaMatchesTheDrawnPolyline(t *testing.T) {
	for _, amp := range []float64{0.2, 0.5, 1.0, 1.5} {
		for _, chamfer := range []float64{0, 0.045, 0.09} {
			for _, count := range []int{1, 2, 5} {
				flat := math.Max(4*chamfer, 0.36)
				space := 0.27
				runLen := float64(count)*flat + float64(count-1)*space + 2.0
				if amp < 2*chamfer {
					continue
				}
				m := meanderShape{
					from: geom.Pt{X: 10, Y: 10},
					to:   geom.Pt{X: 10 + runLen, Y: 10},
					dir:  geom.Pt{X: 1, Y: 0},
					side: geom.Pt{X: 0, Y: 1},
					amp:  amp, flat: flat, space: space, chamfer: chamfer, count: count,
				}
				if !m.valid() {
					t.Fatalf("amp=%v c=%v n=%d: shape rejected as invalid", amp, chamfer, count)
				}
				wantGain := m.gain()
				gotGain := m.polylineLength() - m.from.Dist(m.to)
				// The only difference allowed is the nanometre grid the points
				// are snapped onto, a few per vertex at worst.
				if math.Abs(wantGain-gotGain) > 1e-4 {
					t.Errorf("amp=%v c=%v n=%d: formula says %.6f mm gained, polyline gives %.6f",
						amp, chamfer, count, wantGain, gotGain)
				}
			}
		}
	}
}

// TestChamferCostsLength records the trade the mitred corners make: a sharper
// corner gains more length, and a square corner gains the full 2h per
// excursion.
func TestChamferCostsLength(t *testing.T) {
	mk := func(c float64) meanderShape {
		return meanderShape{
			from: geom.Pt{}, to: geom.Pt{X: 10},
			dir: geom.Pt{X: 1}, side: geom.Pt{Y: 1},
			amp: 1, flat: 0.5, space: 0.3, chamfer: c, count: 1,
		}
	}
	if got := mk(0).gain(); math.Abs(got-2.0) > 1e-12 {
		t.Errorf("square corners gain %.6f, want 2h = 2", got)
	}
	prev := math.Inf(1)
	for _, c := range []float64{0, 0.05, 0.1, 0.125} {
		g := mk(c).gain()
		if g >= prev {
			t.Errorf("chamfer %v gained %.6f, not less than the previous %.6f", c, g, prev)
		}
		prev = g
	}
}

// TestPolylineKeepsEndpointsAndAxialExtent checks the property that makes an
// edit safe: the meander starts and ends exactly where the run did, so the
// track it replaces still joins whatever it joined.
func TestPolylineKeepsEndpointsAndAxialExtent(t *testing.T) {
	m := meanderShape{
		from: geom.Pt{X: 1.234567, Y: 2.345678},
		to:   geom.Pt{X: 9.876543, Y: 2.345678},
		dir:  geom.Pt{X: 1, Y: 0},
		side: geom.Pt{X: 0, Y: -1},
		amp:  0.8, flat: 0.36, space: 0.27, chamfer: 0.09, count: 4,
	}
	pts := m.polyline()
	if !pts[0].Near(m.from.Snap()) {
		t.Errorf("starts at %v, want %v", pts[0], m.from)
	}
	if !pts[len(pts)-1].Near(m.to.Snap()) {
		t.Errorf("ends at %v, want %v", pts[len(pts)-1], m.to)
	}
	// Every point must lie between the endpoints along the axis, and within the
	// amplitude across it.
	for _, p := range pts {
		along := p.Sub(m.from).Dot(m.dir)
		across := p.Sub(m.from).Dot(m.side)
		if along < -1e-6 || along > m.from.Dist(m.to)+1e-6 {
			t.Errorf("point %v is %v along the axis, outside the run", p, along)
		}
		if across < -1e-6 || across > m.amp+1e-6 {
			t.Errorf("point %v strays %v across the axis, beyond the amplitude %v", p, across, m.amp)
		}
	}
	// And it must be centred: the lead-in equals the lead-out.
	lead := pts[1].Sub(m.from).Dot(m.dir)
	tail := m.to.Sub(pts[len(pts)-2]).Dot(m.dir)
	if math.Abs(lead-tail) > 1e-3 {
		t.Errorf("meander is not centred: lead %.4f, tail %.4f", lead, tail)
	}
}

// TestMeanderLegsKeepTheirDistance checks the spacing rule that keeps a meander
// sound rather than merely legal. The parallel legs of a serpentine couple to
// each other, and that coupling eats into the very delay the meander was added
// to provide, so they are held three track widths of clear copper apart.
//
// What is compared here is the legs -- the runs across the excursion -- and the
// original axis. The two ends of one U-turn come closer than that by
// construction, but they are the same continuous piece of copper turning
// around, not two conductors running alongside each other.
func TestMeanderLegsKeepTheirDistance(t *testing.T) {
	const width = 0.09
	const gapW = 3.0
	chamfer := 0.09
	space := (gapW + 1) * width
	m := meanderShape{
		from: geom.Pt{}, to: geom.Pt{X: 12},
		dir: geom.Pt{X: 1}, side: geom.Pt{Y: 1},
		amp: 1.0, flat: math.Max(space+2*chamfer, 4*chamfer), space: space, chamfer: chamfer, count: 5,
	}
	pts := m.polyline()

	// The legs are the segments running across the excursion.
	var legs []geom.Seg
	for i := 0; i+1 < len(pts); i++ {
		s := geom.Seg{A: pts[i], B: pts[i+1]}
		if math.Abs(s.B.X-s.A.X) < 1e-9 && math.Abs(s.B.Y-s.A.Y) > 1e-9 {
			legs = append(legs, s)
		}
	}
	if want := 2 * m.count; len(legs) != want {
		t.Fatalf("found %d legs, want %d", len(legs), want)
	}
	worst := math.Inf(1)
	for i := range legs {
		for j := i + 1; j < len(legs); j++ {
			if d := legs[i].DistToSeg(legs[j]); d < worst {
				worst = d
			}
		}
	}
	if gap := worst - width; gap < gapW*width-1e-6 {
		t.Errorf("closest gap between meander legs is %.4f mm, want at least %.4f (%vW)",
			gap, gapW*width, gapW)
	}
	t.Logf("closest leg-to-leg copper gap %.4f mm = %.1fW", worst-width, (worst-width)/width)

	// The flat tops must clear the original axis by the amplitude.
	axis := geom.Seg{A: m.from, B: m.to}
	for i := 0; i+1 < len(pts); i++ {
		s := geom.Seg{A: pts[i], B: pts[i+1]}
		if math.Abs(s.A.Y-m.amp) > 1e-9 || math.Abs(s.B.Y-m.amp) > 1e-9 {
			continue
		}
		if d := axis.DistToSeg(s); math.Abs(d-m.amp) > 1e-6 {
			t.Errorf("flat top sits %.4f from the axis, want the amplitude %.4f", d, m.amp)
		}
	}
}

// TestMeanderDoesNotCrossItself checks that no two parts of the generated
// centreline intersect. A meander that folded across itself would short to a
// shorter path and measure longer than it is.
func TestMeanderDoesNotCrossItself(t *testing.T) {
	for _, count := range []int{1, 3, 8} {
		for _, amp := range []float64{0.2, 0.9, 1.5} {
			chamfer, width := 0.09, 0.09
			space := 4 * width
			flat := math.Max(space+2*chamfer, 4*chamfer)
			if amp < 2*chamfer {
				continue
			}
			runLen := float64(count)*flat + float64(count-1)*space + 1.0
			m := meanderShape{
				from: geom.Pt{X: 5, Y: 5}, to: geom.Pt{X: 5 + runLen, Y: 5},
				dir: geom.Pt{X: 1}, side: geom.Pt{Y: 1},
				amp: amp, flat: flat, space: space, chamfer: chamfer, count: count,
			}
			pts := m.polyline()
			for i := 0; i+1 < len(pts); i++ {
				a := geom.Seg{A: pts[i], B: pts[i+1]}
				for j := i + 2; j+1 < len(pts); j++ {
					b := geom.Seg{A: pts[j], B: pts[j+1]}
					if a.Intersects(b) {
						t.Errorf("count=%d amp=%v: segment %d crosses segment %d", count, amp, i, j)
					}
				}
			}
		}
	}
}

func TestInvalidShapesAreRejected(t *testing.T) {
	base := meanderShape{
		from: geom.Pt{}, to: geom.Pt{X: 10},
		dir: geom.Pt{X: 1}, side: geom.Pt{Y: 1},
		amp: 1, flat: 0.4, space: 0.3, chamfer: 0.09, count: 2,
	}
	if !base.valid() {
		t.Fatal("the base shape should be valid")
	}
	bad := map[string]meanderShape{
		"no excursions":                  func() meanderShape { m := base; m.count = 0; return m }(),
		"amplitude below the chamfer":    func() meanderShape { m := base; m.amp = 0.1; return m }(),
		"flat too short for the corners": func() meanderShape { m := base; m.flat = 0.2; return m }(),
		"longer than the run":            func() meanderShape { m := base; m.count = 40; return m }(),
	}
	for name, m := range bad {
		if m.valid() {
			t.Errorf("%s: accepted as valid", name)
		}
	}
}

func TestNewUUIDIsWellFormedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		u := newUUID()
		if len(u) != 36 || u[8] != '-' || u[13] != '-' || u[14] != '4' || u[18] != '-' || u[23] != '-' {
			t.Fatalf("malformed uuid %q", u)
		}
		if seen[u] {
			t.Fatalf("duplicate uuid %q", u)
		}
		seen[u] = true
	}
}
