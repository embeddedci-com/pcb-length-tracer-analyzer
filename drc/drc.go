// Package drc checks whether copper may go somewhere.
//
// This is not a reimplementation of KiCad's design rule engine and does not
// try to be. It answers one question quickly and conservatively: if a meander
// were drawn here, would it clash with anything? Everything on the board is
// indexed once, and each candidate is tested against only the copper near it,
// so the tuner can try thousands of shapes.
//
// Conservative means the check never relaxes a clearance. Net class clearances
// are taken at their strictest, a board with custom rules is flagged rather
// than interpreted, and the same-net gap a meander needs is treated as a hard
// requirement even though KiCad would not call it a violation. A candidate this
// package rejects might in fact have fitted; a candidate it accepts is
// overwhelmingly likely to pass KiCad, and the flow ends by running KiCad's own
// DRC to be sure.
package drc

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// obstacle is one piece of copper or board geometry a candidate must clear.
type obstacle struct {
	shape geom.RoundPoly
	layer string
	net   string

	// kind describes the obstacle for reporting.
	kind string

	// ref names the item, e.g. a pad id.
	ref string

	// edge marks board outline geometry, which every net must clear by the
	// copper-to-edge distance rather than a net clearance.
	edge bool

	// dead marks copper that has been taken off the board. The index holds
	// positions in the obstacle list, so removing an entry would invalidate
	// every index after it; marking is what lets a checker follow a board
	// being edited instead of being rebuilt for each edit.
	dead bool
}

// Violation is one clash found.
type Violation struct {
	// Kind is what was hit: track, via, pad, zone or board-edge.
	Kind string

	// Ref names the obstacle where one is nameable.
	Ref string

	// Net is the obstacle's net, empty for board geometry.
	Net string

	Layer string

	// Required is the clearance that applied; Actual is what the candidate
	// would leave.
	Required, Actual float64

	// At is roughly where the clash is.
	At geom.Pt
}

// OpenSpacing is a clearance that applies away from the components and nowhere
// else.
//
// A BGA is the hardest part of a board to route, so the clearance written on
// the netlist is usually the one that makes the escape possible -- 0.1 mm is
// common under a fine-pitch part -- and every other rule on the board inherits
// it. Between the components there is normally far more room than that, and the
// signals are better off using it: wider spacing means less coupling, and for
// this tool it is also what decides whether a meander fits beside a trace.
//
// So this is a floor that applies in the open. Around a component -- across the
// area its own pads occupy, plus a margin for the escape -- the board's own
// rules stand, because that is where the tight figure was chosen deliberately
// and nothing here knows better.
type OpenSpacing struct {
	// Clearance is the floor between two different nets' copper in the open.
	// Zero leaves the board's own rules in force everywhere.
	Clearance float64

	// Margin is how far outside a component's own pads still counts as being
	// around it.
	Margin float64

	// Coupled maps a net to the net it forms a differential pair with.
	//
	// A pair is exempt in both directions. Its two halves are meant to run
	// close together, and the gap they run at was chosen for the impedance
	// they were drawn to, so widening it is not a favour.
	Coupled map[string]string
}

// Checker answers clearance questions about one board.
type Checker struct {
	b    *board.Board
	proj *board.Project

	// SameNetGap is the minimum gap between two pieces of the same net's
	// copper that are not meant to touch.
	//
	// KiCad raises no violation when a net shorts to itself, so nothing stops a
	// meander folded too tightly from welding itself into a shorter track. That
	// would defeat the whole exercise, and tightly coupled parallel runs of one
	// signal also degrade it. So the tuner treats this as a real limit. The
	// usual guidance for a serpentine is to keep the legs at least three track
	// widths apart, which is what the default works out to.
	SameNetGap float64

	// index maps a coarse grid cell to the obstacles overlapping it.
	index map[cell][]int
	obs   []obstacle
	step  float64

	// open is the away-from-the-components clearance, and comp/compIndex are
	// the areas where it does not apply.
	open      OpenSpacing
	comp      []geom.Rect
	compIndex map[cell][]int

	// rules are the board's own custom design rules, where they could be read,
	// and relax says whether they may take a clearance below the net class.
	rules *board.Rules
	relax bool

	// zones are regions the user has asked for a wider spacing in.
	zones []ClearanceZone
}

