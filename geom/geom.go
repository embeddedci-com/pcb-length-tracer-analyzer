// Package geom holds the planar geometry the length engine and the clearance
// checker need: points, segments, KiCad's three-point arcs, and distance
// queries between them.
//
// Everything is in millimetres, matching the .kicad_pcb file, but positions are
// snapped to KiCad's internal 1 nm grid wherever exactness matters. Copper is
// modelled as a capsule — a segment swept by a circle of the track's width — so
// a clearance test between two tracks reduces to a segment-to-segment distance
// minus the two half-widths.
package geom

import "math"

// Eps is the tolerance below which two coordinates are the same point. KiCad
// stores nanometres, so anything under half a nanometre is noise; 1e-6 mm gives
// a comfortable margin while still being far tighter than any real feature.
const Eps = 1e-6

// Pt is a point in board coordinates (millimetres, Y increasing downwards as
// KiCad stores it).
type Pt struct {
	X, Y float64
}

// Add, Sub, Mul and the rest are the usual vector helpers.
func (p Pt) Add(q Pt) Pt        { return Pt{p.X + q.X, p.Y + q.Y} }
func (p Pt) Sub(q Pt) Pt        { return Pt{p.X - q.X, p.Y - q.Y} }
func (p Pt) Mul(s float64) Pt   { return Pt{p.X * s, p.Y * s} }
func (p Pt) Dot(q Pt) float64   { return p.X*q.X + p.Y*q.Y }
func (p Pt) Cross(q Pt) float64 { return p.X*q.Y - p.Y*q.X }
func (p Pt) Len() float64       { return math.Hypot(p.X, p.Y) }

// Norm returns the unit vector in p's direction, or the zero vector if p is
// degenerate.
func (p Pt) Norm() Pt {
	l := p.Len()
	if l < Eps {
		return Pt{}
	}
	return Pt{p.X / l, p.Y / l}
}

// Perp returns p rotated a quarter turn.
func (p Pt) Perp() Pt { return Pt{-p.Y, p.X} }

// Dist is the distance between two points.
func (p Pt) Dist(q Pt) float64 { return math.Hypot(p.X-q.X, p.Y-q.Y) }

// Near reports whether two points coincide within Eps.
func (p Pt) Near(q Pt) bool { return p.Dist(q) < Eps }

// Snap rounds a point onto KiCad's 1 nm grid. Coordinates written to the file
// must be on that grid, or the value KiCad reads back differs from the one the
// tuner computed and lengths stop adding up.
func (p Pt) Snap() Pt { return Pt{snap(p.X), snap(p.Y)} }

func snap(v float64) float64 {
	if v >= 0 {
		return math.Floor(v*1e6+0.5) / 1e6
	}
	return -math.Floor(-v*1e6+0.5) / 1e6
}

// Angle returns the direction of p in radians.
func (p Pt) Angle() float64 { return math.Atan2(p.Y, p.X) }

// Rotate turns p about the origin by a radians.
func (p Pt) Rotate(a float64) Pt {
	s, c := math.Sin(a), math.Cos(a)
	return Pt{p.X*c - p.Y*s, p.X*s + p.Y*c}
}

// ---- segments ----

// Seg is a straight line segment.
type Seg struct {
	A, B Pt
}

// Len is the segment's length.
func (s Seg) Len() float64 { return s.A.Dist(s.B) }

// Dir is the unit vector from A to B.
func (s Seg) Dir() Pt { return s.B.Sub(s.A).Norm() }

// Mid is the segment's midpoint.
func (s Seg) Mid() Pt { return Pt{(s.A.X + s.B.X) / 2, (s.A.Y + s.B.Y) / 2} }

// At returns the point a fraction t along the segment.
func (s Seg) At(t float64) Pt { return s.A.Add(s.B.Sub(s.A).Mul(t)) }

// Closest returns the point on the segment nearest p, and how far along the
// segment it is as a fraction in [0,1].
func (s Seg) Closest(p Pt) (Pt, float64) {
	d := s.B.Sub(s.A)
	l2 := d.Dot(d)
	if l2 < Eps*Eps {
		return s.A, 0
	}
	t := p.Sub(s.A).Dot(d) / l2
	t = math.Max(0, math.Min(1, t))
	return s.A.Add(d.Mul(t)), t
}

// DistToPoint is the distance from the segment to a point.
func (s Seg) DistToPoint(p Pt) float64 {
	c, _ := s.Closest(p)
	return c.Dist(p)
}

// DistToSeg is the minimum distance between two segments, zero if they cross.
func (s Seg) DistToSeg(t Seg) float64 {
	if s.Intersects(t) {
		return 0
	}
	return math.Min(
		math.Min(s.DistToPoint(t.A), s.DistToPoint(t.B)),
		math.Min(t.DistToPoint(s.A), t.DistToPoint(s.B)),
	)
}

