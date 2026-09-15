package tune

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Expanding a bus: making room where there is none.
//
// A bus routed at its minimum clearance has no space beside any one track, so
// there is nowhere to fold a meander. Profiling the room finds the stretches
// where a neighbour happens to fall away; where the whole bus runs tight, it
// finds nothing, because nothing is there.
//
// What a layout engineer does then is spread the bus out. The traces move
// sideways into whatever clear space the bus as a whole has beside it, each one
// a little further than the one below, so gaps open between them -- and a gap is
// exactly what a meander needs. Nothing is rerouted: each trace keeps its own
// endpoints, steps across at 45 degrees inside its own length, runs along its
// new line, and steps back.
//
// Two things make it safe to do. The traces keep their order, so none can cross
// another. And no two neighbours step at the same place along the bus: each
// one's steps are staggered past the steps of everything outside it.
//
// The stagger is not tidiness, it is the whole safety argument. Two parallel
// tracks stepping side by side over the same stretch are not the pitch apart
// any more -- two parallel 45 degree lines whose cross-bus gap is g sit only
// g/sqrt(2) apart, 29% closer. On a bus already at its minimum clearance that
// is a violation, so a bus where every trace stepped at once could never move
// more than one trace. Staggered, every step happens beside a neighbour that
// is running straight, and the closest the two ever come is their final gap,
// which is wider than the pitch they started at.
//
// Two things bound what it can achieve, and both are properties of the board
// rather than of this code:
//
//   - Moving a trace sideways lengthens it, by about 0.83 mm per millimetre of
//     travel over the two steps. That length counts toward what the trace
//     needed, which is convenient -- but a trace that needs nothing cannot be
//     moved at all, because lengthening the longest member of a group raises the
//     target for every other member of it. So a trace's travel is capped by its
//     own requirement.
//   - The gap opened for a trace is how much further the trace above it moved.
//     The topmost trace's cap therefore limits every trace below it, however
//     much clear space lies beyond.
//
// Which is why this is opt-in and best-effort: it helps a bus with room beside
// it and a requirement spread across its members, and it says plainly when it
// cannot help.

// rampGainPerMM is the length two 45 degree steps add per millimetre of sideways
// travel: each step covers d sideways and d along, so it is d*sqrt(2) of track
// where d would have done.
var rampGainPerMM = 2 * (math.Sqrt2 - 1)

// roomMargin is how much of a gap this planner declines to count on.
//
// A gap has to be wide enough for a mitred excursion or it holds no meander at
// all -- there is a threshold, not a taper -- so the search naturally settles
// on the least travel that just clears it. Least is the right instinct and
// exactly is the wrong place to stop: the room is measured afterwards by
// bisection, which under-reports by up to five micrometres, and a gap opened
// to the threshold then measures a hair under it and holds nothing. Aiming
// past the threshold by twice that costs a few hundredths of a millimetre of
// travel and is the difference between a gap that works and one that does not.
const roomMargin = 0.01

// ExpandOptions tune bus expansion.
type ExpandOptions struct {
	Bundle BundleOptions

	// MaxDisplacement caps how far any one trace may be moved sideways,
	// whatever room is available. A large move is a large change to a board
	// somebody else drew.
	MaxDisplacement float64

	// MinGain is the least a bundle must promise before it is worth disturbing.
	MinGain float64

	// Coupled maps a net to the net it must move with, so the two halves of a
	// differential pair keep their spacing. A pair whose halves are not
	// neighbours in the bus is left alone entirely.
	Coupled map[string]string
}

// DefaultExpandOptions returns conservative settings.
func DefaultExpandOptions() ExpandOptions {
	return ExpandOptions{
		Bundle:          DefaultBundleOptions(),
		MaxDisplacement: 2.0,
		MinGain:         0.2,
	}
}

// ExpandMember is what happened to one trace.
type ExpandMember struct {
	Net string

	// DisplacementMM is how far it moved sideways.
	DisplacementMM float64

	// OpenedMM is the room that opened beside it as a result.
	OpenedMM float64

	// AddedMM is the length its two steps added.
	AddedMM float64
}

