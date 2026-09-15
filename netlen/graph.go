package netlen

import (
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// SnapFloor is the smallest junction tolerance used, to absorb rounding in a
// hand-edited file even on hairline tracks. Above it, two ends count as the
// same junction when their copper actually overlaps -- that is, when they are
// within the sum of their half-widths.
//
// Copper overlap rather than a fixed tolerance is the right rule because it is
// the same rule the board obeys electrically. pcbnew's router does not always
// land two tracks on the same coordinate: corners on this board are out by up
// to 30 micrometres, and one meander turn leaves a 66 micrometre gap between
// two arcs with no segment between them at all. All of it is solid copper,
// because the tracks are 90 micrometres wide. Matching coordinates exactly
// finds such a net in a dozen pieces; a fixed 50 micrometre tolerance still
// misses that meander turn.
const SnapFloor = 0.01

// SnapTolerance is the junction tolerance between two terminals.
func snapTolerance(a, b terminal) float64 {
	if d := a.half + b.half; d > SnapFloor {
		return d
	}
	return SnapFloor
}

// terminal is one end of one piece of copper, or a pad anchor: the places where
// copper can join copper.
type terminal struct {
	p     geom.Pt
	layer string

	// owner identifies the item this terminal belongs to. Two terminals of the
	// same track must never be merged into one node -- that would let a route
	// skip the track entirely.
	owner int

	// half is the owning item's half-width: half the track width, or the via's
	// pad radius. Two pieces of copper touch when their centrelines are within
	// the sum of their halves.
	half float64
}

// graph is a net's copper as a weighted graph.
//
// The invariant that makes the measurement correct is that every track
// contributes its whole centreline length, split only at genuine junctions.
// An earlier version joined nearby copper with zero-length bridges between
// separate nodes, which let a route cut the corner where two tracks meet
// through a short connector segment -- costing half a millimetre on a net with
// a dozen tuning corners, and silently under-reporting exactly the nets that
// had already been tuned. Junctions are therefore merged into single nodes
// rather than bridged.
type graph struct {
	adj []([]edge)
	pts []geom.Pt
}

type edge struct {
	to     int
	length float64
	delay  float64
	via    bool

	// track is the copper this edge is a piece of, so a caller can be told
	// which tracks a route actually runs over. That matters for tuning: a net
	// whose copper is in more than one island has tracks that are on no route
	// at all, and lengthening one of those adds copper without shortening any
	// timing path.
	track *board.Track
}

func (g *graph) node(p geom.Pt) int {
	id := len(g.adj)
	g.adj = append(g.adj, nil)
	g.pts = append(g.pts, p)
	return id
}

func (g *graph) link(a, b int, length, delay float64, via bool) {
	g.linkTrack(a, b, length, delay, via, nil)
}

func (g *graph) linkTrack(a, b int, length, delay float64, via bool, tr *board.Track) {
	if a == b || a < 0 || b < 0 {
		return
	}
	g.adj[a] = append(g.adj[a], edge{to: b, length: length, delay: delay, via: via, track: tr})
	g.adj[b] = append(g.adj[b], edge{to: a, length: length, delay: delay, via: via, track: tr})
}

// union is a plain union-find, used to track which pieces of a net are already
// connected while deciding whether a track needs dividing.
type union struct{ parent []int }

func newUnion(n int) *union {
	u := &union{parent: make([]int, n)}
	for i := range u.parent {
		u.parent[i] = i
	}
	return u
}

func (u *union) find(i int) int {
	for u.parent[i] != i {
		u.parent[i] = u.parent[u.parent[i]]
		i = u.parent[i]
	}
	return i
}

func (u *union) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra != rb {
		u.parent[ra] = rb
	}
}

// clusters merges terminals that sit at the same junction.
//
// Candidate merges are taken in order of increasing distance, so the closest
// pair wins when several are in reach. A merge is refused when it would place
// both ends of one piece of copper in the same cluster: that is what keeps a
// two-micrometre connector segment at a tuning corner from collapsing and
// letting the route bypass it.
type clusters struct {
	parent []int
	// owners[root] is the set of item indices with a terminal in that cluster,
	// used to enforce the refusal above.
	owners []map[int]bool
}

func newClusters(t []terminal) *clusters {
	c := &clusters{parent: make([]int, len(t)), owners: make([]map[int]bool, len(t))}
	for i := range t {
		c.parent[i] = i
		c.owners[i] = map[int]bool{t[i].owner: true}
	}
	return c
}

