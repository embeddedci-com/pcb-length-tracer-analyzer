package ddr

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

// GroupKind is the sort of matching a group requires.
type GroupKind int

const (
	// ByteLane matches a lane's data bits, its data mask and its strobe pair
	// to one another.
	ByteLane GroupKind = iota
	// AddressCommand matches the address and command lines to the clock.
	AddressCommand
)

func (k GroupKind) String() string {
	if k == ByteLane {
		return "byte-lane"
	}
	return "address-command"
}

// Member is one net in a group, as measured.
type Member struct {
	Net  string
	Role Role

	// Pair is the complement net when this member is half a differential
	// pair.
	Pair string

	// From and To are the pads the measurement runs between.
	From, To string

	// Length and Delay are the measured route.
	Length, Delay float64

	// Routed is false when no copper joins the two pads, in which case Length
	// and Delay mean nothing and the net cannot be tuned.
	Routed bool

	// Need is how much length has to be added to reach the group target. It is
	// never negative: meandering can only lengthen a track.
	Need float64

	// Excess is how far past the tolerance band a member is on the long side,
	// zero otherwise. The target is the reference's and is not moved to suit
	// the longest member, so a member longer than the clock is not something a
	// meander can fix: it has to be routed shorter, or the reference longer.
	Excess float64

	// Reference marks a member that is part of the reference itself -- a half
	// of the strobe or clock pair. Its offset against the pair's mean is shown,
	// but it is not tuned towards it: moving a half of the reference moves the
	// target. The pair is matched to itself instead.
	Reference bool

	// NeedDelay is the same requirement in picoseconds.
	NeedDelay float64

	// Deviation is the member's current distance from the target, negative
	// when short.
	Deviation float64

	// InTolerance reports whether the member already satisfies the group's
	// tolerance against the target.
	InTolerance bool

	// Headroom is the most length that could be folded into this member's route
	// without breaking a rule, when the caller has measured it. Zero means it
	// was not measured.
	//
	// It separates the two kinds of shortfall. A member short of space might be
	// helped by opening some; a member asking for more length than its route
	// could ever hold needs rerouting, and saying so is more use than trying.
	Headroom float64

	// PathTracks are the uuids of the copper the measured route runs over.
	//
	// Tuning must add its length here and nowhere else. A net can have copper
	// that is on no route -- a stub, or a whole island stranded because a leg
	// was never finished -- and lengthening that adds copper without changing
	// the path being matched. On this board the clock pair has exactly that
	// shape, and a tuner that ignored this happily reported 2.4 mm added to
	// DDR_CLK_N while the leg it was matched on did not move at all.
	PathTracks []string
}

// Group is a set of nets that have to match, on one leg of the topology.
type Group struct {
	Kind GroupKind

	// Name identifies the group in reports, e.g. "byte lane 2" or
	// "address/command U3->U4".
	Name string

	// Lane is the byte lane, or -1.
	Lane int

	// Leg names the pad-to-pad span measured, as "U3->U4". Every member is
	// measured over the same span so the comparison means something. For the
	// address and command groups it runs from the controller to one device.
	Leg string

	// Reference is the net the group is matched against: the strobe pair for a
	// byte lane, the clock pair for the address and command group. Empty when
	// no reference was routed and the group had to fall back to its own
	// longest member.
	Reference string

	// ReferenceLength is the reference's measured length, before any tuning:
	// the mean of the two halves of the pair, because the receiver sees the
	// crossing point of the pair, which sits between them.
	ReferenceLength float64

	// ReferenceMembers are the halves of the reference with their own lengths,
	// so a reader can see what the mean was taken over.
	ReferenceMembers []Member

	// Target is the length every member is to be brought to: the reference
	// length, times any clock offset. Always the reference's, never raised to
	// clear a member that is longer -- that member is reported as too long.
	Target float64

	// TargetDelay is the same target as a delay.
	TargetDelay float64

	// TargetFromReference is the target the reference implied. Equal to Target
	// whenever there is a routed reference; it differs only for a group with
	// no reference, which falls back to its own longest member.
	TargetFromReference float64

	// Tolerance is the matching requirement for this group.
	Tolerance Tolerance

	// ToleranceMM is the tolerance resolved to a length at this group's
	// propagation rate.
	ToleranceMM float64

	Members []Member

	// Unroutable lists members that have no complete route and so cannot be
	// measured or tuned.
	Unroutable []string

	// Notes records why the group looks the way it does.
	Notes []string
}