// ExpandResult is what happened to one bus.
type ExpandResult struct {
	// Group names the matching group whose buses these are, for reporting.
	Group string

	Layer  string
	SpanMM float64
	Nets   []string

	Members []ExpandMember

	// OpenedMM is the total room opened across the bus.
	OpenedMM float64

	// UsefulMM is how much extra length the spread actually delivers: the steps'
	// own contribution plus what each trace could now fold into the room beside
	// it, capped at what that trace needed.
	//
	// It is the figure worth reporting. Room opened beside a trace that needs
	// nothing buys nothing, and on a bus where the outermost trace is the one
	// already at the target -- which is common, since it is the outermost that
	// has the clear space beside it -- most of the room opens where it cannot
	// be spent.
	UsefulMM float64

	// AddedMM is the length the steps themselves added, which counts toward
	// what the traces needed.
	AddedMM float64

	// Skipped explains a bus that was left alone.
	Skipped string
}

// Expand spreads the buses these nets run in, to open room for meandering.
//
// `need` is how much length each net is short, which is what caps how far each
// may be moved. Buses that cannot be helped are reported with a reason rather
// than silently passed over: on a dense board that is most of them, and knowing
// which is the difference between "open some space here" and "reroute this".
//
// Every bus it can help, it helps, even where that is only part of what is
// needed -- a board that is closer to matched is worth more than one left alone
// because it could not be finished.
func (t *Tuner) Expand(nets []string, need map[string]float64, opt ExpandOptions) ([]*ExpandResult, error) {
	if opt.MaxDisplacement <= 0 {
		opt = DefaultExpandOptions()
	}
	var out []*ExpandResult
	for _, b := range t.FindBundles(nets, opt.Bundle) {
		res, err := t.expandBundle(b, need, opt)
		if err != nil {
			return out, err
		}
		if res != nil {
			out = append(out, res)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedMM > out[j].OpenedMM })
	return out, nil
}

// unit is one or two adjacent members that have to move together.
type unit struct {
	members []BundleMember

	// lo and hi are the unit's outer edges across the bus.
	lo, hi float64

	// need is the least of its members' requirements, because moving it
	// lengthens all of them equally and the smallest requirement is what
	// overshoots first.
	need float64

	// disp is how far it will move, filled in by the allocation.
	disp float64

	// flat is the straight run left in the middle of it afterwards, which is
	// what a meander has to fit into. Both this unit's own steps and the steps
	// of everything further out eat into it.
	flat float64

	// opened is the room that ends up beside it.
	opened float64

	// gain0 and gain are the length this unit could fold in before and after
	// the spread. The difference is what the spread is worth to it, and
	// scoring on the difference is what keeps a bus from being credited with
	// room it already had.
	gain0, gain float64
}

func (u *unit) width() float64 { return u.hi - u.lo }