func (c *clusters) find(i int) int {
	for c.parent[i] != i {
		c.parent[i] = c.parent[c.parent[i]]
		i = c.parent[i]
	}
	return i
}

// union merges two clusters, reporting whether it went ahead.
func (c *clusters) union(a, b int) bool {
	ra, rb := c.find(a), c.find(b)
	if ra == rb {
		return false
	}
	// Refuse if any item would end up with both of its ends in one cluster.
	small, large := ra, rb
	if len(c.owners[small]) > len(c.owners[large]) {
		small, large = large, small
	}
	for o := range c.owners[small] {
		if c.owners[large][o] {
			return false
		}
	}
	c.parent[small] = large
	for o := range c.owners[small] {
		c.owners[large][o] = true
	}
	c.owners[small] = nil
	return true
}

// buildGraph turns a net's copper into a graph, and returns the node id for
// each pad.
func (e *Engine) buildGraph(net string) (*graph, map[string]int) {
	b := e.b
	st := b.Stackup
	tracks := b.TracksOfNet(net)
	pads := b.PadsOfNet(net)

	var segs, vias []*board.Track
	for _, t := range tracks {
		if t.Kind == board.KindVia {
			vias = append(vias, t)
		} else {
			segs = append(segs, t)
		}
	}

	// Terminals: both ends of every track, and each via's centre on every
	// layer it spans. Vias get one owner per via so that the barrel's own ends
	// are never merged away.
	var terms []terminal
	owner := 0
	segOwner := make([]int, len(segs))
	for i, t := range segs {
		segOwner[i] = owner
		terms = append(terms,
			terminal{t.Start, t.Layer, owner, t.Width / 2},
			terminal{t.End, t.Layer, owner, t.Width / 2})
		owner++
	}
	viaTerm := make([][]int, len(vias))
	for i, v := range vias {
		for _, l := range v.Layers(st) {
			viaTerm[i] = append(viaTerm[i], len(terms))
			// A via's layer terminals may all merge together -- they are the
			// same point -- so they share no owner constraint with each other.
			terms = append(terms, terminal{v.Start, l, owner, v.Size / 2})
			owner++
		}
	}

	// Merge terminals that are the same junction.
	cl := newClusters(terms)
	type pair struct {
		i, j int
		d    float64
	}
	var cands []pair
	for i := range terms {
		for j := i + 1; j < len(terms); j++ {
			if terms[i].layer != terms[j].layer {
				continue
			}
			d := terms[i].p.Dist(terms[j].p)
			if d <= snapTolerance(terms[i], terms[j]) {
				cands = append(cands, pair{i, j, d})
			}
		}
	}
	sort.Slice(cands, func(a, c int) bool { return cands[a].d < cands[c].d })
	for _, p := range cands {
		cl.union(p.i, p.j)
	}

	g := &graph{}
	nodeOf := map[int]int{}
	for i := range terms {
		r := cl.find(i)
		if _, ok := nodeOf[r]; !ok {
			nodeOf[r] = g.node(terms[r].p)
		}
	}
	termNode := func(i int) int { return nodeOf[cl.find(i)] }

	// T-junctions.
	//
	// A terminal landing partway along another track can divide it. That is
	// essential on a branching net: a fly-by address line leaves the main run
	// somewhere in the middle, and without a division there the branch is
	// invisible.
	//
	// But most copper that lands partway along a track is not a branch. At a
	// corner where two tracks meet through a short connector, or at a spur the
	// router left by a pad, a terminal sits within a track's width of that
	// track's centreline without being a junction at all. Dividing there lets a
	// route leave the centreline and cross the overlap instead -- a genuine
	// copper shortcut, but not the length any tool, guide or reviewer means by
	// "track length". It cost up to 0.12 mm a net here and 0.52 mm before the
	// junctions were merged properly.
	//
	// So a division is made only when it is what joins two otherwise separate
	// pieces of the net. Candidates are considered closest-first, and each is
	// accepted only if the terminal and the track are not already connected
	// some other way.
	// splitPt records where a track is divided: a fraction along it, and the
	// graph node the division shares with whatever landed there.
	type splitPt struct {
		at   float64
		node int
	}
	comp := newUnion(len(cl.parent) + len(vias))
	viaComp := func(i int) int { return len(cl.parent) + i }
	for i := range segs {
		comp.union(cl.find(2*i), cl.find(2*i+1))
	}
	for i := range vias {
		for _, ti := range viaTerm[i] {
			comp.union(cl.find(ti), viaComp(i))
		}
	}

	type candidate struct {
		track int
		term  int
		at    float64
		off   float64
	}
	var cands2 []candidate
	for i, t := range segs {
		box := t.Box()
		for k := range terms {
			if terms[k].owner == segOwner[i] || terms[k].layer != t.Layer {
				continue
			}
			reach := t.Width/2 + terms[k].half
			if !box.Grow(reach).Contains(terms[k].p) {
				continue
			}
			_, at, d := nearestOn(t, terms[k].p)
			if d > reach || at <= 1e-9 || at >= 1-1e-9 {
				continue
			}
			cands2 = append(cands2, candidate{i, k, at, d})
		}
	}
	sort.Slice(cands2, func(a, c int) bool { return cands2[a].off < cands2[c].off })

	splits := make([][]splitPt, len(segs))
	if !e.NoTJunctionSplits {
		for _, c := range cands2 {
			a := comp.find(cl.find(c.term))
			b1 := comp.find(cl.find(2 * c.track))
			if a == b1 {
				continue
			}
			comp.union(a, b1)
			splits[c.track] = append(splits[c.track], splitPt{c.at, termNode(c.term)})
		}
	}

	// One edge per piece of every track.
	for i, t := range segs {
		perMM := st.DelayPerMM(t.Layer, t.Width)
		total := t.Length(st)
		pieces := append([]splitPt{{0, termNode(2 * i)}}, splits[i]...)
		pieces = append(pieces, splitPt{1, termNode(2*i + 1)})
		sort.Slice(pieces, func(a, c int) bool { return pieces[a].at < pieces[c].at })
		for k := 0; k+1 < len(pieces); k++ {
			l := total * (pieces[k+1].at - pieces[k].at)
			if l < 0 {
				l = 0
			}
			g.linkTrack(pieces[k].node, pieces[k+1].node, l, l*perMM, false, t)
		}
	}

	// Vias: a hub carrying half the barrel to each layer it reaches, so a
	// signal crossing the via pays the barrel exactly once.
	viaDelay := st.ViaDelayPerMM()
	for i, v := range vias {
		if len(viaTerm[i]) < 2 {
			continue
		}
		length := 0.0
		if e.CountViaLength {
			length = v.Length(st)
		}
		hub := g.node(v.Start)
		for _, ti := range viaTerm[i] {
			g.linkTrack(hub, termNode(ti), length/2, length/2*viaDelay, true, v)
		}
	}

	// Pads.
	//
	// A pad is a piece of copper, so a signal entering it can leave from
	// anywhere on it. Attaching the anchor to every overlapping terminal with
	// zero length would therefore let a route enter at one point inside the
	// pad and leave at another, skipping the track between them -- 0.21 mm on
	// DDR_DQS1_N, where the router laid two segments across the BGA pad.
	//
	// Charging each attachment the straight-line distance from the anchor to
	// that point removes the discrepancy: entering at the far terminal costs
	// exactly what walking the copper to it would, so no route is cheaper than
	// the centreline and none is dearer. It also makes the measurement
	// independent of where in the pad the router happened to stop, which is the
	// one thing about a pad connection that carries no design intent.
	padNode := map[string]int{}
	for _, p := range pads {
		pn := g.node(p.Centre)
		padNode[p.ID()] = pn
		die := 0.0
		if e.CountDieLength {
			die = p.DieLength
		}
		psPerMM := st.DelayPerMM(firstLayerOf(p, b.CopperLayers), 0.09)
		attached := false
		for k := range terms {
			if !p.OnLayer(terms[k].layer) {
				continue
			}
			if p.Shape.DistToPoint(terms[k].p) > terms[k].half {
				continue
			}
			d := p.Centre.Dist(terms[k].p) + die
			g.link(pn, termNode(k), d, d*psPerMM, false)
			attached = true
		}
		if attached {
			continue
		}
		// Nothing ends on the pad. A track may still run across it -- a fanout
		// routed through a BGA field does exactly that -- so split the nearest
		// such track at the point closest to the anchor and join there.
		best, bestAt, bestIdx := math.Inf(1), 0.0, -1
		for i, t := range segs {
			if !p.OnLayer(t.Layer) {
				continue
			}
			q, at, _ := nearestOn(t, p.Centre)
			if p.Shape.DistToPoint(q) > t.Width/2 {
				continue
			}
			if d := p.Centre.Dist(q); d < best {
				best, bestAt, bestIdx = d, at, i
			}
		}
		if bestIdx < 0 {
			continue
		}
		t := segs[bestIdx]
		q, _, _ := nearestOn(t, p.Centre)
		mid := g.node(q)
		total := t.Length(st)
		perMM := st.DelayPerMM(t.Layer, t.Width)
		a := total * bestAt
		g.linkTrack(termNode(2*bestIdx), mid, a, a*perMM, false, t)
		g.linkTrack(mid, termNode(2*bestIdx+1), total-a, (total-a)*perMM, false, t)
		d := best + die
		g.link(pn, mid, d, d*psPerMM, false)
	}
	return g, padNode
}

