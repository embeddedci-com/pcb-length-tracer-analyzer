package route

import (
	"fmt"
	"math"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// one makes a single connection: propose, verify, feed the failure back, try
// again.
//
// The verification is the part that matters. The grid says a cell is clear
// because a track centred on it would be; it says nothing about the space
// between two centres, and a diagonal move passes closer to its neighbours
// than any cell distance suggests. So the path the grid finds is checked as
// real copper against the real rules, and where it fails, the cells at the
// failure are blocked and the search is asked again. Nothing reaches the board
// that the clearance checker has not accepted in the shape it will actually
// have.
func (r *Router) one(q Request, res *Result) error {
	from, to := r.pad(q.Net, q.From), r.pad(q.Net, q.To)
	if from == nil || to == nil {
		res.Reason = "one of the pads to join is not on this net"
		return nil
	}
	if res.Routed {
		return nil
	}

	starts := r.accessCells(from, q.Net)
	goals := r.accessCells(to, q.Net)
	if len(starts) == 0 || len(goals) == 0 {
		res.Reason = fmt.Sprintf("no clear space beside %s to leave from",
			pick(len(starts) == 0, q.From, q.To))
		return nil
	}

	// Whether a via fits at a cell is asked for nearly every node the search
	// expands, and answering it means a clearance query on every copper layer
	// the barrel passes through -- six, on this board. Cached for the duration
	// of one route, which is as long as the answer is stable: the board does
	// not change until the route is committed.
	fits := map[[2]int]bool{}
	viaOK := func(c cell) bool {
		k := [2]int{c.x, c.y}
		ok, seen := fits[k]
		if !seen {
			ok = r.viaFits(c, q.Net)
			fits[k] = ok
		}
		return ok
	}
	const maxTries = 12
	for try := 0; try < maxTries; try++ {
		res.Attempts++
		path := r.g.search(starts, goals, q.Net, costs{via: r.opt.ViaCost, bend: r.opt.BendCost}, viaOK)
		if path == nil {
			res.Reason = "no way through: the corridor between these pads is full of copper this " +
				"router will not move. It rips up and re-lays its own routes to let another " +
				"through, and nothing else -- existing routing is somebody's work and moving it " +
				"is their decision"
			return nil
		}
		tracks := r.copper(path, q.Net, from, to)
		bad, why := r.reject(tracks, q.Net, from, to)
		if bad < 0 {
			r.commit(path, tracks, q.Net, res)
			return nil
		}
		// The geometry refused it. Block where it went wrong and look again --
		// which is the only way a grid this coarse can be trusted.
		r.blockAround(path, bad)
		res.Reason = why
	}
	res.Reason = fmt.Sprintf("gave up after %d paths, the last refused because %s", maxTries, res.Reason)
	return nil
}

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// accessCells are the cells a route may leave a pad from: the pad's own cell
// and its neighbours, on the layers the pad is on.
func (r *Router) accessCells(p *board.Pad, net string) []cell {
	on := map[string]bool{}
	for _, l := range p.Layers {
		if l == "*.Cu" {
			for _, c := range r.b.CopperLayers {
				on[c] = true
			}
			continue
		}
		on[l] = true
	}
	var out []cell
	for li, l := range r.g.layers {
		if !on[l] {
			continue
		}
		c := r.g.nearest(p.Centre, li)
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				n := cell{c.x + dx, c.y + dy, li}
				if r.g.usableBy(n, net) {
					out = append(out, n)
				}
			}
		}
	}
	return out
}

// viaFits reports whether a via may be placed at a cell: its barrel has to
// clear everything on every layer it passes through.
func (r *Router) viaFits(c cell, net string) bool {
	at := r.g.pt(c)
	for _, l := range r.b.CopperLayers {
		if !r.chk.Clear(drc.Candidate{
			Shapes: []geom.RoundPoly{geom.Disc(at, r.viaD/2)},
			Layer:  l, Net: net, Exempt: r.exempt(net),
		}) {
			return false
		}
	}
	return true
}

// exempt lists the copper a new route is allowed to touch: the net's own, which
// is what it is being joined to.
//
// Cached per net. It is asked for once per via test and once per track of a
// proposed route, and building it walks every piece of copper on the net.
func (r *Router) exempt(net string) map[string]bool {
	if r.exemptions == nil {
		r.exemptions = map[string]map[string]bool{}
	}
	if out, ok := r.exemptions[net]; ok {
		return out
	}
	out := r.buildExempt(net)
	r.exemptions[net] = out
	return out
}

func (r *Router) buildExempt(net string) map[string]bool {
	out := map[string]bool{}
	for _, t := range r.b.TracksOfNet(net) {
		if t.UUID != "" {
			out[t.UUID] = true
		}
	}
	for _, p := range r.b.PadsOfNet(net) {
		out[p.ID()] = true
	}
	return out
}