// expandBundle works out and applies the spread for one bus.
func (t *Tuner) expandBundle(b *Bundle, need map[string]float64, opt ExpandOptions) (*ExpandResult, error) {
	res := &ExpandResult{Layer: b.Layer, SpanMM: b.Span(), Nets: b.Nets()}

	// A net appearing twice in one bus would have two tracks to move in step,
	// and getting that wrong breaks a connection. Not worth the risk.
	seen := map[string]bool{}
	for _, m := range b.Members {
		if seen[m.Net] {
			res.Skipped = fmt.Sprintf("%s has two tracks in this bus, so moving it is not straightforward", shortName(m.Net))
			return res, nil
		}
		seen[m.Net] = true
	}

	// Spreading a bus moves copper sideways into space beside it, which is
	// just as much "putting copper somewhere new" as folding in a meander. If
	// the user has said where that may happen, the stretch being spread has to
	// be there.
	if !t.areas.AllowsSpan(b.point(b.U0, 0), b.point(b.U1, 0)) {
		res.Skipped = "this bus is outside the areas you allowed copper to be added in"
		return res, nil
	}

	units, note := groupUnits(b, opt.Coupled)
	if note != "" {
		res.Skipped = note
		return res, nil
	}
	if len(units) < 2 {
		res.Skipped = "only one trace here can move, so spreading opens nothing"
		return res, nil
	}
	for i := range units {
		units[i].need = unitNeed(units[i], need)
	}

	// Spread toward whichever side has room. Expanding both ways at once would
	// need the bus split at a trace that stays put, and there is rarely one.
	side, free := 1.0, b.FreePos
	if b.FreeNeg > b.FreePos {
		side, free = -1.0, b.FreeNeg
	}
	if free < 0.05 {
		res.Skipped = "no clear space beside this bus to spread into"
		return res, nil
	}
	// Index the units in the direction of travel, so unit 0 stays put and the
	// last one moves furthest.
	if side < 0 {
		for i, j := 0, len(units)-1; i < j; i, j = i+1, j-1 {
			units[i], units[j] = units[j], units[i]
		}
	}

	// A unit only gets room if the unit beyond it moves away, so the outermost
	// one is the gate. If it needs no length it cannot move: the two steps
	// would lengthen it, and it is already the longest trace in its group, so
	// that raises the target for every other member. Say which trace it is --
	// the space is real, and reaching it is a rerouting decision for whoever
	// drew the board.
	if outer := units[len(units)-1]; outer.need <= 1e-9 {
		res.Skipped = fmt.Sprintf(
			"the %.3f mm of clear space here is behind %s, which needs no length: moving it would lengthen it and raise the target for its whole group",
			free, unitName(outer))
		return res, nil
	}

	width := b.Members[0].Width
	keep := math.Max(t.style.Gap*width, t.style.KeepClearOfPads)
	span := b.Span() - 2*keep
	if span < t.style.MinRunLength {
		res.Skipped = fmt.Sprintf("the shared stretch is only %.2f mm, too short to step aside and back", b.Span())
		return res, nil
	}

	plan, opened, useful := t.allocate(units, free, span, width, opt)
	if useful < opt.MinGain {
		res.Skipped = fmt.Sprintf(
			"spreading this bus would open %.3f mm of room but only %.3f mm of length beyond what these traces can already reach",
			opened, useful)
		return res, nil
	}

	// Apply from the far end inward: each trace moves into space the one beyond
	// it has already vacated.
	// Apply from the far end inward, staggering as we go: the units already
	// moved have used that much of the bus for their own steps, so this one's
	// steps start past them.
	applied, stagger := 0, 0.0
	for i := len(plan) - 1; i >= 0; i-- {
		u := plan[i]
		if u.disp <= 1e-6 {
			continue
		}
		added, err := t.moveUnit(b, u, side, keep, width, stagger)
		if err != nil {
			return res, err
		}
		if added <= 0 {
			// The move did not clear the rules. This unit and everything below
			// it depended on the space it would have vacated, so stop rather
			// than press on.
			for j := i; j >= 0; j-- {
				plan[j].disp = 0
				plan[j].opened = 0
			}
			break
		}
		stagger += u.disp
		applied++
		for _, m := range u.members {
			res.Members = append(res.Members, ExpandMember{
				Net:            m.Net,
				DisplacementMM: u.disp,
				OpenedMM:       u.opened,
				AddedMM:        added / float64(len(u.members)),
			})
		}
		res.AddedMM += added
		t.chk = t.rebuild()
	}
	if applied == 0 {
		res.Members = nil
		res.AddedMM = 0
		res.Skipped = "the traces here could not be moved without breaking a clearance"
		return res, nil
	}
	// The same accounting as the allocation, over the plan as it was actually
	// applied: gaps between traces only, and the length each trace gained over
	// what it could already have folded in.
	//
	// Every unit counts here, not only the ones that moved. The trace that
	// gains the most from a spread is usually one that stayed exactly where it
	// was and had the trace beyond it move away -- skipping those reported
	// 0.000 mm for a spread that finished a net.
	for i, u := range plan {
		if i < len(plan)-1 {
			res.OpenedMM += u.opened
		}
		along := u.flat
		if i < len(plan)-1 {
			along = math.Min(along, plan[i+1].flat)
		}
		gain := u.disp*rampGainPerMM + t.capacity(math.Max(0, along-2*keep), u.opened-roomMargin, width)
		res.UsefulMM += math.Max(0, math.Min(u.need, gain)-u.gain0)
	}
	sort.Slice(res.Members, func(i, j int) bool { return res.Members[i].Net < res.Members[j].Net })
	return res, nil
}