// firstLayerOf returns the topmost copper layer a pad is on, used only to pick
// a propagation rate for the pad's own connection.
func firstLayerOf(p *board.Pad, order []string) string {
	for _, l := range order {
		if p.OnLayer(l) {
			return l
		}
	}
	return "F.Cu"
}

// dijkstra returns the shortest length to every node from src, with the delay
// and the number of via half-barrels along that same route.
func (g *graph) dijkstra(src int) (dist, delay []float64, vias []int) {
	dist, delay, vias, _, _ = g.dijkstraFrom(src)
	return dist, delay, vias
}

// dijkstraFrom also returns, for each node, the node it was reached from and
// the track that edge belongs to, so a route can be walked back into the set
// of tracks it runs over.
func (g *graph) dijkstraFrom(src int) (dist, delay []float64, vias, prev []int, via []*board.Track) {
	n := len(g.adj)
	dist = make([]float64, n)
	delay = make([]float64, n)
	vias = make([]int, n)
	prev = make([]int, n)
	via = make([]*board.Track, n)
	for i := range dist {
		dist[i] = math.Inf(1)
		prev[i] = -1
	}
	dist[src] = 0
	h := &heapq{}
	h.push(item{src, 0})
	for h.Len() > 0 {
		it := h.pop()
		if it.d > dist[it.n]+1e-12 {
			continue
		}
		for _, ed := range g.adj[it.n] {
			nd := it.d + ed.length
			if nd < dist[ed.to]-1e-12 {
				dist[ed.to] = nd
				delay[ed.to] = delay[it.n] + ed.delay
				vias[ed.to] = vias[it.n]
				prev[ed.to] = it.n
				via[ed.to] = ed.track
				if ed.via {
					vias[ed.to]++
				}
				h.push(item{ed.to, nd})
			}
		}
	}
	return dist, delay, vias, prev, via
}

