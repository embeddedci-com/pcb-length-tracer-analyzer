package tune

import (
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// A bundle is a run of parallel tracks, belonging to different nets, that sit
// side by side over a common stretch of board -- a bus.
//
// Bundles matter because of a fact about real boards that defeats per-track
// tuning entirely: a bus is often routed at exactly its minimum clearance, so
// there is no room beside any one track for a meander. On the demo board the
// neighbours of one DQ24 segment sit 0.2000 mm and 0.2050 mm away against a
// 0.2 mm rule. No amount of care finds space that is not there.
//
// What a layout engineer does instead is treat the whole bus as the thing being
// tuned: spread it out across whatever room the bus as a whole has, and give
// each track its own lane to meander in. That is what this file measures and
// what bundleTune builds on.

// BundleMember is one track in a bundle.
type BundleMember struct {
	Track *board.Track
	Net   string

	// Offset is the track's position across the bundle, in millimetres from the
	// bundle axis. Negative is one side, positive the other.
	Offset float64

	// U0 and U1 are the track's extent along the bundle axis.
	U0, U1 float64

	// Width is the track width, which need not be the same for every member.
	Width float64
}

// Bundle is a set of parallel tracks on one layer, ordered across the bus.
type Bundle struct {
	Layer string

	// Origin, Dir and Side define the bundle's own coordinate frame: Dir along
	// the bus, Side across it.
	Origin    geom.Pt
	Dir, Side geom.Pt

	// U0 and U1 are the stretch of the axis every member covers.
	U0, U1 float64

	// Members are ordered by Offset, so Members[0] is the outermost on the
	// negative side.
	Members []BundleMember

	// FreeNeg and FreePos are the clear space beyond the outermost member on
	// each side, measured against everything that is not in the bundle.
	FreeNeg, FreePos float64

	// Clearance is the strictest clearance any member's net requires.
	Clearance float64
}

// Span is the length of the axis the whole bundle shares.
func (b *Bundle) Span() float64 { return b.U1 - b.U0 }

// Width is how far across the bundle reaches, outer edge to outer edge.
func (b *Bundle) Width() float64 {
	if len(b.Members) == 0 {
		return 0
	}
	first, last := b.Members[0], b.Members[len(b.Members)-1]
	return (last.Offset + last.Width/2) - (first.Offset - first.Width/2)
}

// Corridor is the total room across the bundle: what it occupies now, plus the
// clear space on either side. This is the budget everything else divides up.
func (b *Bundle) Corridor() float64 { return b.Width() + b.FreeNeg + b.FreePos }

// Nets lists the bundle's nets, sorted.
func (b *Bundle) Nets() []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range b.Members {
		if !seen[m.Net] {
			seen[m.Net] = true
			out = append(out, m.Net)
		}
	}
	sort.Strings(out)
	return out
}

// point converts bundle coordinates back to the board.
func (b *Bundle) point(u, v float64) geom.Pt {
	return b.Origin.Add(b.Dir.Mul(u)).Add(b.Side.Mul(v))
}

// BundleOptions tune how bundles are recognised.
type BundleOptions struct {
	// AngleToleranceDeg is how far two tracks' directions may differ and still
	// count as parallel.
	AngleToleranceDeg float64

	// MaxPitch is the largest gap, centre to centre, between neighbouring
	// members. Beyond it they are two buses rather than one.
	MaxPitch float64

	// MinSpan is the shortest shared stretch worth treating as a bundle.
	MinSpan float64

	// MinMembers is the fewest tracks that make a bundle. Two parallel tracks
	// are usually a differential pair, not a bus, and re-spacing a pair is a
	// different operation with different rules.
	MinMembers int
}

// DefaultBundleOptions returns sensible recognition settings.
//
// A degree and a half of angle tolerance is enough to absorb the rounding in a
// hand-routed bus without joining two buses that merely pass near each other.
// MaxPitch of 1 mm is several times the pitch of a minimum-clearance DDR bus
// and still far below the spacing of unrelated nets.
func DefaultBundleOptions() BundleOptions {
	return BundleOptions{
		AngleToleranceDeg: 1.5,
		MaxPitch:          1.0,
		MinSpan:           2.0,
		MinMembers:        3,
	}
}