// ClearanceZone is a region where the user wants more space between nets than
// the board demands.
//
// It only ever tightens. The region is somewhere they have chosen to let the
// tool put copper, and asking for more room between traces there is a choice
// they are entitled to make; letting it undercut a clearance the board requires
// would be something else, and is not what this does.
type ClearanceZone struct {
	Area         geom.Rect
	MinClearance float64
}

// SetClearanceZones asks for a wider spacing inside particular regions.
func (c *Checker) SetClearanceZones(z []ClearanceZone) { c.zones = z }

// zoneClearance is the widest spacing any zone demands of two pieces of copper,
// both of which have to be inside it.
func (c *Checker) zoneClearance(a, b geom.Rect) float64 {
	out := 0.0
	for _, z := range c.zones {
		if z.MinClearance <= out {
			continue
		}
		if z.Area.Overlaps(a) && z.Area.Overlaps(b) {
			out = z.MinClearance
		}
	}
	return out
}

// SetRules applies the board's custom design rules.
//
// relax decides whether a rule may take the clearance *below* what the net
// class asks, and the answer depends on what the caller is doing.
//
// A rule that relaxes a clearance exists because something would otherwise be
// impossible. On the demo board it is the BGA fanout: a ball cannot be escaped
// at the class's 0.2 mm, because the channel between two balls is not wide
// enough, so the board says 0.1 mm is allowed inside the courtyard. A router
// needs that permission or it cannot leave the part.
//
// A tuner does not. Nothing forces a meander into a BGA fanout -- it is the
// worst place on the board for one, dense and awkward and nobody's choice --
// and taking the permission there only means cramming copper in at 0.1 mm to
// win a millimetre that could be had somewhere else. So the tuner passes false
// and the rules may only tighten, which keeps it doing what it has always done:
// being stricter than the board, and declining rather than allowing.
//
// The rest of the safety is in board.Rules: a rule applies only when its
// condition is fully understood, only when it holds for both pieces of copper,
// and only when they are wholly inside the area the condition names.
func (c *Checker) SetRules(r *board.Rules, relax bool) {
	c.rules, c.relax = r, relax
}

// SetOpenSpacing applies an open-field clearance to every question this checker
// answers from here on. A zero clearance clears it again.
func (c *Checker) SetOpenSpacing(o OpenSpacing) {
	c.open, c.comp, c.compIndex = o, nil, nil
	if o.Clearance <= 0 {
		return
	}
	c.compIndex = map[cell][]int{}
	for _, f := range c.b.Footprints {
		var r geom.Rect
		first := true
		for _, p := range f.Pads {
			if first {
				r, first = p.Shape.Box(), false
				continue
			}
			r = r.Union(p.Shape.Box())
		}
		if first {
			continue
		}
		// The pads' own extent is the component: under a BGA that is the whole
		// ball field, and the margin covers the escape just outside it.
		r = r.Grow(o.Margin)
		i := len(c.comp)
		c.comp = append(c.comp, r)
		for x := int(math.Floor(r.MinX / c.step)); x <= int(math.Floor(r.MaxX/c.step)); x++ {
			for y := int(math.Floor(r.MinY / c.step)); y <= int(math.Floor(r.MaxY/c.step)); y++ {
				c.compIndex[cell{x, y}] = append(c.compIndex[cell{x, y}], i)
			}
		}
	}
}

// OpenClearance reports the open-field clearance in force, zero if none.
func (c *Checker) OpenClearance() float64 { return c.open.Clearance }

// aroundComponent reports whether an area falls across any component.
func (c *Checker) aroundComponent(r geom.Rect) bool {
	for x := int(math.Floor(r.MinX / c.step)); x <= int(math.Floor(r.MaxX/c.step)); x++ {
		for y := int(math.Floor(r.MinY / c.step)); y <= int(math.Floor(r.MaxY/c.step)); y++ {
			for _, i := range c.compIndex[cell{x, y}] {
				if c.comp[i].Overlaps(r) {
					return true
				}
			}
		}
	}
	return false
}

