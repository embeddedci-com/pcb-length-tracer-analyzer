package preview

import (
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Turning copper into triangles.
//
// The viewer wants a triangle soup: one flat float32 array of x,y pairs, three
// vertices to a triangle, with an index saying which run of vertices belongs to
// which layer and net. Nothing here is about accuracy of analysis -- the
// clearance checker works on the real shapes -- so the only requirements are
// that a shape look like itself on screen and that the buffer not be
// needlessly large.
//
// Every copper item except a zone fill is convex: a track is a capsule, a via
// is a disc, and KiCad's pad shapes are rectangles, ovals, trapezoids and
// discs, each possibly with a corner radius. A convex outline fans from any one
// of its own vertices, so those need no triangulation algorithm at all. Zone
// fills are the exception and are ear-clipped.

// flatness is how far an arc's chord may sit from the true curve, in
// millimetres. It sets how many segments a rounded corner gets.
//
// Five micrometres is the useful setting, and it was measured rather than
// guessed: at 0.02 mm a via reads as a visible decagon once a meander fills the
// view, and it costs a tenth of a millimetre of area on a 0.3 mm pad. At
// 0.005 mm a via is an 18-gon and within 0.7% of its true area, and the whole
// demo board still comes to under two megabytes of triangles.
const flatness = 0.005

// arcSteps is how many segments to spend on a half turn of radius r.
func arcSteps(r float64) int {
	if r <= flatness {
		return 2
	}
	// The chord of an angle t sits r(1-cos(t/2)) inside the arc.
	t := 2 * math.Acos(1-flatness/r)
	n := int(math.Ceil(math.Pi / t))
	if n < 2 {
		return 2
	}
	if n > 24 {
		return 24
	}
	return n
}

// outline returns the boundary of a shape, counter-clockwise.
//
// This is the shape's true boundary -- the Minkowski sum of its polygon with a
// disc of radius R -- rather than the polygon itself, because that is what the
// shape means everywhere else in this tool: a track is its centreline swept by
// its width, and drawing the centreline would show a board with no copper on
// it.
func outline(s geom.RoundPoly) []geom.Pt {
	switch len(s.Pts) {
	case 0:
		return nil
	case 1:
		return circle(s.Pts[0], s.R)
	}
	pts := ccw(s.Pts)
	if s.R <= 0 {
		return pts
	}
	if len(pts) == 2 {
		return capsule(pts[0], pts[1], s.R)
	}
	return inflate(pts, s.R)
}

func circle(c geom.Pt, r float64) []geom.Pt {
	if r <= 0 {
		return nil
	}
	n := 2 * arcSteps(r)
	out := make([]geom.Pt, 0, n)
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64(i) / float64(n)
		out = append(out, geom.Pt{X: c.X + r*math.Cos(a), Y: c.Y + r*math.Sin(a)})
	}
	return out
}

// capsule is a segment swept by a disc: two parallel sides and two half-turn
// caps.
func capsule(a, b geom.Pt, r float64) []geom.Pt {
	d := b.Sub(a)
	if d.Len() < 1e-12 {
		return circle(a, r)
	}
	n := d.Norm().Perp()
	steps := arcSteps(r)
	out := make([]geom.Pt, 0, 2*steps+2)
	out = append(out, a.Add(n.Mul(r)))
	out = append(out, b.Add(n.Mul(r)))
	base := n.Angle()
	for i := 1; i < steps; i++ { // cap at b, turning from +n to -n
		t := base - math.Pi*float64(i)/float64(steps)
		out = append(out, geom.Pt{X: b.X + r*math.Cos(t), Y: b.Y + r*math.Sin(t)})
	}
	out = append(out, b.Sub(n.Mul(r)))
	out = append(out, a.Sub(n.Mul(r)))
	for i := 1; i < steps; i++ { // cap at a, turning from -n back to +n
		t := base + math.Pi + math.Pi*float64(i)/float64(steps)
		out = append(out, geom.Pt{X: a.X + r*math.Cos(t), Y: a.Y + r*math.Sin(t)})
	}
	return out
}