// SpreadBefore is the length difference between the longest and shortest
// routed member, including the reference.
func (g *Group) SpreadBefore() float64 {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, m := range g.Members {
		if !m.Routed {
			continue
		}
		lo = math.Min(lo, m.Length)
		hi = math.Max(hi, m.Length)
	}
	if math.IsInf(lo, 1) {
		return 0
	}
	return hi - lo
}

// TotalNeed is how much copper length the group's tuning would add in total.
func (g *Group) TotalNeed() float64 {
	var t float64
	for _, m := range g.Members {
		t += m.Need
	}
	return t
}

// OutOfTolerance counts members that do not yet meet the group's tolerance.
func (g *Group) OutOfTolerance() int {
	n := 0
	for _, m := range g.Members {
		if m.Routed && !m.InTolerance {
			n++
		}
	}
	return n
}

// Plan is the full matching plan for an interface.
type Plan struct {
	Interface *Interface
	Rules     Rules
	Groups    []*Group

	// Skipped lists nets that belong to the interface but are in no group,
	// with the reason.
	Skipped map[string]string

	// Chain is the fly-by topology as the board has it: the order the address,
	// command, control and clock nets visit the devices, which hops exist, and
	// how that order was established.
	//
	// Nil when the interface has no fly-by nets at all.
	Chain *Chain

	// clockTo is the clock pair's length from the controller to each device
	// of the chain, for the checks across groups.
	clockTo map[string]clockLength
}

// TotalNeed is how much length the whole plan would add.
func (p *Plan) TotalNeed() float64 {
	var t float64
	for _, g := range p.Groups {
		t += g.TotalNeed()
	}
	return t
}

// Actionable returns the groups that have something to do.
func (p *Plan) Actionable() []*Group {
	var out []*Group
	for _, g := range p.Groups {
		if g.OutOfTolerance() > 0 {
			out = append(out, g)
		}
	}
	return out
}

// Measurer is the length engine the plan needs. netlen.Engine satisfies it.
type Measurer interface {
	Measure(net string) *netlen.Measure
}

// BuildPlan measures every net in the interface and works out what has to
// change.
//
// Two decisions in here are worth knowing about.
//
// First, a group's target is its reference: the strobe pair's mean for a byte
// lane, the clock pair's for address and command. It is not moved to suit the
// longest member. A meander adds copper and cannot shorten a track, so a member
// longer than the reference is reported as too long -- a reroute -- rather than
// quietly becoming the thing the rest of the group is matched to.
//
// Second, a fly-by group is planned per leg. The address and command lines on
// this kind of board reach two devices in series, and matching their total
// length while their first legs differ still fails write levelling. Each leg is
// therefore a group of its own, and a net appears once per leg it is routed on.
func BuildPlan(iface *Interface, m Measurer, r Rules) (*Plan, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	p := &Plan{Interface: iface, Rules: r, Skipped: map[string]string{}}

	meas := map[string]*netlen.Measure{}
	for net := range iface.Signals {
		meas[net] = m.Measure(net)
	}

	// Byte lanes are point to point: the controller to whichever device carries
	// the lane.
	for lane := 0; lane < iface.Lanes; lane++ {
		nets := laneNets(iface, lane)
		if len(nets) == 0 {
			continue
		}
		g := buildGroup(iface, meas, r, ByteLane, lane, nets)
		if g != nil {
			p.Groups = append(p.Groups, g)
		}
	}

	// The address and command group is planned once per leg of the fly-by
	// chain, in the order the chain runs.
	acNets := addrCmdNets(iface, r)
	clkNets := iface.NetsWithRole(RoleClock)
	if len(acNets) > 0 {
		// Work the chain out before planning against it: the order decides
		// which span each hop's lengths are compared over, so it has to be the
		// board's order and not the designators'.
		p.Chain = chainOf(iface, m, append(append([]string{}, clkNets...), acNets...))
		p.clockTo = clockToDevices(p.Chain, meas, clkNets)
		// One group per device, each measured from the controller, the way
		// ST's length equalization sheet does: at every memory, each address
		// and command line against the clock that reaches the same memory.
		upstream := map[string]float64{}
		var prev *leg
		for _, l := range reachesOf(iface, p.Chain) {
			g := buildFlyByGroup(iface, meas, r, l, prev, upstream, append(append([]string{}, clkNets...), acNets...))
			if g != nil {
				p.Groups = append(p.Groups, g)
				for _, m := range g.Members {
					upstream[m.Net] += m.Need
				}
			}
			prev = &l
		}
	}

	// Record anything left out.
	inGroup := map[string]bool{}
	for _, g := range p.Groups {
		for _, mm := range g.Members {
			inGroup[mm.Net] = true
		}
	}
	for net, s := range iface.Signals {
		if inGroup[net] {
			continue
		}
		switch {
		case s.Role == RoleControl && !r.IncludeControl:
			p.Skipped[net] = "control line, not length matched (set IncludeControl to change that)"
		case s.Lane < 0 && (s.Role == RoleData || s.Role == RoleStrobe || s.Role == RoleDataMask):
			p.Skipped[net] = "could not be assigned to a byte lane"
		default:
			p.Skipped[net] = "no group applies"
		}
	}
	return p, nil
}

