package ddr

import (
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/proto"
)

// Scope decides which nets the DDR analysis works on: the prefix given, or
// one guessed from hierarchical names, or -- for a bus with flat names such as
// "DDR_DQ0", which has no sheet path to be a prefix -- the nets the interface
// detector recognised as DDR. The last is what the interface list shows, so
// the two can never disagree about whether the board has DDR on it. Both
// results empty means no DDR.
func Scope(b *board.Board, given string) (prefix string, nets []string) {
	if given != "" {
		return given, nil
	}
	if g := GuessPrefix(b); g != "" {
		return g, nil
	}
	for _, i := range proto.Detect(b) {
		if i.Kind == proto.DDR && len(i.Nets) > len(nets) {
			nets = i.Nets
		}
	}
	return "", nets
}

// GuessPrefix looks for the sheet path that most DDR-looking nets share, so the
// common case needs no parameter. Empty when there is no such path.
func GuessPrefix(b *board.Board) string {
	counts := map[string]int{}
	for _, net := range b.Nets() {
		i := strings.LastIndexByte(net, '/')
		if i < 0 {
			continue
		}
		leaf := strings.ToUpper(net[i+1:])
		if !strings.Contains(leaf, "DDR") && !strings.Contains(leaf, "DQ") {
			continue
		}
		counts[net[:i+1]]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bestN := "", 0
	for _, k := range keys {
		if counts[k] > bestN {
			best, bestN = k, counts[k]
		}
	}
	if bestN < 8 {
		return ""
	}
	return best
}