// Intersects reports whether two segments cross or touch.
func (s Seg) Intersects(t Seg) bool {
	d1 := s.B.Sub(s.A)
	d2 := t.B.Sub(t.A)
	den := d1.Cross(d2)
	w := t.A.Sub(s.A)
	if math.Abs(den) < Eps*Eps {
		// Parallel: they only meet if collinear and overlapping, which the
		// endpoint distances in DistToSeg already cover.
		return false
	}
	u := w.Cross(d2) / den
	v := w.Cross(d1) / den
	return u >= -Eps && u <= 1+Eps && v >= -Eps && v <= 1+Eps
}

// Box is the segment's axis-aligned bounding box.
func (s Seg) Box() Rect { return RectFromPts(s.A, s.B) }

// ---- arcs ----

// Arc is a circular arc in KiCad's storage form: three points, with the arc
// running from Start through Mid to End. Centre, radius and sweep are derived.
//
// Storing it this way rather than as centre+angles is deliberate: it is what
// the file holds, it survives round-tripping without drift, and it makes
// "does this arc pass through that point" exact.
type Arc struct {
	Start, Mid, End Pt
}

// Degenerate reports whether the three points are collinear, in which case the
// arc is really a straight segment and must be treated as one.
func (a Arc) Degenerate() bool {
	return math.Abs(a.End.Sub(a.Start).Cross(a.Mid.Sub(a.Start))) < 1e-12
}

// Circle returns the arc's centre and radius. ok is false for a degenerate arc.
func (a Arc) Circle() (centre Pt, radius float64, ok bool) {
	ax, ay := a.Start.X, a.Start.Y
	bx, by := a.Mid.X, a.Mid.Y
	cx, cy := a.End.X, a.End.Y
	d := 2 * (ax*(by-cy) + bx*(cy-ay) + cx*(ay-by))
	if math.Abs(d) < 1e-12 {
		return Pt{}, 0, false
	}
	a2 := ax*ax + ay*ay
	b2 := bx*bx + by*by
	c2 := cx*cx + cy*cy
	ux := (a2*(by-cy) + b2*(cy-ay) + c2*(ay-by)) / d
	uy := (a2*(cx-bx) + b2*(ax-cx) + c2*(bx-ax)) / d
	ctr := Pt{ux, uy}
	return ctr, ctr.Dist(a.Start), true
}

// Sweep returns the signed total turn from Start to End going through Mid, in
// radians. The sign gives the direction: positive is counter-clockwise in
// mathematical terms, which is clockwise on screen because Y points down.
func (a Arc) Sweep() float64 {
	ctr, _, ok := a.Circle()
	if !ok {
		return 0
	}
	a1 := a.Start.Sub(ctr).Angle()
	a2 := a.Mid.Sub(ctr).Angle()
	a3 := a.End.Sub(ctr).Angle()
	// Walk start -> mid -> end taking the short way at each step. Summing the
	// two hops gives the true sweep even for a major arc, which a single
	// start-to-end difference cannot distinguish from its complement.
	return normAngle(a2-a1) + normAngle(a3-a2)
}

func normAngle(x float64) float64 {
	for x <= -math.Pi {
		x += 2 * math.Pi
	}
	for x > math.Pi {
		x -= 2 * math.Pi
	}
	return x
}

// Len is the arc's length along the curve. A degenerate arc measures as the
// straight distance from Start to End.
func (a Arc) Len() float64 {
	_, r, ok := a.Circle()
	if !ok {
		return a.Start.Dist(a.End)
	}
	return r * math.Abs(a.Sweep())
}

