package ddr

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

// Checks across groups.
//
// Every group matches its members to its own reference. Two requirements are
// about the references themselves: each byte lane's strobe against the clock
// that reaches the same device, and one device's byte lanes against another's.
// Neither is something a meander on one net fixes -- moving a strobe moves its
// whole lane -- so they are reported as checks, with the numbers, rather than
// turned into length to add.

// Check is one cross-group requirement and whether the board meets it.
type Check struct {
	// Kind is "strobe-to-clock" or "chip-delta".
	Kind string

	// Name says what was compared, e.g. "byte lane 2 strobe vs CLK at U5".
	Name string

	// ValueMM is the measured difference, signed where the sign means
	// something (strobe minus clock), and LimitMM the band it must sit inside.
	ValueMM float64
	LimitMM float64
	OK      bool

	// Detail is the arithmetic, so the figure can be checked.
	Detail string
}

// Checks runs the cross-group requirements.
func (p *Plan) Checks() []Check {
	var out []Check
	clockTo := p.clockTo
	lanes := p.lanesByDevice()

	// Each lane's strobe against the clock reaching the same device.
	if !p.Rules.StrobeToClock.Zero() {
		devices := make([]string, 0, len(lanes))
		for d := range lanes {
			devices = append(devices, d)
		}
		sort.Strings(devices)
		for _, d := range devices {
			clk, ok := clockTo[d]
			if !ok {
				continue
			}
			for _, g := range lanes[d] {
				limit := p.Rules.StrobeToClock.LimitMM(psPerMM(g))
				v := g.ReferenceLength - clk.length
				out = append(out, Check{
					Kind:    "strobe-to-clock",
					Name:    fmt.Sprintf("%s strobe vs CLK at %s", g.Name, d),
					ValueMM: v, LimitMM: limit, OK: math.Abs(v) <= limit,
					Detail: fmt.Sprintf("strobe mean %.3f mm, clock to %s %.3f mm (%s)",
						g.ReferenceLength, d, clk.length, clk.how),
				})
			}
		}
	}

	// One device's lanes against another's.
	if p.Rules.MaxChipDeltaMM > 0 && len(lanes) >= 2 {
		type chip struct {
			device string
			mean   float64
			names  []string
		}
		var chips []chip
		for d, gs := range lanes {
			var sum float64
			var names []string
			for _, g := range gs {
				sum += g.ReferenceLength
				names = append(names, strings.TrimPrefix(g.Name, "byte lane "))
			}
			chips = append(chips, chip{d, sum / float64(len(gs)), names})
		}
		sort.Slice(chips, func(i, j int) bool { return chips[i].device < chips[j].device })
		for i := 0; i < len(chips); i++ {
			for j := i + 1; j < len(chips); j++ {
				a, b := chips[i], chips[j]
				v := math.Abs(a.mean - b.mean)
				out = append(out, Check{
					Kind:    "chip-delta",
					Name:    fmt.Sprintf("%s bytes vs %s bytes", a.device, b.device),
					ValueMM: v, LimitMM: p.Rules.MaxChipDeltaMM, OK: v <= p.Rules.MaxChipDeltaMM,
					Detail: fmt.Sprintf("%s lanes %s average %.3f mm; %s lanes %s average %.3f mm",
						a.device, strings.Join(a.names, ", "), a.mean,
						b.device, strings.Join(b.names, ", "), b.mean),
				})
			}
		}
	}
	return out
}

type clockLength struct {
	length float64
	how    string
}

// clockToDevices is the clock pair's length from the controller to each
// device: the mean of its halves over the path from the controller's pad to
// that device's.
//
// The path, not the legs added up. A leg ends at a device's pad, so summing
// U3->U4 and U4->U5 walks the stub down to U4's ball and back up again, which
// the clock reaching U5 never does. ST's sheet measures the same way: the
// controller to the first memory, and the controller to the second.
func clockToDevices(c *Chain, meas map[string]*netlen.Measure, clocks []string) map[string]clockLength {
	out := map[string]clockLength{}
	if c == nil || len(c.Order) < 2 {
		return out
	}
	sort.Strings(clocks)
	for _, d := range c.Order[1:] {
		l := leg{from: c.Order[0], to: d}
		var sum float64
		var parts []string
		for _, net := range clocks {
			m := meas[net]
			if m == nil {
				continue
			}
			if pth, ok := legPath(m, l); ok {
				sum += pth.Length
				parts = append(parts, fmt.Sprintf("%s %.3f", leafName(net), pth.Length))
			}
		}
		if len(parts) == 0 {
			continue
		}
		out[d] = clockLength{sum / float64(len(parts)), fmt.Sprintf("%s: mean of %s", l.String(), strings.Join(parts, " and "))}
	}
	return out
}

func leafName(net string) string {
	if i := strings.LastIndexByte(net, '/'); i >= 0 {
		return net[i+1:]
	}
	return net
}

// lanesByDevice groups the byte lanes by the memory device their strobe lands
// on: whichever end of the strobe's route is not the controller.
func (p *Plan) lanesByDevice() map[string][]*Group {
	out := map[string][]*Group{}
	controller := ""
	if p.Interface != nil {
		controller = p.Interface.Controller
	}
	for _, g := range p.Groups {
		if g.Kind != ByteLane || g.ReferenceLength <= 0 || len(g.ReferenceMembers) == 0 {
			continue
		}
		m := g.ReferenceMembers[0]
		device := refOf(m.To)
		if device == controller {
			device = refOf(m.From)
		}
		if device == "" || device == controller {
			continue
		}
		out[device] = append(out[device], g)
	}
	return out
}

func psPerMM(g *Group) float64 {
	for _, m := range g.Members {
		if m.Routed && m.Length > 0 {
			return m.Delay / m.Length
		}
	}
	return 0
}