// groupUnits merges the two halves of a differential pair into one unit, so they
// move together and keep their spacing.
func groupUnits(b *Bundle, coupled map[string]string) ([]*unit, string) {
	var out []*unit
	for i := 0; i < len(b.Members); i++ {
		m := b.Members[i]
		pair, isPair := coupled[m.Net]
		if isPair && pair != "" {
			// Only if the partner is the very next trace across: a pair split
			// apart in the bus cannot be moved as one, and moving either half
			// alone would spoil the coupling it exists for.
			if i+1 < len(b.Members) && b.Members[i+1].Net == pair {
				n := b.Members[i+1]
				out = append(out, &unit{
					members: []BundleMember{m, n},
					lo:      m.Offset - m.Width/2,
					hi:      n.Offset + n.Width/2,
				})
				i++
				continue
			}
			return nil, fmt.Sprintf("%s and %s are a pair but are not neighbours here, so the bus cannot be spread safely",
				shortName(m.Net), shortName(pair))
		}
		out = append(out, &unit{
			members: []BundleMember{m},
			lo:      m.Offset - m.Width/2,
			hi:      m.Offset + m.Width/2,
		})
	}
	return out, ""
}

// unitNeed is the least of a unit's members' requirements.
//
// Moving a unit lengthens every trace in it by the same amount, so the smallest
// requirement is the one that overshoots first -- and overshooting means a trace
// becomes the longest in its group, which raises the target for everything else
// in it.
func unitNeed(u *unit, need map[string]float64) float64 {
	least := math.Inf(1)
	for _, m := range u.members {
		least = math.Min(least, need[m.Net])
	}
	if math.IsInf(least, 1) {
		return 0
	}
	return math.Max(0, least)
}