// Flatten approximates the arc as a polyline whose maximum deviation from the
// true curve is at most tol. This is how clearance checks handle arcs, and it
// mirrors what KiCad does with its own max_error setting.
func (a Arc) Flatten(tol float64) []Pt {
	ctr, r, ok := a.Circle()
	if !ok || r < Eps {
		return []Pt{a.Start, a.End}
	}
	sweep := a.Sweep()
	if tol <= 0 {
		tol = 0.005
	}
	// Chord sagitta for n equal steps is r(1-cos(sweep/2n)); invert for n.
	n := 1
	if tol < r {
		step := 2 * math.Acos(1-tol/r)
		if step > Eps {
			n = int(math.Ceil(math.Abs(sweep) / step))
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 512 {
		n = 512
	}
	a0 := a.Start.Sub(ctr).Angle()
	pts := make([]Pt, 0, n+1)
	for i := 0; i <= n; i++ {
		ang := a0 + sweep*float64(i)/float64(n)
		pts = append(pts, Pt{ctr.X + r*math.Cos(ang), ctr.Y + r*math.Sin(ang)})
	}
	// Pin the ends to the stored points so the polyline shares endpoints with
	// the neighbouring items exactly.
	pts[0] = a.Start
	pts[len(pts)-1] = a.End
	return pts
}

// ---- rectangles ----

// Rect is an axis-aligned bounding box.
type Rect struct {
	MinX, MinY, MaxX, MaxY float64
}

// RectFromPts is the bounding box of the given points.
func RectFromPts(pts ...Pt) Rect {
	r := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range pts {
		r = r.Include(p)
	}
	return r
}

// Include grows the box to contain p.
func (r Rect) Include(p Pt) Rect {
	return Rect{
		math.Min(r.MinX, p.X), math.Min(r.MinY, p.Y),
		math.Max(r.MaxX, p.X), math.Max(r.MaxY, p.Y),
	}
}

// Union grows the box to contain another box.
func (r Rect) Union(o Rect) Rect {
	return Rect{
		math.Min(r.MinX, o.MinX), math.Min(r.MinY, o.MinY),
		math.Max(r.MaxX, o.MaxX), math.Max(r.MaxY, o.MaxY),
	}
}

// Grow expands the box by d on every side.
func (r Rect) Grow(d float64) Rect {
	return Rect{r.MinX - d, r.MinY - d, r.MaxX + d, r.MaxY + d}
}

// Overlaps reports whether two boxes intersect.
func (r Rect) Overlaps(o Rect) bool {
	return r.MinX <= o.MaxX && o.MinX <= r.MaxX && r.MinY <= o.MaxY && o.MinY <= r.MaxY
}

// Contains reports whether p is inside the box.
func (r Rect) Contains(p Pt) bool {
	return p.X >= r.MinX && p.X <= r.MaxX && p.Y >= r.MinY && p.Y <= r.MaxY
}

// Empty reports whether the box has never had a point added.
func (r Rect) Empty() bool { return r.MinX > r.MaxX }

// Centre is the box's midpoint.
func (r Rect) Centre() Pt { return Pt{(r.MinX + r.MaxX) / 2, (r.MinY + r.MaxY) / 2} }

// ---- polygons ----

// Poly is a closed polygon, used for zone outlines and custom pad shapes. The
// closing edge from the last point back to the first is implied.
type Poly []Pt

// Contains reports whether p is inside the polygon, by ray casting.
func (g Poly) Contains(p Pt) bool {
	in := false
	n := len(g)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		pi, pj := g[i], g[j]
		if (pi.Y > p.Y) != (pj.Y > p.Y) {
			x := pi.X + (p.Y-pi.Y)/(pj.Y-pi.Y)*(pj.X-pi.X)
			if p.X < x {
				in = !in
			}
		}
	}
	return in
}

// DistToPoint is the distance from p to the polygon boundary, negative when p
// is inside. Callers that only care about "is it clear" can compare against a
// clearance directly.
func (g Poly) DistToPoint(p Pt) float64 {
	if len(g) == 0 {
		return math.Inf(1)
	}
	best := math.Inf(1)
	n := len(g)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		if d := (Seg{g[j], g[i]}).DistToPoint(p); d < best {
			best = d
		}
	}
	if g.Contains(p) {
		return -best
	}
	return best
}

// DistToSeg is the distance from a segment to the polygon boundary, zero if the
// segment is inside or crosses it.
func (g Poly) DistToSeg(s Seg) float64 {
	if len(g) == 0 {
		return math.Inf(1)
	}
	if g.Contains(s.A) || g.Contains(s.B) {
		return 0
	}
	best := math.Inf(1)
	n := len(g)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		if d := s.DistToSeg(Seg{g[j], g[i]}); d < best {
			best = d
			if best == 0 {
				return 0
			}
		}
	}
	return best
}

// Box is the polygon's bounding box.
func (g Poly) Box() Rect { return RectFromPts(g...) }

// ---- rounded polygons ----

// RoundPoly is a polygon inflated by a radius: the set of points within R of
// the outline. One type covers every copper shape the checker has to handle:
//
//   - a circular pad or a via is a single point with R = radius;
//   - a track segment is two points with R = half the track width;
//   - a rectangular pad is four points with R = 0;
//   - a roundrect or an oval pad is the same four points with R > 0;
//   - a trapezoid or a custom pad is its own point list.
//
// Having one representation means the clearance checker has exactly one
// distance routine to get right, instead of one per shape pair.
type RoundPoly struct {
	Pts []Pt
	R   float64
}

// Disc builds a circular shape.
func Disc(c Pt, r float64) RoundPoly { return RoundPoly{Pts: []Pt{c}, R: r} }

// Capsule builds a track-shaped shape: a segment with rounded ends.
func Capsule(a, b Pt, width float64) RoundPoly {
	return RoundPoly{Pts: []Pt{a, b}, R: width / 2}
}