// inflate grows a convex counter-clockwise polygon by r: each edge moves out
// along its own normal, and the corners between them become arcs.
func inflate(pts []geom.Pt, r float64) []geom.Pt {
	n := len(pts)
	out := make([]geom.Pt, 0, n*4)
	for i := range pts {
		prev, here, next := pts[(i+n-1)%n], pts[i], pts[(i+1)%n]
		in := here.Sub(prev)
		eo := next.Sub(here)
		if in.Len() < 1e-12 || eo.Len() < 1e-12 {
			continue
		}
		// Outward normals of the two edges meeting at this vertex. For a
		// counter-clockwise polygon that is the clockwise perpendicular.
		a0 := in.Norm().Perp().Mul(-1).Angle()
		a1 := eo.Norm().Perp().Mul(-1).Angle()
		sweep := a1 - a0
		for sweep <= -math.Pi {
			sweep += 2 * math.Pi
		}
		for sweep > math.Pi {
			sweep -= 2 * math.Pi
		}
		steps := 1
		if sweep > 1e-9 {
			steps = int(math.Ceil(float64(arcSteps(r)) * sweep / math.Pi))
			if steps < 1 {
				steps = 1
			}
		}
		for k := 0; k <= steps; k++ {
			t := a0 + sweep*float64(k)/float64(steps)
			out = append(out, geom.Pt{X: here.X + r*math.Cos(t), Y: here.Y + r*math.Sin(t)})
		}
	}
	return out
}

// ccw returns the points wound counter-clockwise.
func ccw(pts []geom.Pt) []geom.Pt {
	if signedArea(pts) >= 0 {
		return pts
	}
	out := make([]geom.Pt, len(pts))
	for i, p := range pts {
		out[len(pts)-1-i] = p
	}
	return out
}

func signedArea(pts []geom.Pt) float64 {
	var a float64
	for i := range pts {
		j := (i + 1) % len(pts)
		a += pts[i].X*pts[j].Y - pts[j].X*pts[i].Y
	}
	return a / 2
}

// fan triangulates a convex polygon from its first vertex, appending x,y pairs.
func fan(dst []float32, pts []geom.Pt) []float32 {
	for i := 1; i+1 < len(pts); i++ {
		dst = append(dst,
			float32(pts[0].X), float32(pts[0].Y),
			float32(pts[i].X), float32(pts[i].Y),
			float32(pts[i+1].X), float32(pts[i+1].Y))
	}
	return dst
}

// tessellate appends one copper shape's triangles.
func tessellate(dst []float32, s geom.RoundPoly) []float32 {
	o := outline(s)
	if len(o) < 3 {
		return dst
	}
	return fan(dst, o)
}

// earClip triangulates one simple polygon, which may be concave.
//
// Zone fills are the only copper that is not convex, and they are also the only
// copper whose shape this tool did not construct itself: KiCad writes the fill
// as it computed it, with slits where a pour goes round a via and the odd
// repeated or collinear vertex. So this has to be defensive rather than
// clever. It is O(n^2) on the vertex count, which on a 4000-point pour is a few
// million operations -- once, at export.
//
// A polygon it cannot finish costs the triangles it had already found and
// nothing else; the caller records a warning rather than failing the export.
func earClip(dst []float32, ring []geom.Pt) ([]float32, bool) {
	pts := dedupe(ring)
	if len(pts) < 3 {
		return dst, true
	}
	pts = ccw(pts)
	start := len(dst)
	want := math.Abs(signedArea(pts))

	idx := make([]int, len(pts))
	for i := range idx {
		idx[i] = i
	}
	// A guard on the number of failed attempts in a row: once every remaining
	// vertex has been tried and none is an ear, the polygon is not simple and
	// no amount of further walking will help.
	fails := 0
	for len(idx) > 3 && fails <= len(idx) {
		found := false
		for k := range idx {
			i, j, l := idx[(k+len(idx)-1)%len(idx)], idx[k], idx[(k+1)%len(idx)]
			if !isEar(pts, idx, i, j, l) {
				continue
			}
			dst = append(dst,
				float32(pts[i].X), float32(pts[i].Y),
				float32(pts[j].X), float32(pts[j].Y),
				float32(pts[l].X), float32(pts[l].Y))
			idx = append(idx[:k], idx[k+1:]...)
			found = true
			fails = 0
			break
		}
		if !found {
			fails++
			// Rotate and try again: a vertex that is not an ear now may be one
			// after its neighbours are removed.
			idx = append(idx[1:], idx[0])
		}
	}
	if len(idx) == 3 {
		a, b, c := pts[idx[0]], pts[idx[1]], pts[idx[2]]
		if math.Abs(cross(a, b, c)) > 1e-12 {
			dst = append(dst,
				float32(a.X), float32(a.Y),
				float32(b.X), float32(b.Y),
				float32(c.X), float32(c.Y))
		}
	}
	// Whether it worked is a question about area, not about finishing the loop.
	//
	// Ear clipping is only correct on a simple polygon, and it does not detect
	// that it was given something else: a self-crossing ring yields triangles
	// that overlap or leave holes, and the loop still runs out of vertices and
	// looks successful. The triangles have to come to the area the ring
	// encloses, and on a ring that crosses itself they do not.
	// The tolerance is relative plus a floor, and the floor is what catches a
	// ring whose signed area is zero because it crosses itself and its two
	// lobes cancel -- a bow tie, whose triangles have real area against an
	// expectation of none. A ring that is merely collinear draws nothing and
	// matches its own zero.
	if got := triangleArea(dst[start:]); math.Abs(got-want) > 0.001*want+1e-9 {
		return dst, false
	}
	return dst, true
}

