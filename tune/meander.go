// Package tune lengthens an existing track by folding a meander into it.
//
// Nothing here reroutes. A straight run of the existing track is replaced by a
// longer path with exactly the same two endpoints, on the same layer, at the
// same width, so the net's topology, its layer transitions and its connections
// are all untouched. Everything outside the replaced run is left as it was, and
// every candidate is cleared with the design rules before it is accepted.
package tune

import (
	"fmt"
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Style controls meander shape.
type Style struct {
	// MaxAmplitude caps how far a meander may stray from the original track.
	MaxAmplitude float64

	// MinAmplitude is the smallest excursion worth drawing. Below it a meander
	// is all corner and no benefit.
	MinAmplitude float64

	// Chamfer is the 45 degree cut taken off each corner, as a multiple of the
	// track width.
	//
	// Square corners in a meander are an impedance discontinuity and a
	// manufacturing nuisance, so every corner is mitred. It costs a little
	// length per corner, which the arithmetic accounts for exactly.
	Chamfer float64

	// Gap is the copper-to-copper spacing between adjacent meander legs, as a
	// multiple of the track width.
	//
	// This is the number that decides whether a meander is sound or merely
	// legal. Parallel runs of the same signal couple to each other, and the
	// coupling both eats into the delay the meander was added for and degrades
	// the edge. Three track widths of clear space -- the usual "3W" guidance --
	// is the default. It is a gap, not a pitch: legs end up 4 widths apart
	// centre to centre.
	Gap float64

	// MinRunLength is the shortest straight track worth meandering.
	MinRunLength float64

	// KeepClearOfPads keeps meanders this far from any pad, so tuning does not
	// end up inside a BGA fanout where it would not fit and does not belong.
	KeepClearOfPads float64
}

// DefaultStyle returns a style suitable for a DDR interface.
//
// Gap stays at the full three track widths: it is the one figure here that is
// about signal integrity rather than fit, and relaxing it would buy length by
// spoiling the thing the length was being matched for. The rest are set so a
// meander will be attempted wherever there is genuinely room, since on a
// densely routed board there is not much.
func DefaultStyle() Style {
	return Style{
		MaxAmplitude:    1.5,
		MinAmplitude:    0.12,
		Chamfer:         1.0,
		Gap:             3.0,
		MinRunLength:    0.8,
		KeepClearOfPads: 0.3,
	}
}

// Result reports what tuning one net achieved.
type Result struct {
	Net string

	// Leg is what this length was asked for, as "U3->U4", when the caller is
	// matching a net over more than one span of a chain and so asks for length
	// on the same net more than once. The tuner never sets it: it knows about
	// copper, not about topology, and fills a request for a set of tracks
	// without needing to know why those tracks.
	Leg string

	// Requested is the length that was asked for, Added what was achieved.
	Requested, Added float64

	// Shortfall is what could not be fitted.
	Shortfall float64

	// Meanders is how many separate meanders were drawn.
	Meanders int

	// Replaced and Created count the tracks removed and added.
	Replaced, Created int

	// Notes explains any shortfall.
	Notes []string
}

// Met reports whether the requested length was achieved to within tol.
func (r *Result) Met(tol float64) bool { return r.Shortfall <= tol }

// Tuner adds length to nets on a board.
type Tuner struct {
	b     *board.Board
	chk   *drc.Checker
	proj  *board.Project
	style Style

	// open and rules are what the checker is built with, kept here rather than
	// captured in a closure so that setting one does not quietly discard the
	// other.
	open  drc.OpenSpacing
	rules *board.Rules

	// areas restrict where copper may be added. Empty means anywhere.
	areas Areas

	// rebuild re-indexes the design rule checker. Copper added to one net is an
	// obstacle for the next, so the index is refreshed after every change.
	rebuild func() *drc.Checker
}

// SetOpenSpacing holds everything this tuner draws to a wider clearance away
// from the components, whatever the board's own rules say there.
//
// It goes through the checker, so it governs every question asked of the
// geometry and not only the copper written out: how much room there is beside
// a trace, how far a bus could be spread, and whether a meander fits.
func (t *Tuner) SetOpenSpacing(o drc.OpenSpacing) {
	t.open = o
	t.chk = t.rebuild()
}

// SetRules applies the board's own custom design rules to everything this tuner
// asks of the geometry.
func (t *Tuner) SetRules(r *board.Rules) {
	t.rules = r
	t.chk = t.rebuild()
}

// NewTuner builds a tuner for a board.
func NewTuner(b *board.Board, proj *board.Project, style Style) *Tuner {
	t := &Tuner{b: b, proj: proj, style: style}
	t.rebuild = func() *drc.Checker {
		c := drc.New(t.b, t.proj)
		c.SetOpenSpacing(t.open)
		// false: the rules may tighten what the tuner draws to, never loosen
		// it. A meander has no business being crammed into a BGA fanout at the
		// clearance that exists so the fanout can be escaped at all.
		c.SetRules(t.rules, false)
		c.SetClearanceZones(t.areas.Spacing())
		return c
	}
	t.chk = t.rebuild()
	return t
}

// Checker exposes the current design rule checker, for reporting.
func (t *Tuner) Checker() *drc.Checker { return t.chk }

// run is a straight piece of an existing track that could carry a meander.
type run struct {
	track *board.Track
	// usable is the part of the track a meander may occupy, shortened at each
	// end to stay clear of junctions and pads.
	from, to geom.Pt
	length   float64
	// amp is the largest excursion the surroundings allow, and side which way.
	amp  float64
	side geom.Pt
}

// Headroom is the most length that could be folded into a net without breaking
// any rule: the sum of what every usable stretch of its route can hold.
//
// This is the number that says what kind of problem a shortfall is. A net
// needing 0.7 mm with 0.2 mm of headroom is short of space, and re-spacing the
// bus around it might find some. A net needing 14 mm on a 17 mm route is not
// short of space at all -- no meander turns a 17 mm route into a 31 mm one -- and
// no amount of tuning will help. Reporting the two the same way would leave the
// user to guess which they had.
func (t *Tuner) Headroom(net string, on map[string]bool) float64 {
	width := t.trackWidth(net)
	if width <= 0 {
		return 0
	}
	var total float64
	for _, r := range t.findRuns(net, width, on) {
		total += t.capacity(r.length, r.amp, width)
	}
	return total
}

// BundleSpare is the clear space the buses these nets run in could be spread
// into, summed over every bundle found.
//
// It bounds what re-spacing the bus could add. Where it is small, a shortfall
// is not going to be solved by moving the neighbours either.
func (t *Tuner) BundleSpare(nets []string) (spare, span float64) {
	for _, b := range t.FindBundles(nets, DefaultBundleOptions()) {
		spare += b.Corridor() - b.Width()
		span += b.Span()
	}
	return spare, span
}

// Tune adds `need` millimetres to a net, returning what it managed.
//
// `on` restricts which of the net's tracks may carry a meander, by uuid, and
// must be the tracks of the route whose length is being corrected. That is not
// an optimisation. A net can have copper that no route runs over -- a dangling
// stub, or a whole island stranded because a leg was never finished -- and
// lengthening one of those adds copper while the path being matched does not
// move. The clock pair on the demo board has exactly that shape. Passing nil
// allows the whole net, which is only right when the net has one route.
//
// Runs are taken longest first, because a long straight has room for more
// meander and is usually further from the dense fanout at either end. Each run
// is given as much of the remaining requirement as it can hold, and the last
// one used is trimmed to land on the target exactly rather than overshooting.
func (t *Tuner) Tune(net string, need float64, on map[string]bool) (*Result, error) {
	res := &Result{Net: net, Requested: need}
	if need <= 0 {
		return res, nil
	}
	width := t.trackWidth(net)
	if width <= 0 {
		return nil, fmt.Errorf("tune: cannot determine a track width for %s", net)
	}

	runs := t.findRuns(net, width, on)
	if len(runs) == 0 {
		res.Shortfall = need
		if len(on) == 0 && len(t.b.TracksOfNet(net)) == 0 {
			res.Notes = append(res.Notes, "this net has no copper on the board")
		} else {
			res.Notes = append(res.Notes,
				fmt.Sprintf("no straight run of at least %.2f mm with room beside it, on the route being matched",
					t.style.MinRunLength))
		}
		return res, nil
	}

	// Group the stretches by the track they are on, because a track is replaced
	// once with every meander it carries. Tracks are taken in the order
	// findRuns gave them -- most capacious first -- so the requirement is spent
	// where there is most room for it.
	type group struct {
		track *board.Track
		runs  []run
	}
	var groups []group
	index := map[*board.Track]int{}
	for _, r := range runs {
		if i, ok := index[r.track]; ok {
			groups[i].runs = append(groups[i].runs, r)
			continue
		}
		index[r.track] = len(groups)
		groups = append(groups, group{track: r.track, runs: []run{r}})
	}

	remaining := need
	for _, g := range groups {
		if remaining <= 1e-6 {
			break
		}
		exempt := t.exemptFor(g.track, net)
		var shapes []meanderShape
		var claimed float64
		for _, r := range g.runs {
			if remaining-claimed <= 1e-6 {
				break
			}
			m, gain, ok := t.planMeander(r, net, width, remaining-claimed, exempt)
			if !ok {
				continue
			}
			shapes = append(shapes, m)
			claimed += gain
		}
		got, created, err := t.applyTrack(g.track, shapes, net, width, exempt)
		if err != nil {
			return nil, err
		}
		if got <= 0 {
			continue
		}
		remaining -= got
		res.Added += got
		res.Meanders += len(shapes)
		res.Replaced++
		res.Created += created
		// The board changed, so the obstacle index has to.
		t.chk = t.rebuild()
	}
	res.Shortfall = math.Max(0, remaining)
	if res.Shortfall > 1e-6 {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"fitted %.3f mm of the %.3f mm needed across %d meander(s); the rest does not fit without rerouting",
			res.Added, need, res.Meanders))
	}
	return res, nil
}