// overlap is the area two boxes share, or the second box when they share none.
//
// It localises a clash: the candidate can be a capsule spanning a whole track,
// and asking whether *that* is in the open would give the answer for the wrong
// place. Where the two pieces of copper come close is where the question is.
func overlap(a, b geom.Rect) geom.Rect {
	r := geom.Rect{
		MinX: math.Max(a.MinX, b.MinX), MinY: math.Max(a.MinY, b.MinY),
		MaxX: math.Min(a.MaxX, b.MaxX), MaxY: math.Min(a.MaxY, b.MaxY),
	}
	if r.MinX > r.MaxX || r.MinY > r.MaxY {
		return b
	}
	return r
}

type cell struct {
	x, y int
}

// New builds a checker over a board, indexing every obstacle once.
func New(b *board.Board, proj *board.Project) *Checker {
	c := &Checker{
		b:          b,
		proj:       proj,
		SameNetGap: 3 * math.Max(proj.MinTrackWidth, 0.09),
		index:      map[cell][]int{},
		step:       2.0,
	}

	for _, t := range b.Tracks {
		kind := "track"
		if t.Kind == board.KindVia {
			kind = "via"
		}
		for _, sh := range t.Shape(b.MaxError) {
			for _, l := range t.Layers(b.Stackup) {
				c.add(obstacle{shape: sh, layer: l, net: t.Net, kind: kind, ref: t.UUID})
			}
		}
	}
	for _, p := range b.Pads {
		for _, l := range b.CopperLayers {
			if p.OnLayer(l) {
				c.add(obstacle{shape: p.Shape, layer: l, net: p.Net, kind: "pad", ref: p.ID()})
			}
		}
	}
	for _, z := range b.Zones {
		for layer, polys := range z.Filled {
			for _, poly := range polys {
				c.add(obstacle{shape: geom.RoundPoly{Pts: poly}, layer: layer, net: z.Net, kind: "zone"})
			}
		}
	}
	// The board edge applies on every layer.
	for _, s := range b.Outline {
		c.add(obstacle{shape: geom.RoundPoly{Pts: []geom.Pt{s.A, s.B}}, kind: "board-edge", edge: true})
	}
	return c
}

// AddTrack indexes one more piece of copper.
//
// A checker is built over a whole board, which on a real one means several
// thousand shapes and a zone fill of a few thousand points. Rebuilding that
// after every edit is what a router does hundreds of times over, and it is the
// difference between a routing pass that takes seconds and one that takes
// minutes. Adding and retiring individual pieces keeps the index honest at a
// cost proportional to the edit rather than to the board.
func (c *Checker) AddTrack(t *board.Track) {
	kind := "track"
	if t.Kind == board.KindVia {
		kind = "via"
	}
	for _, sh := range t.Shape(c.b.MaxError) {
		for _, l := range t.Layers(c.b.Stackup) {
			c.add(obstacle{shape: sh, layer: l, net: t.Net, kind: kind, ref: t.UUID})
		}
	}
}

// RemoveRef retires every obstacle with this reference -- a track's uuid or a
// pad's id -- so copper taken off the board stops being an obstacle.
func (c *Checker) RemoveRef(ref string) {
	if ref == "" {
		return
	}
	for i := range c.obs {
		if c.obs[i].ref == ref {
			c.obs[i].dead = true
		}
	}
}

func (c *Checker) add(o obstacle) {
	i := len(c.obs)
	c.obs = append(c.obs, o)
	box := o.shape.Box()
	// A wide obstacle -- a long track, a zone -- spans many cells. Indexing it
	// in all of them keeps queries cheap at the cost of a little memory.
	for x := int(math.Floor(box.MinX / c.step)); x <= int(math.Floor(box.MaxX/c.step)); x++ {
		for y := int(math.Floor(box.MinY / c.step)); y <= int(math.Floor(box.MaxY/c.step)); y++ {
			c.index[cell{x, y}] = append(c.index[cell{x, y}], i)
		}
	}
}