func laneNets(iface *Interface, lane int) []string {
	var out []string
	for net, s := range iface.Signals {
		if s.Lane != lane {
			continue
		}
		switch s.Role {
		case RoleData, RoleDataMask, RoleStrobe:
			out = append(out, net)
		}
	}
	sort.Slice(out, func(a, b int) bool { return netLess(iface, out[a], out[b]) })
	return out
}

func addrCmdNets(iface *Interface, r Rules) []string {
	roles := []Role{RoleAddress, RoleCommand}
	if r.IncludeControl {
		roles = append(roles, RoleControl)
	}
	return iface.NetsWithRole(roles...)
}

func netLess(iface *Interface, a, b string) bool {
	sa, sb := iface.Signals[a], iface.Signals[b]
	if sa.Role != sb.Role {
		return sa.Role < sb.Role
	}
	if sa.Index != sb.Index {
		return sa.Index < sb.Index
	}
	return a < b
}

// Hop is one span of the fly-by chain and whether the board has it.
//
// DDR3 and DDR4 address, command, control and clock are fly-by: one net leaves
// the controller, reaches the first device, carries on to the next, and ends in
// a termination resistor. It is not a style -- the topology is what makes write
// levelling work and what keeps the stub reflections a T would create off the
// bus -- so a board missing a hop has not routed its address bus yet, however
// complete the copper that is there looks.
type Hop struct {
	From, To string

	// Nets is how many of the chain's nets have copper joining these two, and
	// Of is how many were looked at.
	Nets, Of int
}

// Routed reports whether every net makes this hop.
func (h Hop) Routed() bool { return h.Of > 0 && h.Nets == h.Of }

// Chain describes the fly-by topology as the board actually has it.
type Chain struct {
	// Order is the controller followed by the devices, outward.
	Order []string

	// Hops are the spans between them, in order, plus the last device to its
	// termination.
	Hops []Hop

	// OrderFrom says how the order was established, for the report.
	OrderFrom string

	// Note records a disagreement worth acting on: the copper says the chain
	// runs in an order the placement did not suggest, or a hop is routed while
	// an earlier one is not, which no fly-by chain can be.
	Note string
}

// FirstGap is the first hop the board does not have, or nil when the chain is
// complete.
func (c *Chain) FirstGap() *Hop {
	for i := range c.Hops {
		if !c.Hops[i].Routed() {
			return &c.Hops[i]
		}
	}
	return nil
}

// leg is one span of the topology, named by the two component references it
// runs between.
type leg struct {
	from, to string
}

func (l leg) String() string { return l.from + "->" + l.to }