// FindBundles groups the given nets' straight tracks into bundles.
//
// Only tracks on the listed nets are considered: a bundle is something the
// caller is going to re-space, so sweeping in a neighbouring bus that nobody
// asked about would be the wrong kind of helpful.
func (t *Tuner) FindBundles(nets []string, opt BundleOptions) []*Bundle {
	if opt.MinMembers <= 0 {
		opt = DefaultBundleOptions()
	}
	type cand struct {
		tr  *board.Track
		net string
	}
	byLayer := map[string][]cand{}
	for _, net := range nets {
		for _, tr := range t.b.TracksOfNet(net) {
			if tr.Kind != board.KindSegment || tr.Width <= 0 {
				continue
			}
			if tr.Length(t.b.Stackup) < opt.MinSpan {
				continue
			}
			byLayer[tr.Layer] = append(byLayer[tr.Layer], cand{tr, net})
		}
	}

	var out []*Bundle
	layers := make([]string, 0, len(byLayer))
	for l := range byLayer {
		layers = append(layers, l)
	}
	sort.Strings(layers)

	for _, layer := range layers {
		cands := byLayer[layer]
		// Sort for determinism: the same board must produce the same bundles.
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].tr.Start.X != cands[j].tr.Start.X {
				return cands[i].tr.Start.X < cands[j].tr.Start.X
			}
			if cands[i].tr.Start.Y != cands[j].tr.Start.Y {
				return cands[i].tr.Start.Y < cands[j].tr.Start.Y
			}
			return cands[i].net < cands[j].net
		})

		used := make([]bool, len(cands))
		tol := opt.AngleToleranceDeg * math.Pi / 180
		for i := range cands {
			if used[i] {
				continue
			}
			ref := cands[i].tr
			dir := ref.End.Sub(ref.Start).Norm()
			side := dir.Perp()
			origin := ref.Start

			// Everything parallel to the reference, in the reference's frame.
			type proj struct {
				idx    int
				offset float64
				u0, u1 float64
			}
			var group []proj
			for j := range cands {
				if used[j] {
					continue
				}
				o := cands[j].tr
				od := o.End.Sub(o.Start).Norm()
				if !parallelWithin(dir, od, tol) {
					continue
				}
				a := o.Start.Sub(origin)
				c := o.End.Sub(origin)
				// A parallel track has both ends at the same offset; average
				// them so a hair of non-parallelism does not matter.
				offset := (a.Dot(side) + c.Dot(side)) / 2
				u0, u1 := a.Dot(dir), c.Dot(dir)
				if u0 > u1 {
					u0, u1 = u1, u0
				}
				group = append(group, proj{j, offset, u0, u1})
			}
			if len(group) < opt.MinMembers {
				continue
			}
			sort.Slice(group, func(a, c int) bool { return group[a].offset < group[c].offset })

			// Walk across, breaking wherever the gap is too wide or the shared
			// span would collapse.
			start := 0
			for k := 1; k <= len(group); k++ {
				split := k == len(group)
				if !split {
					gap := group[k].offset - group[k-1].offset
					// Same offset means two collinear tracks of one net, not a
					// neighbour; a gap wider than MaxPitch is a different bus.
					split = gap > opt.MaxPitch
				}
				if !split {
					continue
				}
				run := group[start:k]
				start = k
				if len(run) < opt.MinMembers {
					continue
				}
				u0, u1 := run[0].u0, run[0].u1
				for _, p := range run[1:] {
					u0 = math.Max(u0, p.u0)
					u1 = math.Min(u1, p.u1)
				}
				if u1-u0 < opt.MinSpan {
					continue
				}
				bundle := &Bundle{
					Layer: layer, Origin: origin, Dir: dir, Side: side,
					U0: u0, U1: u1,
				}
				for _, p := range run {
					used[p.idx] = true
					c := cands[p.idx]
					bundle.Members = append(bundle.Members, BundleMember{
						Track: c.tr, Net: c.net, Offset: p.offset,
						U0: p.u0, U1: p.u1, Width: c.tr.Width,
					})
					if cl := t.proj.ClearanceOf(c.net); cl > bundle.Clearance {
						bundle.Clearance = cl
					}
				}
				t.measureFreeSpace(bundle)
				out = append(out, bundle)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Span() != out[j].Span() {
			return out[i].Span() > out[j].Span()
		}
		return len(out[i].Members) > len(out[j].Members)
	})
	return out
}

// parallelWithin reports whether two unit directions are parallel, in either
// sense, to within tol radians. Either sense matters because a bus does not
// care which way each track was drawn.
func parallelWithin(a, b geom.Pt, tol float64) bool {
	dot := math.Abs(a.Dot(b))
	if dot > 1 {
		dot = 1
	}
	return math.Acos(dot) <= tol
}

// measureFreeSpace finds how far the bundle could spread on each side.
//
// The probe is the swept area a widened bundle would occupy, tested against
// everything that is not a bundle member, at the strictest clearance any member
// requires. Bisection finds the limit in about a dozen queries.
func (t *Tuner) measureFreeSpace(b *Bundle) {
	exempt := map[string]bool{}
	for _, m := range b.Members {
		if m.Track.UUID != "" {
			exempt[m.Track.UUID] = true
		}
		// A member's own neighbours, and its pads, are things it is already
		// joined to rather than obstacles.
		for _, o := range t.b.TracksOfNet(m.Net) {
			for _, x := range []geom.Pt{m.Track.Start, m.Track.End} {
				for _, y := range []geom.Pt{o.Start, o.End} {
					if x.Dist(y) < 0.1 && o.UUID != "" {
						exempt[o.UUID] = true
					}
				}
			}
		}
		for _, p := range t.b.PadsOfNet(m.Net) {
			exempt[p.ID()] = true
		}
	}

	first, last := b.Members[0], b.Members[len(b.Members)-1]
	negEdge := first.Offset - first.Width/2
	posEdge := last.Offset + last.Width/2
	width := math.Max(first.Width, last.Width)

	fits := func(from, to float64) bool {
		if to-from <= 0 {
			return true
		}
		var shapes []geom.RoundPoly
		steps := int(math.Ceil((to-from)/width)) + 1
		for i := 0; i <= steps; i++ {
			v := from + (to-from)*float64(i)/float64(steps)
			shapes = append(shapes,
				geom.Capsule(b.point(b.U0, v), b.point(b.U1, v), width))
		}
		return t.chk.Clear(drc.Candidate{
			Shapes: shapes, Layer: b.Layer, Net: first.Net,
			Exempt: exempt, MinClearance: b.Clearance,
		})
	}

	// Outward from each edge, leaving the bundle itself alone.
	b.FreeNeg = bisectFree(func(d float64) bool { return fits(negEdge-d, negEdge-width/2) })
	b.FreePos = bisectFree(func(d float64) bool { return fits(posEdge+width/2, posEdge+d) })
}

// bisectFree returns the largest distance for which fits still holds, searching
// up to a few millimetres. Nothing on a board needs more room than that, and
// bounding the search keeps the probe cheap.
func bisectFree(fits func(float64) bool) float64 {
	const max = 5.0
	if fits(max) {
		return max
	}
	lo, hi := 0.0, max
	if !fits(0.05) {
		return 0
	}
	lo = 0.05
	for range 12 {
		mid := (lo + hi) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}
