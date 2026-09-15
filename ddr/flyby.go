package ddr

import (
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
	"github.com/embeddedci-com/pcb-autorouter/route"
)

// MissingHops turns the gaps in the fly-by chain into connections to make.
//
// Only the hops that are actually missing, and only for nets that have a pad at
// both ends of one: a hop nothing is missing needs no copper, and a net without
// a pad on a device is not part of that hop. The byte lanes are left out
// entirely -- DQ, DQS and DM are point to point between the controller and one
// device, not through the chain, and asking for a hop of them would be asking
// for copper the topology does not want.
//
// This lives here rather than in the router because which connection is missing
// is a question about the interface, not about the search: the router is told
// pad to pad and has no idea what a fly-by chain is.
func MissingHops(b *board.Board, iface *Interface, plan *Plan) []route.Request {
	if plan == nil || plan.Chain == nil {
		return nil
	}
	engine := netlen.New(b)
	known := map[string]bool{iface.Controller: true}
	for _, d := range iface.Devices {
		known[d] = true
	}

	var out []route.Request
	for _, hop := range plan.Chain.Hops {
		if hop.Routed() {
			continue
		}
		for net, sig := range iface.Signals {
			if sig.Role == RoleData || sig.Role == RoleStrobe || sig.Role == RoleDataMask {
				continue // the byte lanes are point to point, not fly-by
			}
			from := PadOn(b, net, hop.From, known)
			to := PadOn(b, net, hop.To, known)
			if from == "" || to == "" {
				continue
			}
			if m := engine.Measure(net); m != nil && m.PathBetween(from, to).Found {
				continue // this net already makes this hop
			}
			out = append(out, route.Request{Net: net, From: from, To: to})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].Net < out[j].Net
	})
	return out
}

// PadOn finds a net's pad on a named component, or on its terminator when the
// name is the end of the chain.
//
// "termination" is not a reference on the board: it is whatever the bus ends
// at, which is a resistor pack nobody named in advance. So the end of the chain
// is any pad on the net belonging to something that is neither the controller
// nor a memory device.
func PadOn(b *board.Board, net, ref string, known map[string]bool) string {
	for _, p := range b.PadsOfNet(net) {
		if ref == "termination" {
			if !known[p.Ref] {
				return p.ID()
			}
			continue
		}
		if p.Ref == ref {
			return p.ID()
		}
	}
	return ""
}