// triangleArea is the total area of a run of triangles, as x,y pairs.
func triangleArea(verts []float32) float64 {
	var total float64
	for i := 0; i+5 < len(verts); i += 6 {
		a := geom.Pt{X: float64(verts[i]), Y: float64(verts[i+1])}
		b := geom.Pt{X: float64(verts[i+2]), Y: float64(verts[i+3])}
		c := geom.Pt{X: float64(verts[i+4]), Y: float64(verts[i+5])}
		total += math.Abs(cross(a, b, c)) / 2
	}
	return total
}

// isEar reports whether the triangle i-j-l can be cut off: it must turn the
// same way as the polygon, and contain no other vertex.
func isEar(pts []geom.Pt, idx []int, i, j, l int) bool {
	a, b, c := pts[i], pts[j], pts[l]
	if cross(a, b, c) <= 1e-12 {
		return false // reflex or degenerate
	}
	for _, m := range idx {
		if m == i || m == j || m == l {
			continue
		}
		if inTriangle(pts[m], a, b, c) {
			return false
		}
	}
	return true
}

func cross(a, b, c geom.Pt) float64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

func inTriangle(p, a, b, c geom.Pt) bool {
	d1 := cross(a, b, p)
	d2 := cross(b, c, p)
	d3 := cross(c, a, p)
	return d1 >= 0 && d2 >= 0 && d3 >= 0
}

// dedupe drops repeated consecutive points, and the closing point when a ring
// repeats its first.
func dedupe(ring []geom.Pt) []geom.Pt {
	out := make([]geom.Pt, 0, len(ring))
	for _, p := range ring {
		if len(out) > 0 && out[len(out)-1].Dist(p) < 1e-9 {
			continue
		}
		out = append(out, p)
	}
	for len(out) > 1 && out[0].Dist(out[len(out)-1]) < 1e-9 {
		out = out[:len(out)-1]
	}
	return out
}

// simplify thins a ring with Douglas-Peucker, keeping its shape to within tol.
//
// KiCad writes a pour's boundary at the resolution its filler worked at, which
// on a plane with a few hundred vias is thousands of points describing a shape
// whose interesting features are millimetres across. Thinning it is the
// difference between a megabyte of triangles and a tenth of that, and 20
// micrometres of boundary error is not visible.
func simplify(ring []geom.Pt, tol float64) []geom.Pt {
	if len(ring) < 4 {
		return ring
	}
	keep := make([]bool, len(ring))
	keep[0], keep[len(ring)-1] = true, true
	type span struct{ lo, hi int }
	stack := []span{{0, len(ring) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if s.hi <= s.lo+1 {
			continue
		}
		line := geom.Seg{A: ring[s.lo], B: ring[s.hi]}
		worst, at := -1.0, -1
		for i := s.lo + 1; i < s.hi; i++ {
			if d := line.DistToPoint(ring[i]); d > worst {
				worst, at = d, i
			}
		}
		if worst > tol && at > 0 {
			keep[at] = true
			stack = append(stack, span{s.lo, at}, span{at, s.hi})
		}
	}
	out := make([]geom.Pt, 0, len(ring))
	for i, k := range keep {
		if k {
			out = append(out, ring[i])
		}
	}
	if len(out) < 3 {
		return ring
	}
	return out
}

// group is one run of vertices in the buffer: all of one layer and one net.
type group struct {
	layer, net string
	verts      []float32
}

// sortGroups orders groups by layer then net, so the same board always produces
// the same file.
func sortGroups(gs []*group) {
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].layer != gs[j].layer {
			return gs[i].layer < gs[j].layer
		}
		return gs[i].net < gs[j].net
	})
}
