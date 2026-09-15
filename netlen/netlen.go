// Package netlen measures routed nets: how long each one is, how long it takes
// a signal to cross it, and which pads are actually joined by copper.
//
// The work is done on a graph built from the net's own copper. Tracks are
// edges; their endpoints, the points where one track lands partway along
// another, and the pads themselves are nodes. Vias stitch layers together. A
// net's length is then a question about paths in that graph rather than a sum
// over items, which matters because a fly-by net branches: adding up all its
// copper counts the stubs, and KiCad does not.
//
// Reproducing KiCad's own number exactly is a deliberate goal. A tool that
// reports a length the user cannot see in pcbnew's net inspector is a tool the
// user cannot check, so the engine is validated against lengths extracted from
// kicad-cli itself.
package netlen

import (
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// tol is the distance below which two copper features are treated as the same
// point. pcbnew writes coincident endpoints as identical decimals, so this only
// has to absorb floating-point noise and the odd rounding in a hand-edited
// file; 0.1 micrometre is four orders of magnitude below any real feature.
const tol = 1e-4

// Path is a measured route between two pads.
type Path struct {
	From, To string

	// Length is the geometric length in millimetres, including via barrels
	// when the board counts them, and pad die lengths at both ends.
	Length float64

	// Delay is the propagation delay in picoseconds, computed per layer so
	// that a microstrip millimetre and a stripline millimetre are not treated
	// as equal.
	Delay float64

	// Vias is how many via barrels the route passes through.
	Vias int

	// Found is false when no copper joins the two pads.
	Found bool

	// Tracks are the uuids of the copper the route runs over.
	//
	// A net can have copper that is on no route at all -- a dangling stub, or
	// a whole island stranded because a leg was never finished. Lengthening
	// one of those adds copper without changing any timing path, so anything
	// that edits a net to change its length has to know which tracks are
	// actually on the path being changed.
	Tracks []string
}

// Measure is everything known about one net's routing.
type Measure struct {
	Net string

	// Pads lists the net's pads, sorted, as "U3.K18".
	Pads []string

	// TotalCopper is the length of every piece of copper on the net, connected
	// or not. It over-counts a branching net and says nothing about whether a
	// signal can actually cross it, so no matching decision is based on it.
	TotalCopper float64

	// KiCadLength reproduces the figure pcbnew's net inspector and its length
	// design rule report, so that anything this tool says can be checked in
	// the GUI.
	//
	// That figure is a sum, not a route: it adds up every piece of copper on
	// the net -- dangling stubs, both sides of a loop, and even islands no
	// signal could reach -- plus the barrel of every via that has net copper on
	// at least two layers. On a finished board it equals the route; on a board
	// still being routed it can be much larger, and the difference from
	// PathLength is the signal that something is unrouted.
	//
	// pcbnew additionally shortens the part of a track that lies inside a pad,
	// by an amount that depends on how it polygonises the pad outline. That is
	// not reproduced here, so this figure can read up to a few tenths of a
	// millimetre high on nets whose copper runs across a BGA pad.
	KiCadLength float64

	// Longest is the longest pad-to-pad route on the net. This is the single
	// number KiCad's net inspector and its length DRC constraint report.
	Longest Path

	// Paths holds every pad-pair route, keyed by "U3.K18->U5.P3" with the
	// endpoints in sorted order. Per-leg matching on a fly-by net is done by
	// asking for the legs directly rather than by subtracting totals.
	Paths map[string]Path

	// Islands groups the pads into sets that copper actually joins. A fully
	// routed net has exactly one island containing every pad.
	Islands [][]string

	// Complete reports whether all the net's pads are in one island.
	Complete bool

	Segments, Arcs, Vias int
}

// PathBetween returns the route between two pads in either order.
func (m *Measure) PathBetween(a, b string) Path {
	if p, ok := m.Paths[pairKey(a, b)]; ok {
		return p
	}
	return Path{From: a, To: b}
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "->" + b
}

// Engine measures nets on a board.
type Engine struct {
	b *board.Board

	// CountViaLength mirrors the board setting of the same name: when false,
	// via barrels are treated as zero-length, as KiCad does with
	// use_height_for_length_calcs turned off.
	CountViaLength bool

	// CountDieLength adds each end pad's declared on-package routing length,
	// which KiCad includes in its own figure.
	CountDieLength bool

	// NoTJunctionSplits stops the graph from dividing a track where another
	// piece of copper lands partway along it.
	//
	// Splitting is needed for a branching net -- without it a fly-by T is
	// invisible -- but on a net that does not branch it must not change any
	// length. That makes this a validation lever rather than a setting:
	// measuring a two-pad net both ways and comparing the answers is what
	// catches a split that lets a route cut a corner instead of following the
	// centreline.
	NoTJunctionSplits bool
}

// New builds an engine for a board.
func New(b *board.Board) *Engine {
	return &Engine{b: b, CountViaLength: b.UseHeightForLength, CountDieLength: true}
}

// Measure measures one net.
func (e *Engine) Measure(net string) *Measure {
	b := e.b
	tracks := b.TracksOfNet(net)
	pads := b.PadsOfNet(net)

	m := &Measure{Net: net, Paths: map[string]Path{}, Complete: true}
	for _, p := range pads {
		m.Pads = append(m.Pads, p.ID())
	}
	sort.Strings(m.Pads)

	var segs, vias []*board.Track
	for _, t := range tracks {
		if t.Kind == board.KindVia {
			vias = append(vias, t)
			m.Vias++
			continue
		}
		segs = append(segs, t)
		m.countKind(t.Kind)
	}
	for _, t := range tracks {
		if t.Kind == board.KindVia && !e.CountViaLength {
			continue
		}
		m.TotalCopper += t.Length(b.Stackup)
	}
	m.KiCadLength = e.kicadLength(net, segs, vias)
	if len(pads) == 0 {
		return m
	}

	g, padNode := e.buildGraph(net)

	ids := make([]string, 0, len(padNode))
	for id := range padNode {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for i, a := range ids {
		distL, distD, distV, prev, viaOf := g.dijkstraFrom(padNode[a])
		for _, other := range ids[i+1:] {
			n := padNode[other]
			p := Path{From: a, To: other}
			if d := distL[n]; !math.IsInf(d, 1) {
				p.Found = true
				p.Length = d
				p.Delay = distD[n]
				p.Vias = distV[n] / 2
				p.Tracks = tracksOnRoute(prev, viaOf, n)
			}
			m.Paths[pairKey(a, other)] = p
			if p.Found && p.Length > m.Longest.Length {
				m.Longest = p
			}
		}
	}
	m.Islands = islands(ids, m.Paths)
	m.Complete = len(m.Islands) <= 1
	return m
}

func (m *Measure) countKind(k board.TrackKind) {
	switch k {
	case board.KindSegment:
		m.Segments++
	case board.KindArc:
		m.Arcs++
	}
}

// nearestOn returns the point on a track's centreline closest to q, how far
// along the track it lies as a fraction, and the distance to it. Arcs are
// handled on the true circle rather than a flattened approximation, so a
// junction on a tuning arc lands at the right place.
func nearestOn(t *board.Track, q geom.Pt) (p geom.Pt, at float64, dist float64) {
	if t.Kind == board.KindArc {
		a := geom.Arc{Start: t.Start, Mid: t.Mid, End: t.End}
		ctr, r, ok := a.Circle()
		if ok && r > geom.Eps {
			sweep := a.Sweep()
			a0 := t.Start.Sub(ctr).Angle()
			f := normTo(q.Sub(ctr).Angle()-a0, sweep) / sweep
			if f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			ang := a0 + sweep*f
			p = geom.Pt{X: ctr.X + r*math.Cos(ang), Y: ctr.Y + r*math.Sin(ang)}
			return p, f, p.Dist(q)
		}
	}
	seg := geom.Seg{A: t.Start, B: t.End}
	p, at = seg.Closest(q)
	return p, at, p.Dist(q)
}

// normTo brings an angle difference into the half-turn window that runs in the
// same direction as the sweep, so a point at the far end of a major arc is not
// mistaken for one just before the start.
func normTo(d, sweep float64) float64 {
	if sweep >= 0 {
		for d < 0 {
			d += 2 * math.Pi
		}
		for d > 2*math.Pi {
			d -= 2 * math.Pi
		}
		return d
	}
	for d > 0 {
		d -= 2 * math.Pi
	}
	for d < -2*math.Pi {
		d += 2 * math.Pi
	}
	return d
}

// islands groups pads into copper-connected sets.
func islands(ids []string, paths map[string]Path) [][]string {
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if parent[x] == "" || parent[x] == x {
			parent[x] = x
			return x
		}
		r := find(parent[x])
		parent[x] = r
		return r
	}
	for _, id := range ids {
		parent[id] = id
	}
	for _, p := range paths {
		if p.Found {
			parent[find(p.From)] = find(p.To)
		}
	}
	groups := map[string][]string{}
	for _, id := range ids {
		r := find(id)
		groups[r] = append(groups[r], id)
	}
	out := make([][]string, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i][0] < out[j][0]
	})
	return out
}

