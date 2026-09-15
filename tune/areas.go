package tune

import (
	"math"

	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Where the tool is allowed to put copper.
//
// Length matching decides how much to add. It does not decide where, and
// "wherever there is room" is not the same answer as "wherever you would want
// it". Room beside a trace can be in a BGA fanout, under a connector, across a
// split in a reference plane, or in the one part of the board somebody has left
// clear on purpose -- and none of that is visible to a clearance check, which
// only knows whether copper fits.
//
// So the person who drew the board says where. An empty list means anywhere,
// which is what the tool did before and is right for a first look; once areas
// are given, nothing is added outside them.
//
// The areas are in the board's own coordinates, the same ones the file uses.
// The front end works in the viewer's -- origin at the bottom-left corner of
// the board, Y up -- and converts on the way in, which is the one place that
// conversion happens.

// Area is one region copper may be added in, and the spacing that applies
// there.
//
// The spacing lives on the area rather than in a global setting because that is
// where the user actually knows it. "Keep 0.25 mm apart away from the
// components" needs the tool to work out what "away from the components" means,
// and it guesses with a margin around every courtyard; "keep 0.25 mm apart in
// here" needs nothing guessed, because the user drew the here.
type Area struct {
	geom.Rect

	// MinClearance is the least space between different nets' copper inside
	// this area, in millimetres. Zero leaves the board's own rules in force.
	//
	// It only ever tightens. An area is somewhere the user has chosen to put
	// copper, and asking for more room between traces there is a choice they
	// are entitled to make; letting it override a clearance the board demands
	// would be a different thing entirely.
	MinClearance float64

	// Label is what the user called it, for reports.
	Label string
}

// Areas are the regions copper may be added in. An empty Areas means anywhere.
type Areas []Area

// Allows reports whether a point is somewhere copper may be added.
func (a Areas) Allows(p geom.Pt) bool {
	if len(a) == 0 {
		return true
	}
	for _, r := range a {
		if r.Contains(p) {
			return true
		}
	}
	return false
}

// AllowsSpan reports whether a whole stretch is allowed, ends included.
//
// The ends and the middle, because a meander folded into a stretch wanders
// either side of it along its whole length: a stretch half inside an area is
// not half allowed, it is a meander that leaves the area.
func (a Areas) AllowsSpan(from, to geom.Pt) bool {
	if len(a) == 0 {
		return true
	}
	mid := geom.Pt{X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2}
	for _, r := range a {
		if r.Contains(from) && r.Contains(to) && r.Contains(mid) {
			return true
		}
	}
	return false
}

// RoomToward is how far a stretch may excurse in a direction and stay inside
// an allowed area.
//
// Checking that the stretch itself is inside is not enough. A meander wanders
// sideways from the track it is folded into, by as much as the amplitude, so a
// stretch a tenth of a millimetre inside the boundary can put copper outside
// it -- which is exactly what happened: 26 samples of new copper landed up to
// 0.05 mm beyond the edge of the area somebody had drawn. The area is a
// statement about where copper may be, not about where centrelines may be.
//
// Returns +Inf when no areas are set, which is the "anywhere" case.
func (a Areas) RoomToward(from, to geom.Pt, side geom.Pt) float64 {
	if len(a) == 0 {
		return math.Inf(1)
	}
	best := 0.0
	for _, r := range a {
		if !r.Contains(from) || !r.Contains(to) {
			continue
		}
		d := math.Min(edgeDistance(r.Rect, from, side), edgeDistance(r.Rect, to, side))
		if d > best {
			best = d
		}
	}
	return best
}

// edgeDistance is how far a point may travel along a direction before leaving a
// rectangle.
func edgeDistance(r geom.Rect, p geom.Pt, d geom.Pt) float64 {
	lim := math.Inf(1)
	if d.X > 1e-12 {
		lim = math.Min(lim, (r.MaxX-p.X)/d.X)
	} else if d.X < -1e-12 {
		lim = math.Min(lim, (r.MinX-p.X)/d.X)
	}
	if d.Y > 1e-12 {
		lim = math.Min(lim, (r.MaxY-p.Y)/d.Y)
	} else if d.Y < -1e-12 {
		lim = math.Min(lim, (r.MinY-p.Y)/d.Y)
	}
	if lim < 0 || math.IsInf(lim, 1) {
		return 0
	}
	return lim
}

// Overlaps reports whether any area touches a box, which is the cheap test for
// whether a track is worth looking at at all.
func (a Areas) Overlaps(box geom.Rect) bool {
	if len(a) == 0 {
		return true
	}
	for _, r := range a {
		if r.Overlaps(box) {
			return true
		}
	}
	return false
}

// SetAreas restricts where this tuner may add copper, and with what spacing.
// An empty list lifts the restriction.
func (t *Tuner) SetAreas(a Areas) {
	t.areas = a
	t.chk = t.rebuild()
}

// Rects is the areas as plain rectangles.
func (a Areas) Rects() []geom.Rect {
	out := make([]geom.Rect, 0, len(a))
	for _, r := range a {
		out = append(out, r.Rect)
	}
	return out
}

// Spacing is the clearance zones these areas impose, for the checker.
func (a Areas) Spacing() []drc.ClearanceZone {
	var out []drc.ClearanceZone
	for _, r := range a {
		if r.MinClearance > 0 {
			out = append(out, drc.ClearanceZone{Area: r.Rect, MinClearance: r.MinClearance})
		}
	}
	return out
}

// Areas returns the restriction in force.
func (t *Tuner) Areas() Areas { return t.areas }