func (t *Tuner) trackWidth(net string) float64 {
	// Prefer the width actually used on the net: the class may disagree with
	// what was routed, and a meander has to match its neighbours.
	counts := map[float64]int{}
	for _, tr := range t.b.TracksOfNet(net) {
		if tr.Kind != board.KindVia && tr.Width > 0 {
			counts[tr.Width]++
		}
	}
	best, bestN := 0.0, 0
	for w, n := range counts {
		if n > bestN || (n == bestN && w > best) {
			best, bestN = w, n
		}
	}
	if best > 0 {
		return best
	}
	return t.proj.TrackWidthOf(net)
}

// findRuns lists the stretches of a net's existing track that a meander could
// occupy, most capacious first. Only tracks in `on` are considered, unless it
// is empty.
//
// The room beside a track is not uniform along it, and asking for one amplitude
// that holds over the whole thing throws away most of what a real board offers:
// a 20 mm track squeezed by a neighbour for 2 mm of its length measures as
// having no room at all. So the room is profiled in steps and the best
// non-overlapping stretches are taken, which on the demo board raises the
// usable capacity of the out-of-tolerance nets from 49 mm to 67 mm.
func (t *Tuner) findRuns(net string, width float64, on map[string]bool) []run {
	var out []run
	margin := t.style.Gap * width
	for _, tr := range t.b.TracksOfNet(net) {
		if tr.Kind != board.KindSegment || tr.Width <= 0 {
			continue
		}
		if len(on) > 0 && !on[tr.UUID] {
			continue
		}
		l := tr.Length(t.b.Stackup)
		if l < t.style.MinRunLength {
			continue
		}
		// Nothing may be added where the user has not allowed it, and a track
		// no area covers is not worth profiling at all.
		if !t.areas.Overlaps(geom.RectFromPts(tr.Start, tr.End)) {
			continue
		}
		// Leave the ends alone: a meander butted against a corner or a pad has
		// nowhere to turn, and the fanout at either end of a DDR net is the
		// last place to put one.
		keep := math.Max(margin, t.style.KeepClearOfPads)
		if l-2*keep < t.style.MinRunLength {
			continue
		}
		dir := tr.End.Sub(tr.Start).Norm()
		from := tr.Start.Add(dir.Mul(keep))
		to := tr.End.Sub(dir.Mul(keep))
		out = append(out, t.subRuns(tr, net, from, to, width)...)
	}
	// Most capacious first: a stretch that can hold more length is the one to
	// spend the requirement on.
	sort.Slice(out, func(i, j int) bool {
		ci, cj := t.capacity(out[i].length, out[i].amp, width), t.capacity(out[j].length, out[j].amp, width)
		if math.Abs(ci-cj) > 1e-9 {
			return ci > cj
		}
		return out[i].length > out[j].length
	})
	return out
}