// Candidate is a piece of copper being considered.
type Candidate struct {
	// Shapes is the candidate's copper on one layer.
	Shapes []geom.RoundPoly

	Layer string
	Net   string

	// Exempt lists obstacles the candidate is allowed to touch: the copper it
	// is replacing, and the copper at either end it has to join. Identified by
	// the ref recorded when the board was indexed, which for a track is its
	// uuid and for a pad its id.
	Exempt map[string]bool

	// MinClearance raises the clearance this candidate is held to, above what
	// its own net class asks.
	//
	// It exists for a candidate that stands in for several nets at once -- the
	// swept area of a whole bus, say -- where the right figure is the strictest
	// of the nets involved rather than any one of them.
	MinClearance float64
}

// Check returns every clash the candidate would cause, worst first.
func (c *Checker) Check(cand Candidate) []Violation {
	netClear := math.Max(c.proj.ClearanceOf(cand.Net), cand.MinClearance)
	var out []Violation
	seen := map[string]bool{}

	for _, sh := range cand.Shapes {
		box := sh.Box()
		for _, i := range c.near(box, c.searchMargin(netClear)) {
			o := &c.obs[i]
			if o.dead {
				continue
			}
			if !o.edge && o.layer != cand.Layer {
				continue
			}
			if o.ref != "" && cand.Exempt[o.ref] {
				continue
			}
			required := c.required(cand.Net, o, netClear, box)
			if required <= 0 {
				continue
			}
			d := sh.Dist(o.shape)
			if d >= required-1e-9 {
				continue
			}
			key := o.kind + "|" + o.ref + "|" + o.net
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Violation{
				Kind: o.kind, Ref: o.ref, Net: o.net, Layer: o.layer,
				Required: required, Actual: d, At: sh.Box().Centre(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Actual < out[j].Actual })
	return out
}

// Worst returns, for each obstacle the candidate clashes with, how little
// clearance it would leave. Used to compare a replacement against what it
// replaces.
func (c *Checker) Worst(cand Candidate) map[string]float64 {
	out := map[string]float64{}
	for _, v := range c.Check(cand) {
		key := v.Kind + "|" + v.Ref + "|" + v.Net
		if cur, ok := out[key]; !ok || v.Actual < cur {
			out[key] = v.Actual
		}
	}
	return out
}

// NoWorseThan reports whether a candidate clashes with nothing the baseline did
// not already clash with, and with nothing more tightly than the baseline did.
//
// This is what makes an edit to an existing track defensible on a board that
// already has violations -- and this board has 687 of them. Replacing a track
// with a longer path re-creates the parts of it that were not changed, and if
// the original copper was already too close to something, so is the copy. The
// question worth asking is not whether the replacement is clean but whether it
// is worse, and this answers exactly that.
func NoWorseThan(baseline, candidate map[string]float64) (ok bool, why string) {
	for key, actual := range candidate {
		was, existed := baseline[key]
		if !existed {
			return false, "new clash with " + key
		}
		if actual < was-1e-9 {
			return false, fmt.Sprintf("clearance to %s would fall from %.4f to %.4f mm", key, was, actual)
		}
	}
	return true, ""
}

// Clear reports whether the candidate has no clashes. It stops at the first
// one, which is what the tuner wants when trying many shapes.
func (c *Checker) Clear(cand Candidate) bool {
	netClear := math.Max(c.proj.ClearanceOf(cand.Net), cand.MinClearance)
	margin := c.searchMargin(netClear)
	for _, sh := range cand.Shapes {
		for _, i := range c.near(sh.Box(), margin) {
			o := &c.obs[i]
			if o.dead {
				continue
			}
			if !o.edge && o.layer != cand.Layer {
				continue
			}
			if o.ref != "" && cand.Exempt[o.ref] {
				continue
			}
			required := c.required(cand.Net, o, netClear, sh.Box())
			if required <= 0 {
				continue
			}
			if sh.Dist(o.shape) < required-1e-9 {
				return false
			}
		}
	}
	return true
}

// required is the clearance between a candidate and one obstacle. box is the
// candidate shape's extent, used to place the clash.
func (c *Checker) required(net string, o *obstacle, netClear float64, box geom.Rect) float64 {
	switch {
	case o.edge:
		return c.proj.EdgeClearance
	case o.net == net:
		// Same net: not a KiCad violation, but a meander must not weld itself
		// to the track it came from. Zones of the same net are a deliberate
		// connection and are left alone.
		if o.kind == "zone" || o.kind == "pad" {
			return 0
		}
		return c.SameNetGap
	default:
		req := math.Max(netClear, c.proj.ClearanceOf(o.net))
		// The board's own rules, where one applies to both items. They may
		// always tighten; whether they may loosen is the caller's call.
		if c.rules != nil {
			if v, ok := c.rules.ClearanceFor(candItem(net, box), obstacleItem(o)); ok {
				if c.relax || v > req {
					req = v
				}
			}
		}
		// A region the user asked for more space in. Only between two pieces
		// of copper that are both in it, and only ever upward.
		if !c.pairOf(net, o.net) {
			if z := c.zoneClearance(box, o.shape.Box()); z > req {
				req = z
			}
		}
		if c.open.Clearance > req && !c.pairOf(net, o.net) &&
			!c.aroundComponent(overlap(box.Grow(c.open.Clearance), o.shape.Box().Grow(c.open.Clearance))) {
			req = c.open.Clearance
		}
		return req
	}
}

// candItem describes the copper being offered, for a rule condition. It is
// always a track: this tool draws tracks and vias, and a via is a track to the
// rule language's way of thinking about clearance.
func candItem(net string, box geom.Rect) board.Item {
	return board.Item{Type: "Track", Net: net, Box: box}
}

// obstacleItem describes indexed copper for a rule condition.
func obstacleItem(o *obstacle) board.Item {
	kind := "Track"
	switch o.kind {
	case "pad":
		kind = "Pad"
	case "via":
		kind = "Via"
	case "zone":
		kind = "Zone"
	}
	owner := ""
	if o.kind == "pad" {
		if i := strings.IndexByte(o.ref, '.'); i > 0 {
			owner = o.ref[:i]
		}
	}
	return board.Item{Type: kind, Net: o.net, Owner: owner, Box: o.shape.Box()}
}

// pairOf reports whether two nets are the halves of one differential pair.
func (c *Checker) pairOf(a, b string) bool {
	return c.open.Coupled[a] == b || c.open.Coupled[b] == a
}

// searchMargin is how far out to look for obstacles: far enough that nothing
// which could be too close is missed, the open-field clearance included.
func (c *Checker) searchMargin(netClear float64) float64 {
	widest := math.Max(math.Max(netClear, c.proj.EdgeClearance), c.open.Clearance)
	for _, z := range c.zones {
		widest = math.Max(widest, z.MinClearance)
	}
	return widest + c.SameNetGap
}

// near returns the indices of obstacles whose boxes come within margin of box.
func (c *Checker) near(box geom.Rect, margin float64) []int {
	g := box.Grow(margin)
	var out []int
	seen := map[int]bool{}
	for x := int(math.Floor(g.MinX / c.step)); x <= int(math.Floor(g.MaxX/c.step)); x++ {
		for y := int(math.Floor(g.MinY / c.step)); y <= int(math.Floor(g.MaxY/c.step)); y++ {
			for _, i := range c.index[cell{x, y}] {
				if seen[i] {
					continue
				}
				if !c.obs[i].shape.Box().Overlaps(g) {
					continue
				}
				seen[i] = true
				out = append(out, i)
			}
		}
	}
	return out
}

// Obstacles is how many obstacles were indexed, for reporting.
func (c *Checker) Obstacles() int { return len(c.obs) }

// ExemptTracks builds the exemption set for a run of tracks.
func ExemptTracks(ts ...*board.Track) map[string]bool {
	m := map[string]bool{}
	for _, t := range ts {
		if t != nil && t.UUID != "" {
			m[t.UUID] = true
		}
	}
	return m
}
