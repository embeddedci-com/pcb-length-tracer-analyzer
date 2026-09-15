package route

import (
	"math"

	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// The search space.
//
// Routing is done on a grid, and the grid is an approximation: a cell is judged
// by whether a track centred on it would be clear, which says nothing about the
// space between two cell centres. A gridded router that trusted that would
// produce copper at 0.2 mm where it believed 0.29, and on a diagonal it would
// be worse -- a 45 degree move between two cells passes within 0.205 of the two
// cells beside it.
//
// So the grid is never the authority here. It proposes a path quickly; the real
// clearance checker accepts or rejects it against the actual geometry; and a
// rejection is fed back into the grid as blocked cells and searched again. That
// loop is what makes the approximation safe, and it is also the only honest way
// to route a board this dense: the first path is rarely the one that fits.

// cell is one position in the grid.
type cell struct {
	x, y, layer int
}

// grid holds what is where, per layer, at one pitch.
type grid struct {
	origin geom.Pt
	pitch  float64
	nx, ny int
	layers []string

	// owner is who occupies each cell: "" for clear, a net name when
	// everything too close belongs to that one net, and blocked when it is
	// something a route cannot share with.
	owner []string

	// stop ends a search early when closed; nil never does.
	stop <-chan struct{}
}

// blocked marks a cell nothing may route through.
const blocked = "\x00blocked"

func newGrid(origin geom.Pt, pitch float64, nx, ny int, layers []string) *grid {
	return &grid{
		origin: origin, pitch: pitch, nx: nx, ny: ny, layers: layers,
		owner: make([]string, nx*ny*len(layers)),
	}
}

func (g *grid) index(c cell) int { return (c.layer*g.ny+c.y)*g.nx + c.x }

func (g *grid) inside(c cell) bool {
	return c.x >= 0 && c.y >= 0 && c.layer >= 0 &&
		c.x < g.nx && c.y < g.ny && c.layer < len(g.layers)
}

// pt is the board position of a cell's centre.
func (g *grid) pt(c cell) geom.Pt {
	return geom.Pt{
		X: g.origin.X + float64(c.x)*g.pitch,
		Y: g.origin.Y + float64(c.y)*g.pitch,
	}
}

// nearest is the cell whose centre is closest to a point.
func (g *grid) nearest(p geom.Pt, layer int) cell {
	return cell{
		x:     clampInt(int(math.Round((p.X-g.origin.X)/g.pitch)), 0, g.nx-1),
		y:     clampInt(int(math.Round((p.Y-g.origin.Y)/g.pitch)), 0, g.ny-1),
		layer: layer,
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// usableBy reports whether a net may occupy a cell: it is clear, or everything
// too close to it already belongs to that net.
func (g *grid) usableBy(c cell, net string) bool {
	if !g.inside(c) {
		return false
	}
	o := g.owner[g.index(c)]
	return o == "" || o == net
}

// rasterise fills in the occupancy by asking the clearance checker what a track
// centred on each cell would clash with.
//
// A cell whose every clash is with one net is recorded as that net's, not as
// blocked: a net's own pads and the copper already on it are what a route has
// to start and end at, and a grid that called them obstacles could never leave
// a pad.
func (g *grid) rasterise(chk *drc.Checker, probeNet string, width float64) {
	for l := range g.layers {
		for y := 0; y < g.ny; y++ {
			for x := 0; x < g.nx; x++ {
				c := cell{x, y, l}
				vs := chk.Check(drc.Candidate{
					Shapes: []geom.RoundPoly{geom.Disc(g.pt(c), width/2)},
					Layer:  g.layers[l],
					Net:    probeNet,
				})
				if len(vs) == 0 {
					continue
				}
				owner := vs[0].Net
				for _, v := range vs {
					if v.Net != owner || v.Net == "" {
						owner = blocked
						break
					}
				}
				g.owner[g.index(c)] = owner
			}
		}
	}
}

// block marks a cell unusable by anyone, which is how a path the geometry
// rejected is kept from being found again.
func (g *grid) block(c cell) {
	if g.inside(c) {
		g.owner[g.index(c)] = blocked
	}
}

// claim records copper newly laid on a net, so the next net routes around it.
func (g *grid) claim(c cell, net string) {
	if !g.inside(c) {
		return
	}
	i := g.index(c)
	if g.owner[i] == "" || g.owner[i] == net {
		g.owner[i] = net
	}
}

// release gives a net's cells back, for a rip-up.
func (g *grid) release(net string) {
	for i, o := range g.owner {
		if o == net {
			g.owner[i] = ""
		}
	}
}