// capacity is the most length a meander of this amplitude could add over this
// stretch. It is the same arithmetic meanderRun uses, so the ordering above
// reflects what will actually be achievable.
func (t *Tuner) capacity(span, amp, width float64) float64 {
	if span <= 0 || amp < t.style.MinAmplitude {
		return 0
	}
	c := t.style.Chamfer * width
	space := (t.style.Gap + 1) * width
	flat := space + 2*c
	if amp < 2*c || flat <= 0 {
		return 0
	}
	n := math.Floor((span + space) / (flat + space))
	if n < 1 {
		return 0
	}
	g := n * (2*amp - 4*c*(2-math.Sqrt2))
	return math.Max(0, g)
}

// subRuns profiles the room along a stretch of track and returns the best
// non-overlapping pieces of it, one per side where that side is the better one.
func (t *Tuner) subRuns(tr *board.Track, net string, from, to geom.Pt, width float64) []run {
	usable := from.Dist(to)
	dir := to.Sub(from).Norm()
	perp := dir.Perp()
	exempt := t.exemptFor(tr, net)

	// A step of half the shortest meanderable run: fine enough to find a clear
	// stretch, coarse enough that a long track does not cost hundreds of
	// clearance queries.
	step := math.Max(t.style.MinRunLength/2, 0.2)
	n := int(math.Floor(usable / step))
	if n < 1 {
		return nil
	}

	type candidate struct {
		i, j int // step range, half-open
		amp  float64
		side geom.Pt
		cap  float64
	}
	var cands []candidate

	for _, side := range []geom.Pt{perp, perp.Mul(-1)} {
		prof := make([]float64, n)
		for i := range n {
			a := from.Add(dir.Mul(float64(i) * step))
			c := from.Add(dir.Mul(float64(i+1) * step))
			// A step the user has not allowed has no room in it, whatever the
			// clearance says. Recorded as zero rather than skipped, so a
			// stretch cannot run through a disallowed step to reach an allowed
			// one beyond it.
			if !t.areas.AllowsSpan(a, c) {
				continue
			}
			// And the excursion has to stay inside too, not only the stretch
			// it is folded into. A meander wanders sideways by its amplitude,
			// so the area's edge caps the room as surely as a neighbouring
			// track does.
			room := t.roomBesideOn(tr, net, a, c, width, side, exempt)
			if lim := t.areas.RoomToward(a, c, side) - width/2; lim < room {
				room = math.Max(0, lim)
			}
			prof[i] = room
		}
		// Every contiguous stretch, scored by what it could hold. The amplitude
		// of a stretch is the smallest along it, because the meander has to fit
		// the tightest point.
		for i := range n {
			lo := prof[i]
			for j := i + 1; j <= n; j++ {
				if prof[j-1] < lo {
					lo = prof[j-1]
				}
				if lo < t.style.MinAmplitude {
					break
				}
				span := float64(j-i) * step
				if span < t.style.MinRunLength {
					continue
				}
				if c := t.capacity(span, lo, width); c > 0 {
					cands = append(cands, candidate{i, j, lo, side, c})
				}
			}
		}
	}
	if len(cands) == 0 {
		return nil
	}
	// Greedily take the most capacious stretches that do not overlap. Two
	// stretches cannot share track, whichever side each would meander to.
	sort.Slice(cands, func(a, b int) bool {
		if math.Abs(cands[a].cap-cands[b].cap) > 1e-9 {
			return cands[a].cap > cands[b].cap
		}
		if cands[a].i != cands[b].i {
			return cands[a].i < cands[b].i
		}
		return cands[a].j < cands[b].j
	})
	taken := make([]bool, n)
	var out []run
	for _, c := range cands {
		clash := false
		for k := c.i; k < c.j; k++ {
			if taken[k] {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		for k := c.i; k < c.j; k++ {
			taken[k] = true
		}
		a := from.Add(dir.Mul(float64(c.i) * step))
		b := from.Add(dir.Mul(float64(c.j) * step))
		out = append(out, run{
			track: tr, from: a, to: b, length: a.Dist(b), amp: c.amp, side: c.side,
		})
	}
	return out
}

// roomBesideOn measures how far a meander may stray from a stretch of track, on
// one given side.
//
// The probe is the whole swept area rather than a single line, so an obstacle
// poking into the middle of the excursion is found. The amplitude is bisected
// rather than stepped, which locates the limit to a few micrometres in about a
// dozen clearance queries.
func (t *Tuner) roomBesideOn(tr *board.Track, net string, from, to geom.Pt, width float64, side geom.Pt, exempt map[string]bool) float64 {
	fits := func(amp float64) bool {
		var shapes []geom.RoundPoly
		steps := int(math.Ceil(amp/width)) + 1
		for i := 0; i <= steps; i++ {
			off := amp * float64(i) / float64(steps)
			shapes = append(shapes,
				geom.Capsule(from.Add(side.Mul(off)), to.Add(side.Mul(off)), width))
		}
		return t.chk.Clear(drc.Candidate{Shapes: shapes, Layer: tr.Layer, Net: net, Exempt: exempt})
	}
	if !fits(t.style.MinAmplitude) {
		return 0
	}
	if fits(t.style.MaxAmplitude) {
		return t.style.MaxAmplitude
	}
	// Eight halvings of the amplitude range settle it to about 5 micrometres,
	// which is far finer than any decision made from it and half the cost of
	// the fourteen an earlier version used. The profile calls this once per
	// step along every candidate track, so the constant matters.
	lo, hi := t.style.MinAmplitude, t.style.MaxAmplitude
	for range 8 {
		mid := (lo + hi) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// roomBeside measures how far a meander may stray from a whole track, on
// whichever side has more space. Kept for the bundle survey and for tests that
// want the simple answer.
func (t *Tuner) roomBeside(tr *board.Track, net string, from, to geom.Pt, width float64) (float64, geom.Pt) {
	perp := to.Sub(from).Norm().Perp()
	exempt := t.exemptFor(tr, net)
	best, bestSide := 0.0, perp
	for _, side := range []geom.Pt{perp, perp.Mul(-1)} {
		if a := t.roomBesideOn(tr, net, from, to, width, side, exempt); a > best {
			best, bestSide = a, side
		}
	}
	return best, bestSide
}

// exemptFor lists the copper a meander on this track is allowed to touch: the
// track itself, and whatever it joins at either end.
func (t *Tuner) exemptFor(tr *board.Track, net string) map[string]bool {
	exempt := drc.ExemptTracks(tr)
	for _, o := range t.b.TracksOfNet(net) {
		if o == tr {
			continue
		}
		// Anything sharing an endpoint with this track is a neighbour it is
		// already connected to.
		for _, a := range []geom.Pt{tr.Start, tr.End} {
			for _, c := range []geom.Pt{o.Start, o.End} {
				if a.Dist(c) < 0.1 && o.UUID != "" {
					exempt[o.UUID] = true
				}
			}
		}
	}
	for _, p := range t.b.PadsOfNet(net) {
		exempt[p.ID()] = true
	}
	return exempt
}

// Space is roughly what a meander needs to add a given length.
//
// Two numbers, because they answer different questions. Run is how much
// straight track it has to be folded into, which is what somebody looks for
// when they ask "where could this go". Area is the board that meander would
// occupy -- the run times the width of the band it sweeps -- which is what
// somebody is deciding about when they ask "have I got room for this".
//
// Both are estimates and are meant to be read as such. They assume the meander
// gets the full amplitude it is allowed, which is the best case: half the
// amplitude needs twice the run, and a track already hemmed in on one side
// gets neither.
type Space struct {
	RunMM   float64
	AreaMM2 float64

	// AmplitudeMM is the excursion the estimate assumes, and WidthMM the
	// track it is beside, so a reader can see what it was worked out from.
	AmplitudeMM float64
	WidthMM     float64
}

// SpaceFor estimates what adding gain mm to a net would take.
//
// The arithmetic is the meander's own, run backwards. One tooth spans
// flat + space of track and gains 2a - 4c(2-sqrt2); the number of teeth needed
// is the requirement divided by that, and the run is what those teeth span.
// The band it sweeps is the amplitude plus the track itself, which is the
// copper a neighbour has to keep clear of.
func (t *Tuner) SpaceFor(net string, gain float64) Space {
	width := t.trackWidth(net)
	amp := t.style.MaxAmplitude
	out := Space{AmplitudeMM: amp, WidthMM: width}
	if gain <= 0 || width <= 0 || amp < t.style.MinAmplitude {
		return out
	}
	c := t.style.Chamfer * width
	space := (t.style.Gap + 1) * width
	flat := space + 2*c
	perTooth := 2*amp - 4*c*(2-math.Sqrt2)
	if perTooth <= 0 {
		return out
	}
	teeth := gain / perTooth
	out.RunMM = teeth * (flat + space)
	out.AreaMM2 = out.RunMM * (amp + width)
	return out
}