// chainOf works out the fly-by chain from the routed copper, and checks it
// against the order the placement suggested.
//
// The copper is the authority where it exists: whichever device the
// controller's own copper reaches is the first in the chain, whatever the
// placement or the designators say. Where the copper does not exist -- which on
// a board still being routed is most of it -- the order stays as placement gave
// it, and the note says so, because every per-hop length in the plan below
// rests on it.
func chainOf(iface *Interface, m Measurer, nets []string) *Chain {
	c := &Chain{Order: append([]string{iface.Controller}, iface.Devices...), OrderFrom: iface.ChainOrder}

	// Who reaches whom, over how many of the chain's nets.
	joined := map[[2]string]int{}
	considered := 0
	for _, net := range nets {
		mm := m.Measure(net)
		if mm == nil || len(mm.Pads) < 3 {
			continue
		}
		considered++
		for _, p := range mm.Paths {
			if !p.Found {
				continue
			}
			a, b := refOf(p.From), refOf(p.To)
			if a > b {
				a, b = b, a
			}
			joined[[2]string{a, b}]++
		}
	}
	reaches := func(a, b string) int {
		if a > b {
			a, b = b, a
		}
		return joined[[2]string{a, b}]
	}

	// Re-order from the copper, by how far along it each device sits.
	//
	// Being joined is not the same as being next: on a finished chain the
	// controller reaches the last device as surely as the first, through the
	// ones between. What distinguishes them is distance -- each hop adds
	// length, so the chain visits the devices in increasing order of routed
	// length from the controller. A device the copper does not reach has no
	// distance and keeps its place from the placement, which is all there is.
	if considered > 0 {
		far := map[string]float64{}
		for _, d := range iface.Devices {
			far[d] = math.Inf(1)
		}
		reached := 0
		for _, net := range nets {
			mm := m.Measure(net)
			if mm == nil {
				continue
			}
			for _, p := range mm.Paths {
				if !p.Found {
					continue
				}
				a, b := refOf(p.From), refOf(p.To)
				d := ""
				switch {
				case a == iface.Controller:
					d = b
				case b == iface.Controller:
					d = a
				default:
					continue
				}
				if cur, ok := far[d]; ok && p.Length < cur {
					if math.IsInf(cur, 1) {
						reached++
					}
					far[d] = p.Length
				}
			}
		}
		order := append([]string{}, iface.Devices...)
		sort.SliceStable(order, func(i, j int) bool { return far[order[i]] < far[order[j]] })
		order = append([]string{iface.Controller}, order...)
		if reached > 0 && !sameOrder(order, c.Order) {
			c.Note = fmt.Sprintf(
				"the copper joins these devices in the order %s, not the order their placement suggested (%s); "+
					"the chain order follows the copper",
				strings.Join(order, " -> "), strings.Join(c.Order, " -> "))
			c.Order = order
			iface.Devices = order[1:]
		}
	}

	// The hops, including the last device to its termination.
	for i := 0; i+1 < len(c.Order); i++ {
		c.Hops = append(c.Hops, Hop{From: c.Order[i], To: c.Order[i+1],
			Nets: reaches(c.Order[i], c.Order[i+1]), Of: considered})
	}
	if last := c.Order[len(c.Order)-1]; considered > 0 {
		// Termination is whatever the nets reach that is neither the
		// controller nor a device -- a resistor per net, so it is counted by
		// how many nets reach anything else from the last device.
		known := map[string]bool{}
		for _, r := range c.Order {
			known[r] = true
		}
		term := 0
		for pair, n := range joined {
			if n == 0 {
				continue
			}
			if pair[0] == last && !known[pair[1]] {
				term++
			}
			if pair[1] == last && !known[pair[0]] {
				term++
			}
		}
		c.Hops = append(c.Hops, Hop{From: last, To: "termination", Nets: term, Of: considered})
	}

	// A hop with copper beyond one with none is worth pointing out. It is what
	// a wrong chain order looks like -- but it is also what a half-routed
	// chain looks like when the hops were drawn out of sequence, which is the
	// demo board: someone drew the clock's last stub to its terminator before
	// the hop that feeds it. Both readings are worth the same sentence, and
	// neither is worth a conclusion the copper does not support.
	for i := 1; i < len(c.Hops); i++ {
		if c.Hops[i].Nets > 0 && c.Hops[i-1].Nets == 0 {
			if c.Note != "" {
				c.Note += ". "
			}
			c.Note += fmt.Sprintf(
				"%s has copper while %s has none, so either the hops were drawn out of order or the chain does not run this way",
				c.Hops[i].From+"->"+c.Hops[i].To, c.Hops[i-1].From+"->"+c.Hops[i-1].To)
			break
		}
	}
	return c
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reachesOf returns the spans the address and command groups are measured
// over: the controller to each device, in the order the chain visits them.
func reachesOf(iface *Interface, c *Chain) []leg {
	devices := iface.Devices
	if c != nil && len(c.Order) > 1 {
		devices = c.Order[1:]
	}
	out := make([]leg, 0, len(devices))
	for _, d := range devices {
		out = append(out, leg{iface.Controller, d})
	}
	return out
}

// buildGroup builds a point-to-point group: every member measured over its own
// longest route, which for a two-pad net is the whole of it.
func buildGroup(iface *Interface, meas map[string]*netlen.Measure, r Rules, kind GroupKind, lane int, nets []string) *Group {
	g := &Group{
		Kind:      kind,
		Lane:      lane,
		Name:      fmt.Sprintf("byte lane %d", lane),
		Tolerance: r.DataToStrobe,
	}
	for _, net := range nets {
		mm := meas[net]
		s := iface.Signals[net]
		member := Member{Net: net, Role: s.Role, Pair: s.Pair}
		if mm != nil && mm.Longest.Found {
			member.Routed = true
			member.Length = mm.Longest.Length
			member.Delay = mm.Longest.Delay
			member.From, member.To = mm.Longest.From, mm.Longest.To
			member.PathTracks = mm.Longest.Tracks
		} else {
			g.Unroutable = append(g.Unroutable, net)
		}
		g.Members = append(g.Members, member)
	}
	// The strobe pair is the reference.
	refLen := setReference(g, RoleStrobe)
	if g.Reference == "" {
		g.Notes = append(g.Notes, "no routed strobe (DQS) in this byte, so the target is the longest net")
	}
	finishGroup(g, refLen, 1.0, r)
	return g
}

// buildFlyByGroup builds the address and command group at one device: every
// member measured from the controller to that device, and matched to the clock
// measured the same way.
//
// The length is the whole path, but a meander for a later device can only go
// on the part of it past the previous device: anything added before that
// moves the previous device's lengths too. So PathTracks is that part alone,
// and the length a member will already gain from the previous devices' groups
// (upstream) is counted before working out what is left to add here.
// A member routed to this device without a route to the previous one is
// tuned over its whole path.
func buildFlyByGroup(iface *Interface, meas map[string]*netlen.Measure, r Rules, l leg, prev *leg,
	upstream map[string]float64, nets []string) *Group {
	g := &Group{
		Kind:      AddressCommand,
		Lane:      -1,
		Leg:       l.String(),
		Name:      "address/command " + l.String(),
		Tolerance: r.AddressToClock,
	}
	for _, net := range nets {
		mm := meas[net]
		s := iface.Signals[net]
		member := Member{Net: net, Role: s.Role, Pair: s.Pair}
		if mm != nil {
			if p, ok := legPath(mm, l); ok {
				member.Routed = true
				member.Length = p.Length
				member.Delay = p.Delay
				member.From, member.To = p.From, p.To
				member.PathTracks = p.Tracks
				if prev != nil {
					if before, ok := legPath(mm, *prev); ok {
						member.PathTracks = beyond(p.Tracks, before.Tracks)
					}
				}
			}
		}
		if !member.Routed {
			g.Unroutable = append(g.Unroutable, net)
		}
		g.Members = append(g.Members, member)
	}
	if len(g.Unroutable) == len(g.Members) {
		// Nothing reaches this device: there is no group here, only a fact
		// about the board, which the caller reports separately.
		return nil
	}
	refLen := setReference(g, RoleClock)
	if g.Reference == "" {
		g.Notes = append(g.Notes, "no routed clock (CLK) to this chip, so the target is the longest net")
	}
	finishGroup(g, refLen, 1+r.ClockOffsetPercent/100, r)

	// What the groups before this one will add is already on its way here.
	grown := 0
	for i := range g.Members {
		m := &g.Members[i]
		up := upstream[m.Net]
		if !m.Routed || m.Reference || up <= 0 {
			continue
		}
		after := m.Length + up
		m.Need = math.Max(0, g.Target-after)
		m.NeedDelay = m.Need * safeRatio(m.Delay, m.Length)
		if dev := after - g.Target; dev > g.ToleranceMM && m.Excess == 0 {
			m.Excess = dev - g.ToleranceMM
			grown++
		}
	}
	if grown > 0 {
		g.Notes = append(g.Notes, fmt.Sprintf(
			"%d net(s) will be too long here after the length added for %s. "+
				"Add less before %s, or reroute this part shorter",
			grown, prev.to, prev.to))
		sort.Strings(g.Notes)
	}
	return g
}

// beyond is the tracks of path that are not on before, in path's order.
func beyond(path, before []string) []string {
	on := make(map[string]bool, len(before))
	for _, t := range before {
		on[t] = true
	}
	out := make([]string, 0, len(path))
	for _, t := range path {
		if !on[t] {
			out = append(out, t)
		}
	}
	return out
}

func safeRatio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// legPath finds the route between the two named devices on a net.
func legPath(m *netlen.Measure, l leg) (netlen.Path, bool) {
	for _, p := range m.Paths {
		if !p.Found {
			continue
		}
		a, b := refOf(p.From), refOf(p.To)
		if (a == l.from && b == l.to) || (a == l.to && b == l.from) {
			return p, true
		}
	}
	return netlen.Path{}, false
}

func refOf(padID string) string {
	if i := strings.IndexByte(padID, '.'); i >= 0 {
		return padID[:i]
	}
	return padID
}

// setReference marks the group's reference -- the members with the given role,
// a strobe or a clock pair -- and returns its length: the mean of the routed
// halves.
//
// The mean, not the longer half. A differential receiver switches at the
// crossing point of the pair, and when one half is longer than the other that
// crossing sits between their two arrival times, not at the later one. Taking
// the longer half as the reference shifted every target in the group by half
// the pair's own skew.
func setReference(g *Group, role Role) float64 {
	var sum float64
	var names []string
	for i := range g.Members {
		m := &g.Members[i]
		if m.Role != role || !m.Routed {
			continue
		}
		m.Reference = true
		names = append(names, m.Net)
		sum += m.Length
		g.ReferenceMembers = append(g.ReferenceMembers, *m)
	}
	sort.Strings(names)
	sort.Slice(g.ReferenceMembers, func(i, j int) bool {
		return g.ReferenceMembers[i].Net < g.ReferenceMembers[j].Net
	})
	g.Reference = strings.Join(names, " / ")
	if len(names) == 0 {
		return 0
	}
	g.ReferenceLength = sum / float64(len(names))
	return g.ReferenceLength
}

// finishGroup sets the target and fills in each member's requirement.
//
// The target is the reference's -- the clock or strobe mean, times any clock
// offset -- and every member's offset is measured against it. It used to be
// raised to the longest member, on the reasoning that a meander can only add
// length; the result was a byte lane matched to whichever data bit happened to
// be longest (DQ19 on the second demo board) rather than to its strobe, and an
// offset column that meant "distance from DQ19". A member longer than the
// reference is now reported as exactly that: too long by so much, which is a
// routing change, not a tuning one.
func finishGroup(g *Group, refLen, scale float64, r Rules) {
	target := refLen * scale
	if refLen <= 0 {
		// No routed reference at all: the longest member is the only thing
		// left to match to, and the note added by the caller says so.
		for _, m := range g.Members {
			if m.Routed && m.Length > target {
				target = m.Length
			}
		}
	}
	g.TargetFromReference = target
	g.Target = target

	// Resolve the tolerance into a length using the group's own propagation
	// rate, taken from a routed member so it reflects the layers actually used.
	psPerMM := 0.0
	for _, m := range g.Members {
		if m.Routed && m.Length > 0 {
			psPerMM = m.Delay / m.Length
			break
		}
	}
	if mm, ok := r.GroupToleranceMM[g.Name]; ok && mm > 0 {
		g.Tolerance = Tolerance{MM: mm}
	}
	g.ToleranceMM = g.Tolerance.LimitMM(psPerMM)
	g.TargetDelay = target * psPerMM

	long := 0
	for i := range g.Members {
		m := &g.Members[i]
		if !m.Routed {
			continue
		}
		m.Deviation = m.Length - target
		m.InTolerance = math.Abs(m.Deviation) <= g.ToleranceMM
		if m.Reference {
			// The reference is not tuned towards its own mean; its halves are
			// matched to each other.
			continue
		}
		m.Need = math.Max(0, target-m.Length)
		m.NeedDelay = m.Need * psPerMM
		if m.Deviation > g.ToleranceMM {
			m.Excess = m.Deviation - g.ToleranceMM
			long++
		}
	}
	if long > 0 {
		g.Notes = append(g.Notes, fmt.Sprintf(
			"%d net(s) are too long. A meander cannot make a track shorter: reroute them shorter",
			long))
	}
	// No note on the skew inside a DQS or CLK pair. ST's AN5724 sets no
	// limit on it and forbids adding length to one half of a pair; a pair's
	// length is the mean of its halves, which is what the groups use.
	sort.Strings(g.Notes)
	g.Notes = dedupeStrings(g.Notes)
}

func shortNet(n string) string {
	if i := strings.LastIndexByte(n, '/'); i >= 0 {
		return n[i+1:]
	}
	return n
}

func dedupeStrings(in []string) []string {
	out := in[:0]
	var last string
	for i, s := range in {
		if i > 0 && s == last {
			continue
		}
		out = append(out, s)
		last = s
	}
	return out
}

// Headroomer can say how much length a net's route could absorb. tune.Tuner
// satisfies it.
//
// The plan takes it as an interface rather than importing the tuner, so that
// measuring the board stays independent of the thing that edits it.
type Headroomer interface {
	Headroom(net string, on map[string]bool) float64
}

// MeasureHeadroom fills in each member's Headroom.
//
// It is separate from BuildPlan because it is much the slower half: it probes
// the clearance rules along every candidate track, where the plan only measures
// lengths. A caller that just wants the report can skip it.
func (p *Plan) MeasureHeadroom(h Headroomer) {
	cache := map[string]float64{}
	for _, g := range p.Groups {
		for i := range g.Members {
			m := &g.Members[i]
			if !m.Routed || m.Need <= 1e-6 || m.InTolerance {
				continue
			}
			key := m.Net + "|" + g.Leg
			if v, ok := cache[key]; ok {
				m.Headroom = v
				continue
			}
			on := make(map[string]bool, len(m.PathTracks))
			for _, u := range m.PathTracks {
				on[u] = true
			}
			v := h.Headroom(m.Net, on)
			cache[key] = v
			m.Headroom = v
		}
	}
}

// RerouteThreshold is the fraction of its own length a net may be asked to grow
// before the request stops being a tuning job.
//
// A meander folds extra path into the space beside a track. Asking a 17 mm
// route to become 31 mm is not a request for space beside it; it is a request
// for a different route. The threshold is a presentation choice rather than a
// physical limit, set where the honest advice changes from "find room" to
// "route it differently".
const RerouteThreshold = 0.25

// NeedsReroute reports whether a member is asking for more length than tuning
// could sensibly deliver.
func (m Member) NeedsReroute() bool {
	if !m.Routed || m.Length <= 0 {
		return false
	}
	// Too long is a reroute by definition: nothing here shortens a track.
	return m.Excess > 0 || m.Need/m.Length > RerouteThreshold
}

// IntraPairSkew returns the measured skew between the two halves of each
// differential pair in the plan, keyed by the positive half's net name.
func (p *Plan) IntraPairSkew() map[string]float64 {
	out := map[string]float64{}
	for _, g := range p.Groups {
		byNet := map[string]Member{}
		for _, m := range g.Members {
			byNet[m.Net] = m
		}
		for _, m := range g.Members {
			if m.Pair == "" || !m.Routed {
				continue
			}
			o, ok := byNet[m.Pair]
			if !ok || !o.Routed {
				continue
			}
			if s := p.Interface.Signals[m.Net]; s != nil && s.Polarity != Positive {
				continue
			}
			key := m.Net
			if g.Leg != "" {
				key += " " + g.Leg
			}
			out[key] = math.Abs(m.Length - o.Length)
		}
	}
	return out
}