// allocate decides how far each unit travels and how much room that opens.
//
// The room opened beside a unit is how much further the unit beyond it went, so
// the outermost unit's travel is the budget every inner unit shares. Two things
// about how to spend it are genuine trades rather than matters of taste, so both
// are searched over and scored by the length each would actually deliver.
//
// How far to travel is the first. The second, and the one that is easy to get
// wrong, is how many traces to move at all. Spreading the travel across the
// whole bus looks fair and is usually worse than moving one trace: a gap holds
// no meander until it is wide enough for a mitred excursion, so several gaps
// below that threshold are worth nothing where one above it is worth a lot --
// and every trace that moves has its own straight run cut into by its steps and
// by the steps of everything outside it. On the demo board's byte lane 3,
// moving three traces opened three gaps too narrow to use and cost more run
// length than the steps added, while moving only the outermost opened 0.77 mm
// beside its neighbour and left every other trace whole.
func (t *Tuner) allocate(units []*unit, free, span, width float64, opt ExpandOptions) ([]*unit, float64, float64) {
	n := len(units)
	// A meander has to keep clear of whatever is at the end of the run it sits
	// on, the same margin the room profiler leaves, so a straight run is worth
	// this much less than its length.
	keep := math.Max(t.style.Gap*width, t.style.KeepClearOfPads)
	usable := func(l float64) float64 { return math.Max(0, l-2*keep) }
	cap_ := func(u *unit) float64 {
		return math.Min(u.need/rampGainPerMM, opt.MaxDisplacement)
	}
	// What the bus can already do with nothing moved: the inner units have no
	// room beside them, and the outermost has whatever clear space the bus
	// runs alongside. Every candidate is scored against this, so the figure
	// reported is what the spread adds and not what was there all along.
	base := 0.0
	for i, u := range units {
		room := 0.0
		if i == n-1 {
			room = free
		}
		u.flat = span
		u.gain0 = math.Min(u.need, t.capacity(usable(span), room-roomMargin, width))
		base += u.gain0
	}

	// The outermost unit's travel, which nothing inside it can exceed.
	outerMax := math.Min(math.Min(cap_(units[n-1]), free), (span-t.style.MinRunLength)/2)
	if outerMax < 0 {
		outerMax = 0
	}

	best := make([]*unit, 0)
	bestScore, bestOpened := -1.0, 0.0
	bestMet := -1
	for moving := 1; moving < n; moving++ {
		for step := 0; step <= 20; step++ {
			outer := outerMax * float64(step) / 20
			trial := make([]*unit, n)
			for i, u := range units {
				c := *u
				trial[i] = &c
			}
			// The outermost `moving` units travel, further the further out
			// they are; the rest stay where they are and take the room that
			// opens beside them. Unit 0 never moves: it sits against the
			// closed side, so moving it gains nothing while costing it length.
			//
			// Among those that do move, the travel is shared in proportion to
			// what they need, since the gap each gets is the difference
			// between its own travel and its neighbour's.
			first := n - moving
			var totalNeed float64
			for _, u := range trial[first : n-1] {
				totalNeed += u.need
			}
			acc := 0.0
			for i := 0; i < n-1; i++ {
				trial[i].disp = 0
				trial[i].opened = 0
				if i < first {
					continue
				}
				share := 0.0
				if totalNeed > 0 {
					share = outer * trial[i].need / totalNeed
				}
				trial[i].disp = math.Min(acc, cap_(trial[i]))
				acc += share
			}
			trial[n-1].disp = math.Min(outer, cap_(trial[n-1]))

			// The steps are staggered, so a unit's steps start past the steps
			// of everything further out, and what is left in the middle is its
			// straight run. Spreading therefore costs run length -- on a short
			// bus enough of it to be a bad trade, which is why the run each
			// unit ends up with goes into the score rather than being assumed
			// away.
			//
			// A candidate that does not fit is not rejected outright: the
			// search runs over increasing travel, so the largest one that does
			// fit is still found.
			fits, stagger := true, 0.0
			for i := n - 1; i >= 0; i-- {
				trial[i].flat = span
				if trial[i].disp > 1e-6 {
					trial[i].flat = span - 2*stagger - 2*trial[i].disp
					if trial[i].flat < t.style.MinRunLength {
						fits = false
						break
					}
					stagger += trial[i].disp
				}
			}
			if !fits {
				continue
			}
			// The room beside each unit: how much further the next one went,
			// plus whatever clear space is left beyond the outermost.
			for i := 0; i < n-1; i++ {
				trial[i].opened = math.Max(0, trial[i+1].disp-trial[i].disp)
			}
			trial[n-1].opened = math.Max(0, free-trial[n-1].disp)

			score, opened, met := 0.0, 0.0, 0
			for i, u := range trial {
				// Only the gaps between traces are room the spread created.
				// What lies beyond the outermost trace was already there.
				if i < n-1 {
					opened += u.opened
				}
				// What this unit would gain: the steps, plus a meander in the
				// room opened, capped at what it needed. A gap only runs as
				// far as the stretch the trace beyond it was displaced over,
				// so that bounds the meander as much as the unit's own run
				// does.
				along := u.flat
				if i < n-1 {
					along = math.Min(along, trial[i+1].flat)
				}
				u.gain = u.disp*rampGainPerMM + t.capacity(usable(along), u.opened-roomMargin, width)
				score += math.Min(u.need, u.gain)
				if u.need > 0 && u.gain >= u.need-1e-9 && u.gain0 < u.need-1e-9 {
					met++
				}
			}
			// Matching is pass or fail per trace, so a trace finished is worth
			// more than progress on two that stay out of tolerance -- which
			// millimetres alone cannot express, being indifferent between
			// 1.3 mm on one trace and 0.65 mm on each of two. Traces finished
			// first, then millimetres.
			if met > bestMet || (met == bestMet && score-base > bestScore) {
				bestMet, bestScore, bestOpened, best = met, score-base, opened, trial
			}
		}
	}
	return best, bestOpened, math.Max(0, bestScore)
}

