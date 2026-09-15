package proto

import (
	"fmt"
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// What the board can already tell us, and what it cannot.
//
// Two numbers decide an interface's impedance: how wide its tracks are, and --
// for a pair -- how far apart the two halves run. Where the interface is
// routed, both are on the board and asking the user for them would be asking
// them to retype what they have already drawn. Where it is not routed, neither
// exists yet and the tool has to be told.
//
// So the rule is: measure what is there, say it was measured, and ask for the
// rest. A figure that was measured is offered for confirmation rather than
// presented as settled, because a half-routed interface can easily have been
// drawn at a width nobody meant.

// Geometry is the physical shape of an interface's copper.
type Geometry struct {
	// WidthMM is the track width, and Widths lists every width found with how
	// much copper is at each, longest first. A bus drawn at two widths is
	// worth seeing rather than averaging away.
	WidthMM float64
	Widths  []WidthUse

	// GapMM is the copper-to-copper spacing between the halves of a
	// differential pair, over the stretches where they run together.
	GapMM float64

	// Layer is where most of the interface's copper is, which is what decides
	// whether it is microstrip or stripline -- a difference far larger than
	// any model error.
	Layer string

	// Measured says which of the above came off the board rather than from the
	// user. Empty when nothing could be measured.
	Measured []string

	// Note explains anything surprising: two widths, a pair that never runs
	// coupled, an interface with no copper at all.
	Note string
}

// WidthUse is one track width and how much copper is drawn at it.
type WidthUse struct {
	WidthMM  float64
	LengthMM float64
}

// MeasureGeometry reads an interface's width, spacing and layer off the board.
func (i *Interface) MeasureGeometry(b *board.Board) Geometry {
	g := Geometry{}

	// Width, by how much copper is at each. The dominant width is the one the
	// interface was drawn to; a stub at some other width should not move it.
	byWidth := map[float64]float64{}
	byLayer := map[string]float64{}
	for _, net := range i.Nets {
		for _, t := range b.TracksOfNet(net) {
			if t.Kind != board.KindSegment && t.Kind != board.KindArc {
				continue
			}
			l := t.Length(b.Stackup)
			byWidth[round3(t.Width)] += l
			byLayer[t.Layer] += l
		}
	}
	for w, l := range byWidth {
		g.Widths = append(g.Widths, WidthUse{WidthMM: w, LengthMM: l})
	}
	sort.Slice(g.Widths, func(x, y int) bool { return g.Widths[x].LengthMM > g.Widths[y].LengthMM })
	if len(g.Widths) > 0 {
		g.WidthMM = g.Widths[0].WidthMM
		g.Measured = append(g.Measured, "width")
	}
	for l, length := range byLayer {
		if byLayer[g.Layer] < length {
			g.Layer = l
		}
	}

	if len(g.Widths) > 1 {
		var second float64
		if len(g.Widths) > 1 {
			second = g.Widths[1].WidthMM
		}
		g.Note = fmt.Sprintf("drawn at %d widths; %.3f mm carries most of the copper and %.3f mm the next most",
			len(g.Widths), g.WidthMM, second)
	}

	// Pair spacing, from the stretches where the two halves actually run
	// together. A pair that fans out at its ends and couples in the middle has
	// one spacing that matters, and it is the coupled one.
	var gaps []float64
	for _, p := range i.Pairs {
		if gap, ok := pairGap(b, p); ok {
			gaps = append(gaps, gap)
		}
	}
	if len(gaps) > 0 {
		sort.Float64s(gaps)
		g.GapMM = round3(gaps[len(gaps)/2])
		g.Measured = append(g.Measured, "pair spacing")
	} else if len(i.Pairs) > 0 && g.WidthMM > 0 {
		if g.Note != "" {
			g.Note += ". "
		}
		g.Note += "its pairs have copper but never run coupled, so there is no spacing to read"
	}

	if len(g.Measured) == 0 {
		g.Note = "nothing is routed yet, so the width and spacing have to be given"
	}
	return g
}

// pairGap is the copper-to-copper spacing between two nets where they run
// parallel and close, as a median over the coupled stretches.
//
// Median rather than minimum: a pair necks down at a via or a pad, and the
// tightest point is not the spacing the pair was drawn to.
func pairGap(b *board.Board, p Pair) (float64, bool) {
	ps := straights(b, p.P)
	ns := straights(b, p.N)
	if len(ps) == 0 || len(ns) == 0 {
		return 0, false
	}
	var gaps []float64
	for _, a := range ps {
		for _, c := range ns {
			if a.Layer != c.Layer {
				continue
			}
			da := a.End.Sub(a.Start)
			dc := c.End.Sub(c.Start)
			if da.Len() < 0.5 || dc.Len() < 0.5 {
				continue
			}
			// Parallel, either way round, to within a couple of degrees.
			cos := math.Abs(da.Norm().Dot(dc.Norm()))
			if cos < 0.999 {
				continue
			}
			// Centre-to-centre across, minus the two half widths.
			d := (geom.Seg{A: a.Start, B: a.End}).DistToPoint(midpoint(c))
			gap := d - a.Width/2 - c.Width/2
			// Anything wider than a few track widths is not a coupled pair,
			// it is two tracks that happen to be parallel.
			if gap <= 0 || gap > 6*math.Max(a.Width, c.Width) {
				continue
			}
			gaps = append(gaps, gap)
		}
	}
	if len(gaps) == 0 {
		return 0, false
	}
	sort.Float64s(gaps)
	return gaps[len(gaps)/2], true
}

func straights(b *board.Board, net string) []*board.Track {
	var out []*board.Track
	for _, t := range b.TracksOfNet(net) {
		if t.Kind == board.KindSegment && t.Width > 0 {
			out = append(out, t)
		}
	}
	return out
}

func midpoint(t *board.Track) geom.Pt {
	return geom.Pt{X: (t.Start.X + t.End.X) / 2, Y: (t.Start.Y + t.End.Y) / 2}
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// Impedance is what an interface's geometry comes out at, against what its
// family usually asks for.
type ImpedanceCheck struct {
	// SingleEnded and Differential are the computed figures.
	SingleEnded, Differential board.Impedance

	// TargetSingleEnded and TargetDifferential are the family's usual numbers,
	// zero where it has none worth stating.
	TargetSingleEnded, TargetDifferential float64

	// TargetNote qualifies the targets.
	TargetNote string

	// Layer is what the figures were computed for, and LayerAssumed is true
	// when nothing was routed to read it from.
	Layer        string
	LayerAssumed bool
}

// CheckImpedance computes the impedance of an interface's geometry.
//
// It is an estimate from closed-form models and the stackup in the board file.
// A board that has to hold its impedance to a few percent needs the
// fabricator's own stackup and a field solver; this is here to catch the case
// where the width drawn is nowhere near what the interface wants, which is a
// different and much more common problem.
func (i *Interface) CheckImpedance(b *board.Board, g Geometry) ImpedanceCheck {
	out := ImpedanceCheck{Layer: g.Layer}
	if out.Layer == "" && len(b.CopperLayers) > 0 {
		// Nothing is routed, so which layer this ends up on is not known. The
		// top layer is the guess, and it is a consequential one -- microstrip
		// and stripline differ by far more than the models' own error -- so it
		// is said rather than assumed silently.
		out.Layer = b.CopperLayers[0]
		out.LayerAssumed = true
	}
	if f, ok := FamilyOf(i.Kind); ok {
		out.TargetSingleEnded = f.SingleEndedOhms
		out.TargetDifferential = f.DiffOhms
		out.TargetNote = f.OhmsNote
	}
	if g.WidthMM <= 0 {
		return out
	}
	out.SingleEnded = b.Stackup.TrackImpedance(out.Layer, g.WidthMM, 0)
	if len(i.Pairs) > 0 {
		out.Differential = b.Stackup.DiffImpedance(out.Layer, g.WidthMM, g.GapMM, 0)
	}
	return out
}
