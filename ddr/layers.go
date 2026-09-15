package ddr

import (
	"fmt"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// Layer use.
//
// ST's AN5724 says where DDR signals go: data lines (DQ, DQM, DQS) on the top
// layer only, and address, command and clock distributed on the bottom layer,
// with the top layer kept for the short connections to each memory and for
// crossing the bus. Boards are built other ways on purpose -- other controllers
// and other guides differ, and ST exempts its smallest packages from the
// bottom-layer rule -- so these are observations, not failures.

// LayerFinding is one layer rule and the nets that do not follow it.
type LayerFinding struct {
	// Rule is "data-top" or "address-bottom".
	Rule string
	// Expect says what the guide expects, in words.
	Expect string
	Nets   []NetLayers
}

// NetLayers is the copper of one net's measured route, per layer.
type NetLayers struct {
	Net     string
	ByLayer map[string]float64
	// Share is the share of the copper on the expected layer, 0 to 1.
	Share float64
}

const (
	// dataOffTopMM is how much copper a data line may have off the top layer
	// before it is reported: a fan-out under a BGA can need a short hop.
	dataOffTopMM = 1.0
	// addressBottomShare is the least share of an address or command line's
	// route expected on the bottom layer; the rest is stubs and crossings.
	addressBottomShare = 0.5
)

// LayerFindings checks the layer rules on the measured routes. Only rules
// some net does not follow are returned.
func (p *Plan) LayerFindings(b *board.Board) []LayerFinding {
	if len(b.CopperLayers) < 2 {
		return nil
	}
	top, bottom := b.CopperLayers[0], b.CopperLayers[len(b.CopperLayers)-1]

	// A net's route is the union of its members' tracks across groups: an
	// address line to the second memory is split into the part to the first
	// memory and the part beyond it.
	routes := map[string]map[string]bool{}
	kind := map[string]GroupKind{}
	for _, g := range p.Groups {
		for _, m := range g.Members {
			if !m.Routed {
				continue
			}
			if routes[m.Net] == nil {
				routes[m.Net] = map[string]bool{}
			}
			for _, id := range m.PathTracks {
				routes[m.Net][id] = true
			}
			kind[m.Net] = g.Kind
		}
	}

	data := LayerFinding{Rule: "data-top",
		Expect: fmt.Sprintf("data lines (DQ, DQM, DQS) on the top layer (%s) only", top)}
	addr := LayerFinding{Rule: "address-bottom",
		Expect: fmt.Sprintf("address, command and clock routed mainly on the bottom layer (%s), "+
			"with the top layer only for the connections to each memory and for crossings", bottom)}

	nets := make([]string, 0, len(routes))
	for n := range routes {
		nets = append(nets, n)
	}
	sort.Strings(nets)
	for _, net := range nets {
		by := map[string]float64{}
		total := 0.0
		for _, t := range b.TracksOfNet(net) {
			if t.Kind == board.KindVia || !routes[net][t.UUID] {
				continue
			}
			l := t.Length(b.Stackup)
			by[t.Layer] += l
			total += l
		}
		if total <= 0 {
			continue
		}
		switch kind[net] {
		case ByteLane:
			if total-by[top] > dataOffTopMM {
				data.Nets = append(data.Nets, NetLayers{Net: net, ByLayer: by, Share: by[top] / total})
			}
		case AddressCommand:
			if share := by[bottom] / total; share < addressBottomShare {
				addr.Nets = append(addr.Nets, NetLayers{Net: net, ByLayer: by, Share: share})
			}
		}
	}

	var out []LayerFinding
	for _, f := range []LayerFinding{data, addr} {
		if len(f.Nets) > 0 {
			sort.Slice(f.Nets, func(i, j int) bool { return f.Nets[i].Share < f.Nets[j].Share })
			out = append(out, f)
		}
	}
	return out
}