// DistToPoint is the distance from p to the shape's surface, negative when p is
// inside it.
func (s RoundPoly) DistToPoint(p Pt) float64 {
	switch len(s.Pts) {
	case 0:
		return math.Inf(1)
	case 1:
		return s.Pts[0].Dist(p) - s.R
	case 2:
		return (Seg{s.Pts[0], s.Pts[1]}).DistToPoint(p) - s.R
	default:
		return Poly(s.Pts).DistToPoint(p) - s.R
	}
}

// DistToSeg is the distance from the segment to the shape's surface, clamped to
// zero once they touch. It is the workhorse of the clearance checker: copper is
// flattened to segments, and each is tested against every nearby shape.
func (s RoundPoly) DistToSeg(g Seg) float64 {
	var d float64
	switch len(s.Pts) {
	case 0:
		return math.Inf(1)
	case 1:
		d = g.DistToPoint(s.Pts[0])
	case 2:
		d = g.DistToSeg(Seg{s.Pts[0], s.Pts[1]})
	default:
		d = Poly(s.Pts).DistToSeg(g)
	}
	return d - s.R
}

// Dist is the clearance between two shapes: the gap between their surfaces,
// negative when they overlap.
func (s RoundPoly) Dist(o RoundPoly) float64 {
	switch {
	case len(s.Pts) == 0 || len(o.Pts) == 0:
		return math.Inf(1)
	case len(o.Pts) == 1:
		return s.DistToPoint(o.Pts[0]) - o.R
	case len(o.Pts) == 2:
		return s.DistToSeg(Seg{o.Pts[0], o.Pts[1]}) - o.R
	}
	// Polygon against polygon: the minimum over o's edges, which is exact
	// because at least one of the two closest points lies on an edge.
	best := math.Inf(1)
	n := len(o.Pts)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		if d := s.DistToSeg(Seg{o.Pts[j], o.Pts[i]}); d < best {
			best = d
		}
	}
	return best - o.R
}

// Box is the shape's bounding box, inflated by R.
func (s RoundPoly) Box() Rect { return RectFromPts(s.Pts...).Grow(s.R) }

// Contains reports whether p lies within the shape.
func (s RoundPoly) Contains(p Pt) bool { return s.DistToPoint(p) <= Eps }

// RectShape builds an axis-aligned rectangle of the given size centred on c,
// rotated by rot radians, with corners rounded by r.
func RectShape(c Pt, w, h, rot, r float64) RoundPoly {
	// Shrink the rectangle by the corner radius and inflate it back with R, so
	// the outer dimensions come out as w by h.
	hw, hh := w/2-r, h/2-r
	if hw < 0 {
		hw = 0
	}
	if hh < 0 {
		hh = 0
	}
	corners := []Pt{{-hw, -hh}, {hw, -hh}, {hw, hh}, {-hw, hh}}
	pts := make([]Pt, 0, 4)
	for _, p := range corners {
		pts = append(pts, p.Rotate(rot).Add(c))
	}
	// A fully rounded axis (hw or hh collapsed to zero) degenerates to a
	// capsule; dropping the duplicate points keeps the distance routines on
	// their exact paths instead of the polygon one.
	return RoundPoly{Pts: dedupe(pts), R: r}
}

// OvalShape builds a stadium-shaped pad.
func OvalShape(c Pt, w, h, rot float64) RoundPoly {
	if w >= h {
		r := h / 2
		half := w/2 - r
		a := Pt{-half, 0}.Rotate(rot).Add(c)
		b := Pt{half, 0}.Rotate(rot).Add(c)
		return RoundPoly{Pts: dedupe([]Pt{a, b}), R: r}
	}
	r := w / 2
	half := h/2 - r
	a := Pt{0, -half}.Rotate(rot).Add(c)
	b := Pt{0, half}.Rotate(rot).Add(c)
	return RoundPoly{Pts: dedupe([]Pt{a, b}), R: r}
}

// TrapShape builds a trapezoid pad: a rectangle whose one axis is skewed by
// delta, as KiCad's rect_delta describes it.
func TrapShape(c Pt, w, h, rot float64, delta Pt) RoundPoly {
	hw, hh := w/2, h/2
	// A positive delta.Y widens the top and narrows the bottom, and vice versa
	// for delta.X, matching pcbnew's own construction.
	dx, dy := delta.Y/2, delta.X/2
	corners := []Pt{
		{-hw - dx, -hh - dy},
		{hw + dx, -hh + dy},
		{hw - dx, hh - dy},
		{-hw + dx, hh + dy},
	}
	pts := make([]Pt, 0, 4)
	for _, p := range corners {
		pts = append(pts, p.Rotate(rot).Add(c))
	}
	return RoundPoly{Pts: dedupe(pts)}
}

func dedupe(pts []Pt) []Pt {
	out := pts[:0:0]
	for _, p := range pts {
		dup := false
		for _, q := range out {
			if p.Near(q) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}
