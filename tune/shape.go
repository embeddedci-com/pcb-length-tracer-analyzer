package tune

import (
	"fmt"
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// meanderShape describes a serpentine folded into a straight run.
//
// The run keeps its endpoints and its axial extent; the meander buys extra
// length by stepping off the axis and back n times. Each excursion is a
// mitred rectangle: out at 45 degrees, along, back at 45 degrees.
//
//	          ┌──────┐              ┌──────┐
//	         ╱        ╲            ╱        ╲     amplitude
//	── ─────╯          ╰──────────╯          ╰────────
//	        │←  width  →│← space →│
//
// For one excursion of amplitude h, flat width w and corner chamfer c, the
// path runs 45-degree ramp, leg, ramp, flat, ramp, leg, ramp:
//
//	length = 2h + w + 4c(√2 − 2)
//	axial  = w
//
// so the length gained over going straight is
//
//	gain = 2h − 4c(2 − √2)
//
// which is exact, not an approximation, and is what lets the tuner land on a
// target length rather than near it. The chamfers cost about 2.34c of the 2h
// gained, which is the price of not having square corners.
type meanderShape struct {
	from, to geom.Pt
	dir      geom.Pt
	side     geom.Pt

	amp     float64 // h
	flat    float64 // w
	space   float64 // gap between excursions, along the axis
	chamfer float64 // c
	count   int     // n
}

// gainPerExcursion is the extra length one excursion adds.
func (m meanderShape) gainPerExcursion() float64 {
	return 2*m.amp - 4*m.chamfer*(2-math.Sqrt2)
}

// gain is the extra length the whole meander adds.
func (m meanderShape) gain() float64 { return float64(m.count) * m.gainPerExcursion() }

// axial is how much of the run the meander occupies.
func (m meanderShape) axial() float64 {
	return float64(m.count)*m.flat + float64(m.count-1)*m.space
}

// valid reports whether the shape is geometrically constructible.
func (m meanderShape) valid() bool {
	return m.count >= 1 &&
		m.amp >= 2*m.chamfer &&
		m.flat >= 4*m.chamfer &&
		m.space >= 0 &&
		m.chamfer >= 0 &&
		m.axial() <= m.from.Dist(m.to)+1e-9
}

// polyline returns the meander's centreline, from the run's start to its end.
//
// Excursions are centred in the run, so the meander sits in the middle of the
// straight rather than crowding one end.
func (m meanderShape) polyline() []geom.Pt {
	u, v := m.dir, m.side
	total := m.from.Dist(m.to)
	lead := (total - m.axial()) / 2

	at := func(axial, perp float64) geom.Pt {
		return m.from.Add(u.Mul(axial)).Add(v.Mul(perp))
	}

	c := m.chamfer
	pts := []geom.Pt{m.from}
	x := lead
	pts = append(pts, at(x, 0))
	for i := range m.count {
		if i > 0 {
			x += m.space
			pts = append(pts, at(x, 0))
		}
		// out
		pts = append(pts, at(x+c, c))
		pts = append(pts, at(x+c, m.amp-c))
		pts = append(pts, at(x+2*c, m.amp))
		// along
		pts = append(pts, at(x+m.flat-2*c, m.amp))
		// back
		pts = append(pts, at(x+m.flat-c, m.amp-c))
		pts = append(pts, at(x+m.flat-c, c))
		pts = append(pts, at(x+m.flat, 0))
		x += m.flat
	}
	pts = append(pts, m.to)

	// Snap to KiCad's nanometre grid so the length written to the file is the
	// length that was computed.
	for i := range pts {
		pts[i] = pts[i].Snap()
	}
	return dedupeConsecutive(pts)
}

func dedupeConsecutive(pts []geom.Pt) []geom.Pt {
	out := pts[:0:0]
	for i, p := range pts {
		if i > 0 && p.Near(out[len(out)-1]) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// polylineLength is the true length of the generated centreline, measured after
// snapping rather than from the formula, so the figure reported is the figure
// in the file.
func (m meanderShape) polylineLength() float64 {
	pts := m.polyline()
	var l float64
	for i := 0; i+1 < len(pts); i++ {
		l += pts[i].Dist(pts[i+1])
	}
	return l
}

// shapes returns the copper the meander would occupy, for the clearance check.
func (m meanderShape) shapes(width float64) []geom.RoundPoly {
	pts := m.polyline()
	out := make([]geom.RoundPoly, 0, len(pts))
	for i := 0; i+1 < len(pts); i++ {
		out = append(out, geom.Capsule(pts[i], pts[i+1], width))
	}
	return out
}

// planMeander chooses the meander to fold into one stretch of track, without
// touching the board.
//
// The search is deliberately simple and bounded: try the most excursions the
// stretch has room for, and solve for the amplitude that hits the target
// exactly. If that amplitude is more than the surroundings allow, take the
// largest that is and settle for less. Work down through fewer excursions until
// something clears the design rules.
//
// The meander's own copper is held to a strict standard here -- it is going
// somewhere no copper was, so it has no excuse for being close to anything. What
// the replacement as a whole does to pre-existing violations is settled later,
// once every meander for the track is known.
func (t *Tuner) planMeander(r run, net string, width, want float64, exempt map[string]bool) (meanderShape, float64, bool) {
	if want <= 0 {
		return meanderShape{}, 0, false
	}
	chamfer := t.style.Chamfer * width
	// Gap is copper to copper, so the centre-to-centre pitch adds one width.
	space := (t.style.Gap + 1) * width
	// How wide one excursion has to be.
	//
	// Its two legs sit a chamfer in from each edge of the flat, so they are
	// flat - 2c apart, and that is what has to be at least the configured
	// spacing -- not the flat itself. Getting this wrong leaves the legs one
	// track width apart instead of three, and the mitre ramps closing on the
	// neighbouring leg diagonally make it tighter still. The flat also has to
	// hold two chamfers going out and two coming back.
	flat := math.Max(space+2*chamfer, 4*chamfer)

	maxCount := int(math.Floor((r.length + space) / (flat + space)))
	if maxCount < 1 {
		return meanderShape{}, 0, false
	}
	minAmp := math.Max(t.style.MinAmplitude, 2*chamfer)

	for count := maxCount; count >= 1; count-- {
		m := meanderShape{
			from: r.from, to: r.to, dir: r.to.Sub(r.from).Norm(), side: r.side,
			flat: flat, space: space, chamfer: chamfer, count: count,
		}
		// Amplitude that would gain exactly what is wanted.
		exact := (want/float64(count) + 4*chamfer*(2-math.Sqrt2)) / 2
		amp := math.Min(exact, r.amp)
		if amp < minAmp {
			// Even the smallest excursion this style allows would overshoot;
			// fewer excursions may fit the requirement better.
			if exact < minAmp {
				continue
			}
			amp = minAmp
		}
		m.amp = amp
		if !m.valid() {
			continue
		}
		if !t.chk.Clear(drc.Candidate{
			Shapes: m.shapes(width), Layer: r.track.Layer, Net: net, Exempt: exempt,
		}) {
			continue
		}
		// Measure what would actually be drawn, not what was intended.
		got := m.polylineLength() - r.from.Dist(r.to)
		if got <= 1e-6 {
			continue
		}
		return m, got, true
	}
	return meanderShape{}, 0, false
}

// applyTrack replaces one track with a chain carrying every meander chosen for
// it, and reports how much length that added.
//
// All of a track's meanders go in together, and that is not an optimisation.
// Room beside a track is not uniform, so one track often offers several usable
// stretches; replacing it once per stretch would leave the second edit holding
// a pointer to a track that no longer exists. An earlier version did exactly
// that: the replacement silently did nothing while the length was still counted,
// and DDR_A11 was reported 0.234 mm longer than KiCad could find.
func (t *Tuner) applyTrack(orig *board.Track, ms []meanderShape, net string, width float64, exempt map[string]bool) (float64, int, error) {
	if len(ms) == 0 {
		return 0, 0, nil
	}
	pts := t.replacementPolyline(orig, ms)
	if len(pts) < 2 {
		return 0, 0, fmt.Errorf("tune: meander on %s degenerated to a point", net)
	}
	if !pts[0].Near(orig.Start) || !pts[len(pts)-1].Near(orig.End) {
		return 0, 0, fmt.Errorf("tune: meander on %s would move the track's endpoints", net)
	}

	// The whole replacement -- meanders and the untouched stretches of the
	// original alike -- must be no worse than the track it replaces. Those
	// stretches lie on the original copper, so on a board with pre-existing
	// violations they inherit them; what must not happen is any clearance
	// getting tighter, or anything new being clashed with.
	base := t.chk.Worst(drc.Candidate{
		Shapes: orig.Shape(t.b.MaxError), Layer: orig.Layer, Net: net, Exempt: exempt,
	})
	cand := drc.Candidate{Layer: orig.Layer, Net: net, Exempt: exempt}
	for i := 0; i+1 < len(pts); i++ {
		cand.Shapes = append(cand.Shapes, geom.Capsule(pts[i], pts[i+1], width))
	}
	if ok, _ := drc.NoWorseThan(base, t.chk.Worst(cand)); !ok {
		return 0, 0, nil
	}

	replacement := make([]*board.Track, 0, len(pts)-1)
	for i := 0; i+1 < len(pts); i++ {
		replacement = append(replacement, &board.Track{
			Kind:  board.KindSegment,
			Net:   net,
			Start: pts[i],
			End:   pts[i+1],
			Width: width,
			Layer: orig.Layer,
			UUID:  newUUID(),
		})
	}
	if !t.b.ReplaceTrack(orig, replacement) {
		return 0, 0, fmt.Errorf("tune: the track being lengthened on %s is no longer on the board", net)
	}

	var before, after float64
	before = orig.Length(t.b.Stackup)
	for i := 0; i+1 < len(pts); i++ {
		after += pts[i].Dist(pts[i+1])
	}
	return after - before, len(replacement), nil
}

// replacementPolyline is the full path that will replace a track: its untouched
// stretches, with every chosen meander folded in where it belongs.
//
// The meanders are placed in axial order and must not overlap, which is what
// findRuns guarantees when it picks non-overlapping stretches.
func (t *Tuner) replacementPolyline(orig *board.Track, ms []meanderShape) []geom.Pt {
	dir := orig.End.Sub(orig.Start).Norm()
	sorted := append([]meanderShape{}, ms...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].from.Sub(orig.Start).Dot(dir) < sorted[j].from.Sub(orig.Start).Dot(dir)
	})
	out := []geom.Pt{orig.Start}
	for _, m := range sorted {
		pts := m.polyline()
		// Join the previous point to this meander's start along the original
		// line, then take the meander itself.
		for _, p := range pts {
			if p.Near(out[len(out)-1]) {
				continue
			}
			out = append(out, p)
		}
	}
	if !orig.End.Near(out[len(out)-1]) {
		out = append(out, orig.End)
	}
	return dedupeConsecutive(out)
}
