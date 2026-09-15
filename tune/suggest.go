package tune

import (
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Proposing the areas rather than making the user invent them.
//
// A blank board and "draw where copper may go" is a worse question than it
// looks. The answer depends on where the nets that need length actually run and
// where there happens to be space beside them, and both of those are things the
// tool has already measured and the user would have to hunt for. Starting from
// nothing means drawing a rectangle, waiting to be told it holds 0.000 mm, and
// trying again somewhere else.
//
// So it proposes. The stretches where a net that needs length has room beside
// it are exactly what the room profiler already finds; clustering them into
// rectangles turns them into somewhere to start. What comes out is not a
// recommendation about the board -- the tool still has no idea whether a region
// is under a connector or over a plane split -- it is a statement of where the
// length could come from, for the user to expand, move or delete.

// Spot is one stretch of track with room beside it, as a region.
type Spot struct {
	Net string

	// Box is the stretch plus the room beside it, which is the area a meander
	// there would actually occupy.
	Box geom.Rect

	// GainMM is the length a meander in it could add.
	GainMM float64
}

// Spots returns the places a net could be lengthened, ignoring any areas
// already set: the question is what the board offers, not what is allowed
// today.
func (t *Tuner) Spots(net string, on map[string]bool) []Spot {
	width := t.trackWidth(net)
	if width <= 0 {
		return nil
	}
	was := t.areas
	t.areas = nil
	defer func() { t.areas = was }()

	var out []Spot
	for _, r := range t.findRuns(net, width, on) {
		gain := t.capacity(r.length, r.amp, width)
		if gain <= 0 {
			continue
		}
		// The stretch, widened by the excursion it would make, which is the
		// copper a meander there would occupy rather than the centreline.
		box := geom.RectFromPts(r.from, r.to)
		side := r.side.Mul(r.amp + width/2)
		box = box.Include(r.from.Add(side)).Include(r.to.Add(side))
		out = append(out, Spot{Net: net, Box: box.Grow(width / 2), GainMM: gain})
	}
	return out
}

// Suggestion is a proposed area and what it would be worth.
type Suggestion struct {
	Area geom.Rect

	// GainMM is the length the spots inside it could supply, and Nets is how
	// many nets contribute.
	GainMM float64
	Nets   int
}

// Suggest clusters spots into a handful of areas worth drawing.
//
// Boxes that touch, or nearly, become one: a bus's worth of parallel stretches
// is one place on the board, and offering twenty slivers would be a worse
// answer than offering none. The margin is what "nearly" means, and it is
// generous on purpose -- an area a user is going to adjust anyway is better a
// little too big than in four pieces.
func Suggest(spots []Spot, margin float64, max int) []Suggestion {
	if len(spots) == 0 {
		return nil
	}
	type cluster struct {
		box  geom.Rect
		gain float64
		nets map[string]bool
	}
	var cs []*cluster
	for _, s := range spots {
		grown := s.Box.Grow(margin)
		var hit *cluster
		for _, c := range cs {
			if c.box.Overlaps(grown) {
				hit = c
				break
			}
		}
		if hit == nil {
			cs = append(cs, &cluster{box: s.Box, gain: s.GainMM, nets: map[string]bool{s.Net: true}})
			continue
		}
		hit.box = hit.box.Union(s.Box)
		hit.gain += s.GainMM
		hit.nets[s.Net] = true
	}

	// Clusters grow as they absorb spots, so two that were apart may now
	// overlap. Merge until nothing changes, or the result depends on the order
	// the spots happened to arrive in.
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(cs) && !merged; i++ {
			for j := i + 1; j < len(cs); j++ {
				if !cs[i].box.Grow(margin).Overlaps(cs[j].box) {
					continue
				}
				cs[i].box = cs[i].box.Union(cs[j].box)
				cs[i].gain += cs[j].gain
				for n := range cs[j].nets {
					cs[i].nets[n] = true
				}
				cs = append(cs[:j], cs[j+1:]...)
				merged = true
				break
			}
		}
	}

	out := make([]Suggestion, 0, len(cs))
	for _, c := range cs {
		out = append(out, Suggestion{Area: c.box, GainMM: c.gain, Nets: len(c.nets)})
	}
	// Worth the most first, so a user taking only the top one takes the best.
	sort.Slice(out, func(i, j int) bool {
		if math.Abs(out[i].GainMM-out[j].GainMM) > 1e-9 {
			return out[i].GainMM > out[j].GainMM
		}
		return out[i].Nets > out[j].Nets
	})
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}