// copper turns a path of cells into the tracks and vias it stands for: one
// segment per straight run, a via wherever the layer changes, and a stub at
// each end joining the pad centre.
func (r *Router) copper(path []cell, net string, from, to *board.Pad) []*board.Track {
	var out []*board.Track
	seg := func(a, b geom.Pt, layer string) {
		if a.Dist(b) < 1e-9 {
			return
		}
		out = append(out, &board.Track{
			Kind: board.KindSegment, Net: net, Start: a.Snap(), End: b.Snap(),
			Width: r.width, Layer: r.g.layers[indexOf(r.g.layers, layer)], UUID: newUUID(),
		})
	}

	// Straight runs: walk the path, and keep extending a run while the step
	// from one cell to the next is the same step as before. One segment per
	// run, a via wherever the layer changes.
	for i := 0; i+1 < len(path); {
		if path[i+1].layer != path[i].layer {
			at := r.g.pt(path[i])
			out = append(out, &board.Track{
				Kind: board.KindVia, Net: net, Start: at.Snap(), End: at.Snap(),
				Size: r.viaD, Drill: r.viaK,
				LayerTop:    r.b.CopperLayers[0],
				LayerBottom: r.b.CopperLayers[len(r.b.CopperLayers)-1],
				UUID:        newUUID(),
			})
			i++
			continue
		}
		dx, dy := path[i+1].x-path[i].x, path[i+1].y-path[i].y
		k := i + 1
		for k+1 < len(path) && path[k+1].layer == path[i].layer &&
			path[k+1].x-path[k].x == dx && path[k+1].y-path[k].y == dy {
			k++
		}
		seg(r.g.pt(path[i]), r.g.pt(path[k]), r.g.layers[path[i].layer])
		i = k
	}

	// Join the pad centres, so the route lands on the pad rather than beside it.
	if len(path) > 0 {
		seg(from.Centre, r.g.pt(path[0]), r.g.layers[path[0].layer])
		last := path[len(path)-1]
		seg(r.g.pt(last), to.Centre, r.g.layers[last.layer])
	}
	return out
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return 0
}

// reject checks the proposed copper as the real thing. It returns the index of
// the first track the rules refuse, or -1 when the whole route is clean.
func (r *Router) reject(tracks []*board.Track, net string, from, to *board.Pad) (int, string) {
	exempt := r.exempt(net)
	for i, t := range tracks {
		layers := []string{t.Layer}
		if t.Kind == board.KindVia {
			layers = r.b.CopperLayers
		}
		for _, l := range layers {
			vs := r.chk.Check(drc.Candidate{
				Shapes: t.Shape(r.b.MaxError), Layer: l, Net: net, Exempt: exempt,
			})
			if len(vs) > 0 {
				return i, fmt.Sprintf("it would sit %.3f mm from %s on %s, where %.3f mm is required",
					vs[0].Actual, describe(vs[0]), l, vs[0].Required)
			}
		}
	}
	return -1, ""
}

func describe(v drc.Violation) string {
	if v.Net != "" {
		return v.Net
	}
	if v.Kind != "" {
		return "the " + v.Kind
	}
	return "something"
}

// blockAround marks the cells at a refused track, so the next search goes
// elsewhere. The whole neighbourhood goes, not the one cell: a route rejected
// for being 0.02 mm too close needs to move further than one cell to fix it.
func (r *Router) blockAround(path []cell, at int) {
	// The track index is not a cell index, so block a span around the
	// proportional position rather than pretending to a precision we have not
	// got.
	i := clampInt(at*len(path)/maxInt(1, at+1), 0, len(path)-1)
	for d := -2; d <= 2; d++ {
		j := i + d
		if j < 0 || j >= len(path) {
			continue
		}
		c := path[j]
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				r.g.block(cell{c.x + dx, c.y + dy, c.layer})
			}
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// commit puts the route on the board and takes its space out of the grid.
func (r *Router) commit(path []cell, tracks []*board.Track, net string, res *Result) {
	for _, t := range tracks {
		r.b.AddTrack(t)
		if t.Kind == board.KindVia {
			res.Vias++
			continue
		}
		res.LengthMM += t.Length(r.b.Stackup)
	}
	for _, c := range path {
		r.g.claim(c, net)
		// A track is wider than a line: the cells either side of it are no
		// longer usable by anybody else.
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				r.g.claim(cell{c.x + dx, c.y + dy, c.layer}, net)
			}
		}
	}
	res.Tracks = tracks
	res.Routed = true
	res.Reason = ""
	// The new copper is an obstacle for the next net. Indexing it directly
	// rather than rebuilding the checker matters here more than anywhere else
	// in the tool: this happens once per route and again per rip-up, and a
	// rebuild walks every track, pad and zone polygon on the board.
	for _, t := range tracks {
		r.chk.AddTrack(t)
	}
	delete(r.exemptions, net)
}

// newUUID makes an identifier for new copper. Only the shape matters to KiCad;
// what matters here is that it is unique within the file.
var uuidCounter int

func newUUID() string {
	uuidCounter++
	return fmt.Sprintf("%08x-1111-4000-8000-%012x", uuidCounter, uuidCounter)
}

var _ = math.Abs
