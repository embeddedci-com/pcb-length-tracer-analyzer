package netlen

import (
	"regexp"
	"sort"
	"strings"
)

// Signals that pass through a series part.
//
// A series resistor, ferrite or coupling capacitor splits one signal into two
// nets, and KiCad names the far side something nobody chose: an RGMII clock is
// "ETH1.RX_CLK" up to the resistor and "Net-(U9-RXD0_RXDLY)" after it. Measured
// per net, such a signal reads as only the copper on the near side, and by a
// different amount on each line of a bus, which is exactly the skew the
// matching is supposed to find.
//
// So a length here is the sum of the segments. The part itself is not measured:
// it is a lumped element a millimetre long, and every line of a bus carries the
// same one placed the same way, so what it contributes is common to the group
// and cancels out of the skew. The copper on both sides does not cancel.
//
// What must not happen is following a decoupling capacitor into a power plane.
// The guard is deliberately dumb and strict: two pads, both on real nets, and
// the far net must be small and not named like a rail.

// seriesPart matches the reference of a part a signal can run through: a
// resistor, an inductor or ferrite bead, or a coupling capacitor. Two pads is
// not enough on its own -- a two-pin connector has two pads, and so does a
// small chip, and walking through one of those would join two signals that
// have nothing to do with each other.
var seriesPart = regexp.MustCompile(`^(R|L|FB|C)[0-9]`)

// powerNet matches the names boards give rails. Case is normalised first.
var powerNet = regexp.MustCompile(`^(GND|AGND|DGND|PGND|VSS[A-Z0-9_]*|VCC[A-Z0-9_]*|VDD[A-Z0-9_]*|VBAT|VBUS|VREF[A-Z0-9_]*|VTT|[+-]?[0-9]+V[0-9]*|[0-9]+V[0-9]+)$`)

// diffSuffix are the endings that make two nets the halves of one pair, in the
// order they pair up: _P with _N, + with -, _T with _C.
var diffSuffix = [][2]string{{"_P", "_N"}, {"+", "-"}, {"_T", "_C"}, {"P", "N"}}

// sameDiffPair reports whether two nets are the two halves of one differential
// pair.
//
// A resistor across a pair is a terminator, not something a signal runs
// through: the 100 ohms bridging DDR_CLK_P and DDR_CLK_N sits at the far end of
// both, and walking it reports a clock twice its real length. A series part has
// a different signal on each side; a terminator has the same signal twice.
func sameDiffPair(a, b string) bool {
	x, y := strings.ToUpper(leafOf(a)), strings.ToUpper(leafOf(b))
	if x == y {
		return false
	}
	for _, s := range diffSuffix {
		for _, pair := range [][2]string{{s[0], s[1]}, {s[1], s[0]}} {
			if strings.HasSuffix(x, pair[0]) && strings.HasSuffix(y, pair[1]) &&
				strings.TrimSuffix(x, pair[0]) == strings.TrimSuffix(y, pair[1]) {
				return true
			}
		}
	}
	return false
}

func leafOf(net string) string {
	if i := strings.LastIndexByte(net, '/'); i >= 0 {
		return net[i+1:]
	}
	return net
}

// maxSeriesPads is how many pads a net may have and still be taken for one
// signal's segment. A rail has dozens; a segment between a driver and a series
// part has two, or a few more where the schematic also taps it.
const maxSeriesPads = 6

// maxSeriesHops bounds the walk, so a board that chains parts in a way this did
// not anticipate cannot turn into a tour of the whole net list.
const maxSeriesHops = 4

func isPowerName(net string) bool {
	leaf := leafOf(net)
	if i := strings.LastIndexByte(leaf, '.'); i >= 0 {
		leaf = leaf[i+1:]
	}
	return powerNet.MatchString(strings.ToUpper(leaf))
}

// SeriesLink is one hop: the part crossed and the net on its far side.
type SeriesLink struct {
	Through string // the component's reference, e.g. "R80"
	Net     string
}

// seriesLinks are the hops out of one net, sorted for a stable answer.
func (e *Engine) seriesLinks(net string) []SeriesLink {
	var out []SeriesLink
	for _, f := range e.b.Footprints {
		if len(f.Pads) != 2 || !seriesPart.MatchString(strings.ToUpper(f.Ref)) {
			continue
		}
		a, b := f.Pads[0].Net, f.Pads[1].Net
		if a == "" || b == "" || a == b {
			continue
		}
		far := ""
		switch net {
		case a:
			far = b
		case b:
			far = a
		default:
			continue
		}
		if isPowerName(far) || len(e.b.PadsOfNet(far)) > maxSeriesPads || sameDiffPair(net, far) {
			continue
		}
		out = append(out, SeriesLink{Through: f.Ref, Net: far})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Net < out[j].Net })
	return out
}

// Joined is one signal measured across the parts it passes through.
type Joined struct {
	// Net is the net asked about, the one the interface knows the signal by.
	Net string

	// Segments are every net the signal runs on, starting with Net.
	Segments []string

	// Through are the parts crossed, in the order they were reached.
	Through []string

	// LengthMM is the sum of each segment's own longest route. The parts
	// themselves are not counted; see the note at the top of this file.
	LengthMM float64

	// Found is whether every segment had a route to measure, and Complete
	// whether every segment's pads are joined by copper.
	Found, Complete bool
}

// Split reports whether the signal really does pass through a part. A signal
// that does not is still returned, so a caller can use Joined for everything.
func (j *Joined) Split() bool { return len(j.Segments) > 1 }

// Joined measures a net together with whatever continues it through series
// parts.
func (e *Engine) Joined(net string) *Joined {
	j := &Joined{Net: net, Found: true, Complete: true}
	seen := map[string]bool{net: true}
	queue := []string{net}

	for hop := 0; len(queue) > 0 && hop <= maxSeriesHops; hop++ {
		var next []string
		for _, n := range queue {
			j.Segments = append(j.Segments, n)
			m := e.Measure(n)
			if m == nil || !m.Longest.Found {
				j.Found = false
			} else {
				j.LengthMM += m.Longest.Length
			}
			if m == nil || !m.Complete {
				j.Complete = false
			}
			for _, l := range e.seriesLinks(n) {
				if seen[l.Net] {
					continue
				}
				seen[l.Net] = true
				j.Through = append(j.Through, l.Through)
				next = append(next, l.Net)
			}
		}
		queue = next
	}
	return j
}
