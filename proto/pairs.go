// Package proto recognises the interfaces on a board and says what each one
// needs matched.
//
// The length matcher started as a DDR tool, and DDR is the hardest case: a
// topology with legs, byte lanes tied to their own strobe, and a clock the
// whole address bus is judged against. Everything else -- Ethernet, USB, PCIe,
// MIPI, SD -- is the same measurement over a simpler shape, so the engine
// underneath was always general. What was DDR-specific was knowing what to
// measure against what.
//
// That is what this package supplies. It reads a board, says which interfaces
// are on it and how it recognised them, and describes each one as groups of
// nets that have to match a reference. Nothing here measures or changes
// anything; it decides what the question is, and the question is most of the
// difficulty.
//
// Recognition is by net name, because a net name is what a designer actually
// gives us and it is the only thing on a board that says "this is RGMII". That
// makes it a reading rather than a proof, so every interface carries the
// evidence it was recognised by, and the user picks which ones to act on.
package proto

import (
	"regexp"
	"sort"
	"strings"
)

// Pair is the two halves of a differential pair.
type Pair struct {
	// Base is the name without the polarity suffix, e.g. "PCIE.RX1".
	Base string
	P, N string
}

// polaritySuffixes are the ways a schematic spells the two halves, positive
// first. The list is ordered: "+"/"-" is tried before "P"/"N" so that a net
// ending in "P" is not mistaken for the positive half of something when the
// board actually uses plus and minus.
var polaritySuffixes = [][2]string{
	{"+", "-"},
	{"_P", "_N"},
	{"_T", "_C"},
	{"_DP", "_DM"},
	{"P", "N"},
}

// FindPairs groups nets into differential pairs.
//
// The rule that keeps this honest is that both halves have to exist. A trailing
// N is otherwise indistinguishable from an active-low suffix -- DDR's RESETN,
// CASN and ACTN are single-ended nets whose names end the way a complement
// does -- and asking for the partner settles it without a list of exceptions:
// there is no RESETP on any board.
func FindPairs(nets []string) []Pair {
	have := make(map[string]bool, len(nets))
	for _, n := range nets {
		have[n] = true
	}
	seen := map[string]bool{}
	var out []Pair
	for _, n := range nets {
		if seen[n] {
			continue
		}
		for _, suffix := range polaritySuffixes {
			pos, neg := suffix[0], suffix[1]
			if !strings.HasSuffix(n, pos) {
				continue
			}
			base := strings.TrimSuffix(n, pos)
			if base == "" || strings.HasSuffix(base, "/") {
				continue
			}
			partner := base + neg
			if !have[partner] || seen[partner] {
				continue
			}
			seen[n], seen[partner] = true, true
			out = append(out, Pair{Base: base, P: n, N: partner})
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Base < out[j].Base })
	return out
}

// leaf is the part of a hierarchical net name people say out loud.
func leaf(net string) string {
	if i := strings.LastIndexByte(net, '/'); i >= 0 {
		return net[i+1:]
	}
	return net
}

// scope is the hierarchical path a net sits in, e.g. "/ethernet/". Nets in one
// sheet are usually one interface, which is a useful second opinion when the
// names alone are ambiguous.
func scope(net string) string {
	if i := strings.LastIndexByte(net, '/'); i > 0 {
		return net[:i+1]
	}
	return ""
}

// instanceRE pulls the interface instance out of a leaf name: the "ETH1" of
// "ETH1.TXD0", the "PCIE" of "PCIE.RX1+". A board with two of something names
// them apart, and matching one instance's nets against another's would be
// nonsense.
//
// Two characters minimum. A single letter before a separator is a prefix
// somebody put there for their own reasons -- the "P" of "P_USB_D+" means the
// pod, not a USB controller called P -- and treating it as an instance names
// the interface "USB (P)", which helps nobody.
var instanceRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*?[0-9]*)[._]`)

const minInstance = 2

func instance(net string) string {
	m := instanceRE.FindStringSubmatch(leaf(net))
	if m == nil || len(m[1]) < minInstance {
		return ""
	}
	return strings.ToUpper(m[1])
}

// signal is the leaf name with any instance prefix removed and the case
// normalised: "ETH1.TXD0" becomes "TXD0".
func signal(net string) string {
	s := leaf(net)
	if m := instanceRE.FindStringSubmatch(s); m != nil {
		s = s[len(m[0]):]
	}
	return strings.ToUpper(s)
}

// Leaf is the part of a hierarchical net name people say out loud.
func Leaf(net string) string { return leaf(net) }