// kicadLength reproduces pcbnew's own net length: the sum of all copper plus
// the barrels of vias that actually join two layers carrying net copper.
//
// The via rule is the part worth spelling out. Every via on this board is
// declared F.Cu to B.Cu, but pcbnew charges a barrel only where the net has
// copper on two or more of the layers the via spans -- a via left with copper
// on one side only, which is what a half-finished fly-by leg looks like,
// contributes nothing. That single rule is why pcbnew reports three barrels on
// the clock nets and two on every address net.
func (e *Engine) kicadLength(net string, segs, vias []*board.Track) float64 {
	st := e.b.Stackup
	total := 0.0
	for _, t := range segs {
		total += t.Length(st)
	}
	if !e.CountViaLength {
		return total
	}
	for _, v := range vias {
		layers := map[string]bool{}
		vd := geom.Disc(v.Start, v.Size/2)
		spans := map[string]bool{}
		for _, l := range v.Layers(st) {
			spans[l] = true
		}
		for _, t := range segs {
			if !spans[t.Layer] || layers[t.Layer] {
				continue
			}
			for _, sh := range t.Shape(e.b.MaxError) {
				if sh.Dist(vd) <= geom.Eps {
					layers[t.Layer] = true
					break
				}
			}
		}
		if len(layers) >= 2 {
			total += v.Length(st)
		}
	}
	return total
}

// MeasureAll measures every net matching the filter.
func (e *Engine) MeasureAll(match func(string) bool) map[string]*Measure {
	out := map[string]*Measure{}
	for _, net := range e.b.Nets() {
		if match != nil && !match(net) {
			continue
		}
		out[net] = e.Measure(net)
	}
	return out
}
