// Package route creates copper where a connection is missing.
//
// Everything else in this tool edits copper that exists. This writes new
// copper, which is a different kind of change and is opt-in for that reason:
// lengthening a trace cannot alter what a board does electrically, and adding
// one can.
//
// It exists for one situation in particular. DDR3 and DDR4 address, command,
// control and clock are fly-by -- through each device in turn and terminated at
// the end -- and a board missing a hop of that chain has no topology to match.
// Matching lengths on the hops that exist is then work on a bus that is not
// finished. So the hop has to be made before anything about it means anything.
//
// How it works, and where the honesty is:
//
//   - The search is on a grid, which is fast and approximate. A cell is judged
//     by whether a track centred on it would be clear, which says nothing about
//     the space between two cell centres.
//   - So the grid never decides. It proposes a path; the real clearance checker
//     accepts or rejects it against the actual geometry; a rejection is written
//     back into the grid as blocked cells and the search runs again.
//   - Nets that cannot be routed around what is already there cause the routes
//     in their way to be ripped up and laid again in a different order.
//
// That loop is what "iteratively find a better one" means here, and it is not a
// refinement: on a board with 634 tracks in the corridor, the first path is
// rarely the one that fits.
package route

import (
	"fmt"
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Options tune the router.
type Options struct {
	// Layers the router may use. Empty means the layers the board already
	// carries copper on, which is the conservative reading: an empty inner
	// layer is usually a plane somebody has not poured yet, and putting a bus
	// on it is a decision this tool is not entitled to make.
	Layers []string

	// Width is the track width; zero takes it from the net's class.
	Width float64

	// ViaDiameter and ViaDrill size the vias it places; zero takes them from
	// the net's class, and failing that from the vias already on the board.
	ViaDiameter, ViaDrill float64

	// ViaCost is what a layer change is worth avoiding, in millimetres of
	// detour. A via is a hole, a stub and a discontinuity; taking one to save
	// two millimetres is a bad trade.
	ViaCost float64

	// BendCost is what a change of direction is worth avoiding, in mm. Small:
	// it is there to stop a staircase of single cells, not to forbid corners.
	BendCost float64

	// Margin is how far outside the endpoints the search may wander, in mm.
	Margin float64

	// MaxRipUp is how many times a net may be torn up and laid again.
	MaxRipUp int

	// Rules are the board's own custom design rules. Without them the router
	// works to the net classes at their strictest, and on a board whose rules
	// relax the clearance inside a BGA courtyard that means it cannot escape a
	// ball at all.
	Rules *board.Rules
}

// DefaultOptions are conservative settings.
func DefaultOptions() Options {
	return Options{ViaCost: 6.0, BendCost: 0.15, Margin: 6.0, MaxRipUp: 4}
}

// Request is one connection to make.
type Request struct {
	Net string

	// From and To are the pads to join, as "U4.P3".
	From, To string
}

// Result is what happened to one request.
type Result struct {
	Net      string
	From, To string

	Routed   bool
	LengthMM float64
	Vias     int

	// Tracks is the copper laid, in order.
	Tracks []*board.Track

	// Attempts is how many paths were tried before this one was accepted, or
	// given up on. More than one means the geometry rejected what the grid
	// proposed.
	Attempts int

	// RippedUp is how many already-routed nets were torn up to make room.
	RippedUp int

	// Reason says why it did not route.
	Reason string
}

// Router lays new copper on a board.
type Router struct {
	b    *board.Board
	proj *board.Project
	chk  *drc.Checker
	opt  Options

	width      float64
	viaD, viaK float64
	layers     []string
	g          *grid

	// exemptions caches, per net, the copper a route on it may touch. Built
	// from every piece of copper on the net, and asked for constantly.
	exemptions map[string]map[string]bool
}

// New builds a router for a board. The requests are needed up front because
// the search space is sized from them.
func New(b *board.Board, proj *board.Project, reqs []Request, opt Options) (*Router, error) {
	if opt.MaxRipUp <= 0 {
		opt = DefaultOptions()
	}
	if len(reqs) == 0 {
		return nil, fmt.Errorf("route: nothing to route")
	}
	r := &Router{b: b, proj: proj, opt: opt, chk: drc.New(b, proj)}
	// true: the router takes the board's permission as written. Escaping a BGA
	// ball is exactly what a relaxed fanout clearance is for, and without it
	// the router cannot leave the part at all.
	r.chk.SetRules(opt.Rules, true)

	net := reqs[0].Net
	r.width = opt.Width
	if r.width <= 0 {
		r.width = proj.TrackWidthOf(net)
	}
	if r.width <= 0 {
		r.width = math.Max(proj.MinTrackWidth, 0.09)
	}
	r.viaD, r.viaK = opt.ViaDiameter, opt.ViaDrill
	if r.viaD <= 0 || r.viaK <= 0 {
		r.viaD, r.viaK = r.viaFromBoard(net)
	}

	r.layers = opt.Layers
	if len(r.layers) == 0 {
		r.layers = layersInUse(b)
	}
	if len(r.layers) == 0 {
		return nil, fmt.Errorf("route: the board has no copper layers to route on")
	}

	// The search space: everything the requests have to reach, plus room to go
	// around what is in the way.
	box, ok := r.extent(reqs)
	if !ok {
		return nil, fmt.Errorf("route: none of the pads to join are on the board")
	}
	box = box.Grow(opt.Margin)

	// The grid steps at the tightest clearance anywhere it may have to route,
	// not at the net class's. The board's own rules relax the clearance inside
	// a BGA courtyard, and a grid stepped at the class figure has no cell in
	// the channel between two balls -- so the router reports no way out of a
	// part that the board escapes perfectly well. Which is exactly what it did
	// on the demo board: 50 of 52 connections refused as blocked.
	//
	// The grid being finer does not make anything looser. Every path is still
	// checked against the real rules in the shape the copper will have, and
	// those say 0.2 mm everywhere the relaxation does not apply.
	pitch := r.width + r.clearance(net)
	if t := opt.Rules.TightestClearance(); t > 0 && t < r.clearance(net) {
		pitch = r.width + t
	}
	nx := int(math.Ceil((box.MaxX-box.MinX)/pitch)) + 1
	ny := int(math.Ceil((box.MaxY-box.MinY)/pitch)) + 1
	r.g = newGrid(geom.Pt{X: box.MinX, Y: box.MinY}, pitch, nx, ny, r.layers)
	r.g.rasterise(r.chk, net, r.width)
	return r, nil
}

// Cells reports the size of the search space, for the report.
func (r *Router) Cells() (nx, ny, layers int) { return r.g.nx, r.g.ny, len(r.g.layers) }

// Pitch is the grid step in millimetres.
func (r *Router) Pitch() float64 { return r.g.pitch }

func (r *Router) clearance(net string) float64 {
	if c := r.proj.ClearanceOf(net); c > 0 {
		return c
	}
	return 0.2
}

// viaFromBoard copies the via size the board already uses on this net's class,
// so new vias match the ones a fabricator was already quoted for.
func (r *Router) viaFromBoard(net string) (float64, float64) {
	if c := r.proj.ClassOf(net); c != nil && c.ViaDiameter > 0 && c.ViaDrill > 0 {
		return c.ViaDiameter, c.ViaDrill
	}
	counts := map[[2]float64]int{}
	for _, t := range r.b.Tracks {
		if t.Kind == board.KindVia && t.Size > 0 && t.Drill > 0 {
			counts[[2]float64{t.Size, t.Drill}]++
		}
	}
	best, n := [2]float64{0.6, 0.3}, 0
	for k, c := range counts {
		if c > n {
			best, n = k, c
		}
	}
	return best[0], best[1]
}

// layersInUse are the copper layers the board already routes on, in stack
// order.
func layersInUse(b *board.Board) []string {
	used := map[string]bool{}
	for _, t := range b.Tracks {
		if t.Kind != board.KindVia && t.Layer != "" {
			used[t.Layer] = true
		}
	}
	var out []string
	for _, l := range b.CopperLayers {
		if used[l] {
			out = append(out, l)
		}
	}
	return out
}

func (r *Router) extent(reqs []Request) (geom.Rect, bool) {
	var box geom.Rect
	first := true
	for _, q := range reqs {
		for _, id := range []string{q.From, q.To} {
			p := r.pad(q.Net, id)
			if p == nil {
				continue
			}
			if first {
				box, first = geom.Rect{MinX: p.Centre.X, MinY: p.Centre.Y, MaxX: p.Centre.X, MaxY: p.Centre.Y}, false
				continue
			}
			box = box.Include(p.Centre)
		}
	}
	return box, !first
}

func (r *Router) pad(net, id string) *board.Pad {
	for _, p := range r.b.PadsOfNet(net) {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

// Route makes the connections, hardest first, ripping up and laying again where
// a net cannot get through.
//
// Hardest first is not arbitrary: a bus routed easiest-first fills the corridor
// with the routes that had choices and leaves the one that did not with none.
func (r *Router) Route(reqs []Request) ([]*Result, error) {
	order := append([]Request{}, reqs...)
	sort.SliceStable(order, func(i, j int) bool {
		return r.straight(order[i]) > r.straight(order[j])
	})

	results := map[string]*Result{}
	var out []*Result
	for _, q := range order {
		res := &Result{Net: q.Net, From: q.From, To: q.To}
		results[key(q)] = res
		out = append(out, res)
	}

	queue := order
	ripped := map[string]int{}
	for round := 0; len(queue) > 0 && round <= r.opt.MaxRipUp; round++ {
		var failed []Request
		for _, q := range queue {
			res := results[key(q)]
			if err := r.one(q, res); err != nil {
				return out, err
			}
			if !res.Routed {
				failed = append(failed, q)
			}
		}
		if len(failed) == 0 {
			break
		}
		// Rip up what is in the way of the failures and lay it again after
		// them, so the net with no choices gets the corridor first.
		var again []Request
		for _, q := range failed {
			if ripped[key(q)] >= r.opt.MaxRipUp {
				continue
			}
			ripped[key(q)]++
			for _, v := range r.inTheWay(q, order, results) {
				r.ripUp(results[key(v)])
				again = append(again, v)
			}
		}
		if len(again) == 0 {
			break // nothing left to move out of the way
		}
		for _, q := range failed {
			results[key(q)].RippedUp += len(again)
		}
		queue = append(append([]Request{}, failed...), again...)
	}
	return out, nil
}

func key(q Request) string { return q.Net + "|" + q.From + "|" + q.To }

// straight is the direct distance between a request's pads, which stands in for
// how hard it will be.
func (r *Router) straight(q Request) float64 {
	a, b := r.pad(q.Net, q.From), r.pad(q.Net, q.To)
	if a == nil || b == nil {
		return 0
	}
	return a.Centre.Dist(b.Centre)
}

// inTheWay picks the routed nets whose copper sits between a failed request's
// pads, as the candidates to move.
func (r *Router) inTheWay(q Request, all []Request, results map[string]*Result) []Request {
	a, b := r.pad(q.Net, q.From), r.pad(q.Net, q.To)
	if a == nil || b == nil {
		return nil
	}
	corridor := geom.Rect{
		MinX: math.Min(a.Centre.X, b.Centre.X), MinY: math.Min(a.Centre.Y, b.Centre.Y),
		MaxX: math.Max(a.Centre.X, b.Centre.X), MaxY: math.Max(a.Centre.Y, b.Centre.Y),
	}.Grow(r.g.pitch * 2)
	var out []Request
	for _, v := range all {
		if key(v) == key(q) {
			continue
		}
		res := results[key(v)]
		if res == nil || !res.Routed {
			continue
		}
		for _, t := range res.Tracks {
			if corridor.Contains(t.Start) || corridor.Contains(t.End) {
				out = append(out, v)
				break
			}
		}
	}
	return out
}

// ripUp takes a route off the board and gives its space back.
func (r *Router) ripUp(res *Result) {
	if res == nil || !res.Routed {
		return
	}
	for _, t := range res.Tracks {
		r.chk.RemoveRef(t.UUID)
	}
	delete(r.exemptions, res.Net)
	r.b.RemoveTracks(res.Tracks)
	r.g.release(res.Net)
	res.Routed, res.Tracks, res.LengthMM, res.Vias = false, nil, 0, 0
}
