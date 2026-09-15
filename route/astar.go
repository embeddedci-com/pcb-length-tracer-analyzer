package route

import (
	"container/heap"
	"math"
)

// Finding a path.
//
// A* on the grid, eight ways within a layer and straight up or down between
// them. Three costs shape what comes out, and they are the difference between a
// path and a route somebody would accept:
//
//   - length, in millimetres, so a diagonal costs what a diagonal is;
//   - a via, which is expensive -- it is a hole in the board, a stub, and a
//     discontinuity, and a route that takes one to save two millimetres is a
//     bad trade;
//   - a bend, mildly, because a staircase of single cells is the same length as
//     a straight run and nobody wants it on their board.
//
// The bend cost puts the direction of travel in the search state. That makes
// the state space eight times larger and is worth it: without it the router
// emits paths that are technically minimal and visibly wrong.

// dir is a direction of travel, indexed 0..7 clockwise from east, or none.
type dir int

const dirNone dir = -1

var steps = [8][2]int{
	{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1},
}

type node struct {
	c cell
	d dir
}

type queued struct {
	n     node
	f     float64
	index int
}

type queue []*queued

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].f < q[j].f }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i]; q[i].index = i; q[j].index = j }
func (q *queue) Push(x any)        { it := x.(*queued); it.index = len(*q); *q = append(*q, it) }
func (q *queue) Pop() any          { old := *q; n := len(old); it := old[n-1]; *q = old[:n-1]; return it }

// costs are what the search trades off, all in millimetres.
type costs struct {
	via  float64
	bend float64
}

// search finds the cheapest path from any of the starts to any of the goals,
// through cells the net may use. It returns the cells in order, or nil.
func (g *grid) search(starts, goals []cell, net string, c costs, viaOK func(cell) bool) []cell {
	goal := map[cell]bool{}
	for _, gc := range goals {
		goal[gc] = true
	}
	// The heuristic ignores layers and bends, so it never overestimates.
	h := func(x cell) float64 {
		best := math.Inf(1)
		for _, gc := range goals {
			dx := math.Abs(float64(x.x - gc.x))
			dy := math.Abs(float64(x.y - gc.y))
			// Octile distance: diagonals are cheaper per cell crossed.
			d := (dx + dy) + (math.Sqrt2-2)*math.Min(dx, dy)
			if d < best {
				best = d
			}
		}
		return best * g.pitch
	}

	dist := map[node]float64{}
	prev := map[node]node{}
	open := &queue{}
	heap.Init(open)
	for _, s := range starts {
		if !g.usableBy(s, net) {
			continue
		}
		n := node{s, dirNone}
		dist[n] = 0
		heap.Push(open, &queued{n: n, f: h(s)})
	}

	for pops := 0; open.Len() > 0; pops++ {
		if g.stop != nil && pops&1023 == 0 {
			select {
			case <-g.stop:
				return nil
			default:
			}
		}
		cur := heap.Pop(open).(*queued)
		if d, ok := dist[cur.n]; !ok || cur.f-h(cur.n.c) > d+1e-9 {
			continue // a stale entry
		}
		if goal[cur.n.c] {
			return unwind(prev, cur.n)
		}
		base := dist[cur.n]

		// Sideways, eight ways.
		for i, s := range steps {
			nd := dir(i)
			next := cell{cur.n.c.x + s[0], cur.n.c.y + s[1], cur.n.c.layer}
			if !g.usableBy(next, net) {
				continue
			}
			step := g.pitch
			if s[0] != 0 && s[1] != 0 {
				// A diagonal passes close to the two cells it cuts between, so
				// both have to be free as well. Without this the copper drawn
				// is nearer its neighbours than the grid ever said.
				if !g.usableBy(cell{cur.n.c.x + s[0], cur.n.c.y, cur.n.c.layer}, net) ||
					!g.usableBy(cell{cur.n.c.x, cur.n.c.y + s[1], cur.n.c.layer}, net) {
					continue
				}
				step = g.pitch * math.Sqrt2
			}
			cost := base + step
			if cur.n.d != dirNone && cur.n.d != nd {
				cost += c.bend
			}
			relax(open, dist, prev, cur.n, node{next, nd}, cost, h(next))
		}

		// Up or down, if a via fits here.
		if viaOK != nil && viaOK(cur.n.c) {
			for _, dl := range []int{-1, 1} {
				next := cell{cur.n.c.x, cur.n.c.y, cur.n.c.layer + dl}
				if !g.usableBy(next, net) {
					continue
				}
				relax(open, dist, prev, cur.n, node{next, dirNone}, base+c.via, h(next))
			}
		}
	}
	return nil
}

func relax(open *queue, dist map[node]float64, prev map[node]node, from, to node, cost, h float64) {
	if d, ok := dist[to]; ok && d <= cost+1e-9 {
		return
	}
	dist[to] = cost
	prev[to] = from
	heap.Push(open, &queued{n: to, f: cost + h})
}

func unwind(prev map[node]node, at node) []cell {
	var out []cell
	seen := map[node]bool{}
	for {
		if seen[at] {
			return nil // cannot happen, but a cycle must not hang the router
		}
		seen[at] = true
		out = append(out, at.c)
		p, ok := prev[at]
		if !ok {
			break
		}
		at = p
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