// tracksOnRoute walks the predecessor chain from dst back to the source and
// returns the uuids of the tracks the route runs over.
func tracksOnRoute(prev []int, via []*board.Track, dst int) []string {
	seen := map[string]bool{}
	for n := dst; n >= 0; n = prev[n] {
		if t := via[n]; t != nil && t.UUID != "" {
			seen[t.UUID] = true
		}
	}
	out := make([]string, 0, len(seen))
	for u := range seen {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

type item struct {
	n int
	d float64
}

type heapq struct{ a []item }

func (h *heapq) Len() int { return len(h.a) }

func (h *heapq) push(x item) {
	h.a = append(h.a, x)
	i := len(h.a) - 1
	for i > 0 {
		p := (i - 1) / 2
		if h.a[p].d <= h.a[i].d {
			break
		}
		h.a[p], h.a[i] = h.a[i], h.a[p]
		i = p
	}
}

func (h *heapq) pop() item {
	top := h.a[0]
	last := len(h.a) - 1
	h.a[0] = h.a[last]
	h.a = h.a[:last]
	i := 0
	for {
		l, r := 2*i+1, 2*i+2
		s := i
		if l < len(h.a) && h.a[l].d < h.a[s].d {
			s = l
		}
		if r < len(h.a) && h.a[r].d < h.a[s].d {
			s = r
		}
		if s == i {
			break
		}
		h.a[i], h.a[s] = h.a[s], h.a[i]
		i = s
	}
	return top
}