// moveUnit steps one unit's traces aside and back, and reports the length that
// added.
func (t *Tuner) moveUnit(b *Bundle, u *unit, side, keep, width, stagger float64) (float64, error) {
	d := u.disp
	var total float64
	type edit struct {
		orig *board.Track
		pts  []geom.Pt
	}
	var edits []edit

	for _, m := range u.members {
		// Step aside inside this trace's own extent, so its endpoints do not
		// move and whatever it joined at each end it still joins.
		s0 := math.Max(b.U0+keep, m.U0)
		s1 := math.Min(b.U1-keep, m.U1)
		// The steps go inside the window everything further out has left,
		// which is what keeps two neighbours from stepping side by side.
		r0 := s0 + stagger
		r1 := s1 - stagger
		if r1-r0 < 2*d+t.style.MinRunLength {
			return 0, nil
		}
		v0 := m.Offset
		v1 := v0 + side*d

		// The waypoints have to be emitted in the direction the track was
		// drawn, not the direction the bus runs.
		//
		// Half the tracks in a real bus are drawn the other way round, and a
		// polyline that starts at the high end and then visits the low end
		// doubles back along the whole track. That is not a subtle error: it
		// added 4.44 mm where 0.41 mm was intended, and it looked like a
		// plausible-if-generous figure rather than an obvious fault.
		mid := []geom.Pt{
			b.point(r0, v0).Snap(),
			b.point(r0+d, v1).Snap(),
			b.point(r1-d, v1).Snap(),
			b.point(r1, v0).Snap(),
		}
		if m.Track.End.Sub(m.Track.Start).Dot(b.Dir) < 0 {
			for i, j := 0, len(mid)-1; i < j; i, j = i+1, j-1 {
				mid[i], mid[j] = mid[j], mid[i]
			}
		}
		pts := append([]geom.Pt{m.Track.Start}, mid...)
		pts = append(pts, m.Track.End)
		pts = dedupeConsecutive(pts)
		if len(pts) < 2 || !pts[0].Near(m.Track.Start) || !pts[len(pts)-1].Near(m.Track.End) {
			return 0, nil
		}
		// A step aside and back costs a known amount. Anything else means the
		// waypoints came out in the wrong order or the trace is not as parallel
		// to the bus as it looked, and either way the move is not what was
		// planned.
		var drawn float64
		for i := 0; i+1 < len(pts); i++ {
			drawn += pts[i].Dist(pts[i+1])
		}
		want := m.Track.Length(t.b.Stackup) + d*rampGainPerMM
		if math.Abs(drawn-want) > 0.01 {
			return 0, nil
		}
		edits = append(edits, edit{m.Track, pts})
	}

	// Check every trace in the unit before moving any of them: a pair has to
	// move together or not at all.
	for _, e := range edits {
		exempt := t.exemptFor(e.orig, e.orig.Net)
		for _, o := range u.members {
			if o.Track.UUID != "" {
				exempt[o.Track.UUID] = true
			}
		}
		base := t.chk.Worst(drc.Candidate{
			Shapes: e.orig.Shape(t.b.MaxError), Layer: e.orig.Layer, Net: e.orig.Net, Exempt: exempt,
		})
		cand := drc.Candidate{Layer: e.orig.Layer, Net: e.orig.Net, Exempt: exempt}
		for i := 0; i+1 < len(e.pts); i++ {
			cand.Shapes = append(cand.Shapes, geom.Capsule(e.pts[i], e.pts[i+1], width))
		}
		if ok, _ := drc.NoWorseThan(base, t.chk.Worst(cand)); !ok {
			return 0, nil
		}
	}

	for _, e := range edits {
		var before, after float64
		before = e.orig.Length(t.b.Stackup)
		replacement := make([]*board.Track, 0, len(e.pts)-1)
		for i := 0; i+1 < len(e.pts); i++ {
			after += e.pts[i].Dist(e.pts[i+1])
			replacement = append(replacement, &board.Track{
				Kind: board.KindSegment, Net: e.orig.Net,
				Start: e.pts[i], End: e.pts[i+1],
				Width: e.orig.Width, Layer: e.orig.Layer, UUID: newUUID(),
			})
		}
		if !t.b.ReplaceTrack(e.orig, replacement) {
			return 0, fmt.Errorf("tune: the trace being moved on %s is no longer on the board", e.orig.Net)
		}
		total += after - before
	}
	return total, nil
}

// shortName trims a hierarchical net path down to the part people say.
func shortName(net string) string {
	for i := len(net) - 1; i >= 0; i-- {
		if net[i] == '/' {
			return net[i+1:]
		}
	}
	return net
}

// unitName names a unit for a report: one trace, or a pair as "A/B".
func unitName(u *unit) string {
	names := make([]string, 0, len(u.members))
	for _, m := range u.members {
		names = append(names, shortName(m.Net))
	}
	return strings.Join(names, "/")
}
