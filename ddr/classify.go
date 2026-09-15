// Package ddr encodes what a DDR interface is: which net is a data bit, which
// strobe it belongs to, which nets are addressed fly-by, and what has to match
// what.
//
// Getting this right is the difference between a length matcher and a length
// matcher for DDR. A DQ bit is matched to the strobe of its own byte lane and
// to nothing else; a strobe is matched to its own complement far more tightly
// than to anything else; a fly-by address line is matched per leg, because
// equal total length with unequal legs still fails write levelling. None of
// that is visible in a net name alone, so the classifier works from the net
// name, the pin functions on the pads and the pad-to-device topology together,
// and reports what it could not work out rather than guessing.
package ddr

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Role is what a DDR net does.
type Role int

const (
	// RoleUnknown is a net the classifier could not place.
	RoleUnknown Role = iota
	// RoleData is a DQ bit.
	RoleData
	// RoleDataMask is a DM/DQM/DBI line, which travels with its byte lane.
	RoleDataMask
	// RoleStrobe is a DQS line, the timing reference for its byte lane.
	RoleStrobe
	// RoleClock is CK/CLK, the timing reference for the address and command
	// group.
	RoleClock
	// RoleAddress is an address or bank line: A, BA, BG.
	RoleAddress
	// RoleCommand is a command line: RAS, CAS, WE, ACT, CS, CKE, ODT, PAR.
	RoleCommand
	// RoleControl is a control line matched loosely or not at all, such as
	// RESET.
	RoleControl
)

var roleNames = map[Role]string{
	RoleUnknown:  "unknown",
	RoleData:     "data",
	RoleDataMask: "data-mask",
	RoleStrobe:   "strobe",
	RoleClock:    "clock",
	RoleAddress:  "address",
	RoleCommand:  "command",
	RoleControl:  "control",
}

func (r Role) String() string { return roleNames[r] }

// Polarity is which half of a differential pair a net is.
type Polarity int

const (
	// Single is a net that is not part of a pair.
	Single Polarity = iota
	// Positive is the true half, _P or _T.
	Positive
	// Negative is the complement half, _N or _C.
	Negative
)

// Signal is one classified DDR net.
type Signal struct {
	Net string

	// Role is what the net does.
	Role Role

	// Index is the bit number for data, or the rank/copy number for a strobe,
	// mask or clock; -1 when the signal is not numbered.
	Index int

	// Lane is the byte lane a data, mask or strobe signal belongs to, or -1.
	Lane int

	// Polarity is the half of a differential pair, for strobes and clocks.
	Polarity Polarity

	// Pair is the net name of the other half of the differential pair, empty
	// when there is none.
	Pair string

	// Devices lists the component references the net reaches, in the order it
	// reaches them from the controller: the controller first, then the memory
	// devices, then any termination.
	Devices []string

	// Controller is the reference of the device driving the interface.
	Controller string

	// Why records how the net was classified, so a surprising grouping can be
	// explained rather than argued with.
	Why string
}

// LaneWidth is the number of data bits per byte lane. DDR is byte-organised
// and this is not a tunable.
const LaneWidth = 8

// Interface is a whole classified DDR interface.
type Interface struct {
	// Controller is the reference of the device driving the interface.
	Controller string

	// Devices are the memory device references, ordered along the fly-by chain
	// from the controller outward.
	//
	// The order is not cosmetic. Address, command, control and clock reach the
	// devices in series, so every one of those nets is a chain of hops and each
	// hop has to be matched against the clock over that same hop. Get the order
	// wrong and every comparison is against the wrong span.
	Devices []string

	// ChainOrder says how that order was arrived at, because it is a
	// conclusion and not a reading. It goes in the report.
	ChainOrder string

	// Signals is every classified net, keyed by net name.
	Signals map[string]*Signal

	// Width is the data bus width in bits.
	Width int

	// Lanes is the number of byte lanes.
	Lanes int

	// Unclassified lists nets that look like they belong to the interface but
	// could not be placed.
	Unclassified []string

	// Notes records anything the caller should know about the classification.
	Notes []string
}

// Signal returns a net's classification, or nil.
func (i *Interface) Signal(net string) *Signal { return i.Signals[net] }

// NetsWithRole returns the nets of a given role, sorted by index.
func (i *Interface) NetsWithRole(roles ...Role) []string {
	want := map[Role]bool{}
	for _, r := range roles {
		want[r] = true
	}
	var out []*Signal
	for _, s := range i.Signals {
		if want[s.Role] {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Role != out[b].Role {
			return out[a].Role < out[b].Role
		}
		if out[a].Index != out[b].Index {
			return out[a].Index < out[b].Index
		}
		return out[a].Net < out[b].Net
	})
	names := make([]string, len(out))
	for k, s := range out {
		names[k] = s.Net
	}
	return names
}

// LaneOf returns the byte lane a net belongs to, or -1.
func (i *Interface) LaneOf(net string) int {
	if s := i.Signals[net]; s != nil {
		return s.Lane
	}
	return -1
}

// ---- name parsing ----

// The suffix of a net name after the last separator carries the signal, e.g.
// "/ddr4/DDR_DQ18" -> "DQ18". Hierarchical sheet paths and vendor prefixes are
// stripped so that the same rules work on "DDR_DQ0", "MEM_DQ0" and "dq0".
var sepRE = regexp.MustCompile(`[/\\]`)

func leaf(net string) string {
	parts := sepRE.Split(net, -1)
	return parts[len(parts)-1]
}

// stripPrefix removes a leading bus prefix such as DDR_, DDR4_, LPDDR4_, MEM_,
// SDRAM_ or EMIF_, leaving the signal name.
var prefixRE = regexp.MustCompile(`(?i)^(?:[A-Z0-9]*?(?:DDR|SDRAM|MEM|EMIF|DRAM)[A-Z0-9]*?)[_\-]`)

func signalName(net string) string {
	s := leaf(net)
	if m := prefixRE.FindString(s); m != "" {
		s = s[len(m):]
	}
	return strings.ToUpper(s)
}

// polarityRE matches the differential suffix. DDR uses _T/_C in JEDEC wording
// and _P/_N in most schematics, and both appear in the wild.
var polarityRE = regexp.MustCompile(`^(.*?)[_]?(P|N|T|C)$`)

func splitPolarity(name string) (base string, pol Polarity) {
	// Only treat a trailing letter as polarity when what precedes it looks
	// like a differential signal. "DQ0" must not become "DQ" + polarity, and
	// "CASN"/"ACTN"/"RESETN" are active-low singles, not complements.
	for _, stem := range []string{"DQS", "CK", "CLK", "WCK", "RDQS", "DQSU", "DQSL"} {
		m := polarityRE.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		b := m[1]
		trimmed := strings.TrimRight(b, "0123456789")
		if trimmed != stem {
			continue
		}
		switch m[2] {
		case "P", "T":
			return b, Positive
		case "N", "C":
			return b, Negative
		}
	}
	return name, Single
}

var trailingNumRE = regexp.MustCompile(`^([A-Z_]*?)([0-9]*)$`)

func splitIndex(name string) (stem string, idx int) {
	m := trailingNumRE.FindStringSubmatch(name)
	if m == nil || m[2] == "" {
		return name, -1
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return name, -1
	}
	return strings.TrimRight(m[1], "_"), n
}

// classifyName places a signal from its name alone.
func classifyName(net string) (role Role, stem string, idx int, pol Polarity) {
	name := signalName(net)
	base, pol := splitPolarity(name)
	stem, idx = splitIndex(base)

	switch stem {
	case "DQ", "D":
		return RoleData, stem, idx, pol
	case "DQS", "RDQS", "DQSU", "DQSL":
		return RoleStrobe, "DQS", idx, pol
	case "CK", "CLK", "WCK", "CKT", "CKC":
		return RoleClock, "CK", idx, pol
	case "DM", "DQM", "DMI", "DBI", "UDM", "LDM", "DQSDM":
		return RoleDataMask, "DM", idx, pol
	case "A", "ADDR", "MA", "BA", "BG", "BANK":
		return RoleAddress, stem, idx, pol
	case "RAS", "RASN", "CAS", "CASN", "WE", "WEN", "ACT", "ACTN",
		"CS", "CSN", "CKE", "ODT", "PAR", "ALERT", "ALERTN", "TEN", "CA":
		return RoleCommand, stem, idx, pol
	case "RESET", "RESETN", "RST", "RSTN", "ZQ", "VREF", "MIR", "CAI", "LBDQS":
		return RoleControl, stem, idx, pol
	}
	return RoleUnknown, stem, idx, pol
}

// ---- classification ----

// Options tune the classifier for boards whose naming or topology the defaults
// do not cover.
type Options struct {
	// NetPrefix restricts classification to nets with this prefix, e.g.
	// "/ddr4/". Empty means every net is considered.
	NetPrefix string

	// Nets, when not nil, restricts classification to exactly these nets. It
	// is how a bus with flat names ("DDR_DQ0", no sheet path to act as a
	// prefix) is scoped to itself rather than to every net on the board.
	Nets []string

	// Controller names the driving device. Empty means it is inferred as the
	// component appearing on the most DDR nets.
	Controller string

	// LaneOfNet overrides byte-lane assignment for a net. Used where a design
	// swaps lanes between the controller and the devices, which is legal and
	// invisible to any naming rule.
	LaneOfNet map[string]int
}

// Classify works out the structure of the DDR interface on a board.
//
// The byte lane of a data bit follows from its bit number, but the lane of a
// strobe or a mask does not: on a x32 interface made of two x16 devices, DQS2
// belongs to the lane carrying DQ16..DQ23, and the only reliable way to know
// that is to see which device each net lands on. So lanes are derived from the
// data bits first, then strobes and masks are attached to the lane they share a
// device with.
func Classify(b *board.Board, opt Options) (*Interface, error) {
	iface := &Interface{Signals: map[string]*Signal{}}

	type cand struct {
		net  string
		role Role
		stem string
		idx  int
		pol  Polarity
		refs []string
	}
	var cands []cand
	refCount := map[string]int{}

	inScope := func(net string) bool {
		if opt.Nets != nil {
			return slices.Contains(opt.Nets, net)
		}
		return opt.NetPrefix == "" || strings.HasPrefix(net, opt.NetPrefix)
	}
	for _, net := range b.Nets() {
		if !inScope(net) {
			continue
		}
		role, stem, idx, pol := classifyName(net)
		if role == RoleUnknown {
			continue
		}
		pads := b.PadsOfNet(net)
		if len(pads) < 2 {
			continue
		}
		refs := map[string]bool{}
		for _, p := range pads {
			refs[p.Ref] = true
		}
		var list []string
		for r := range refs {
			list = append(list, r)
			refCount[r]++
		}
		sort.Strings(list)
		cands = append(cands, cand{net, role, stem, idx, pol, list})
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("ddr: no DDR-looking nets found (prefix %q)", opt.NetPrefix)
	}

	// The controller is the device on the most DDR nets: it touches every one,
	// while each memory device touches only its own share of the data bus.
	ctrl := opt.Controller
	if ctrl == "" {
		best := 0
		var refs []string
		for r := range refCount {
			refs = append(refs, r)
		}
		sort.Strings(refs)
		for _, r := range refs {
			if refCount[r] > best {
				best, ctrl = refCount[r], r
			}
		}
	}
	iface.Controller = ctrl

	// Memory devices are the components that appear on data bits alongside the
	// controller. Termination resistors appear only on address and command
	// nets, so this excludes them without needing to know what a resistor is.
	devSet := map[string]int{}
	for _, c := range cands {
		if c.role != RoleData {
			continue
		}
		for _, r := range c.refs {
			if r != ctrl {
				devSet[r]++
			}
		}
	}
	for d := range devSet {
		iface.Devices = append(iface.Devices, d)
	}
	// The nets of the interface, which is what "a DDR pad" means here. Taken
	// from the candidates rather than from iface.Signals, which is not filled
	// in until below -- reading it here found no pads on anything and quietly
	// fell back to the designators, which is the very guess this replaces.
	ddrNets := make(map[string]bool, len(cands))
	for _, c := range cands {
		ddrNets[c.net] = true
	}
	iface.ChainOrder = orderChain(b, iface, ddrNets)

	// Lane of each data bit follows from its bit number; that also tells us
	// which device owns which lane.
	laneOfDevice := map[string][]int{}
	for _, c := range cands {
		if c.role != RoleData || c.idx < 0 {
			continue
		}
		lane := c.idx / LaneWidth
		for _, r := range c.refs {
			if r == ctrl {
				continue
			}
			if !slicesContainsInt(laneOfDevice[r], lane) {
				laneOfDevice[r] = append(laneOfDevice[r], lane)
			}
		}
	}

	for _, c := range cands {
		s := &Signal{
			Net:        c.net,
			Role:       c.role,
			Index:      c.idx,
			Lane:       -1,
			Polarity:   c.pol,
			Devices:    c.refs,
			Controller: ctrl,
		}
		switch c.role {
		case RoleData:
			if c.idx >= 0 {
				s.Lane = c.idx / LaneWidth
				s.Why = fmt.Sprintf("bit %d is in byte lane %d", c.idx, s.Lane)
			}
		case RoleStrobe, RoleDataMask:
			// Attach to the lane of the device this net lands on. Where that
			// device owns more than one lane -- a x16 device carries two -- the
			// net's own index picks between them.
			for _, r := range c.refs {
				if r == ctrl {
					continue
				}
				lanes := laneOfDevice[r]
				if len(lanes) == 0 {
					continue
				}
				sort.Ints(lanes)
				if len(lanes) == 1 {
					s.Lane = lanes[0]
					s.Why = fmt.Sprintf("lands on %s, which carries byte lane %d", r, s.Lane)
					break
				}
				if c.idx >= 0 && c.idx < len(lanes)+lanes[0]+LaneWidth {
					// Strobe and mask numbering runs in step with lane
					// numbering across the whole interface, so the index is
					// the lane when the device owns several.
					if slicesContainsInt(lanes, c.idx) {
						s.Lane = c.idx
						s.Why = fmt.Sprintf("index %d matches byte lane %d on %s", c.idx, c.idx, r)
						break
					}
				}
			}
			if s.Lane < 0 && c.idx >= 0 {
				s.Lane = c.idx
				s.Why = fmt.Sprintf("assumed byte lane %d from the index; no device gave a stronger clue", c.idx)
			}
		case RoleClock, RoleAddress, RoleCommand, RoleControl:
			s.Why = fmt.Sprintf("%s line, shared by %d device(s)", c.role, len(c.refs)-1)
		}
		if l, ok := opt.LaneOfNet[c.net]; ok {
			s.Lane = l
			s.Why = "byte lane set explicitly by the caller"
		}
		iface.Signals[c.net] = s
	}

	// Pair up the differential halves.
	byBase := map[string][]*Signal{}
	for _, s := range iface.Signals {
		if s.Polarity == Single {
			continue
		}
		base, _ := splitPolarity(signalName(s.Net))
		key := fmt.Sprintf("%s/%d/%d", base, s.Role, s.Lane)
		byBase[key] = append(byBase[key], s)
	}
	for _, group := range byBase {
		if len(group) != 2 {
			continue
		}
		group[0].Pair, group[1].Pair = group[1].Net, group[0].Net
	}
	for _, s := range iface.Signals {
		if s.Polarity != Single && s.Pair == "" {
			iface.Notes = append(iface.Notes,
				fmt.Sprintf("%s looks like half a differential pair but its complement was not found", s.Net))
		}
	}

	// Width and lane count from the data bits actually present.
	bits := map[int]bool{}
	lanes := map[int]bool{}
	for _, s := range iface.Signals {
		if s.Role == RoleData && s.Index >= 0 {
			bits[s.Index] = true
		}
		if s.Lane >= 0 {
			lanes[s.Lane] = true
		}
	}
	iface.Width = len(bits)
	iface.Lanes = len(lanes)

	// Anything with the right prefix that did not classify is worth saying out
	// loud rather than dropping.
	for _, net := range b.Nets() {
		if (opt.NetPrefix == "" && opt.Nets == nil) || !inScope(net) {
			continue
		}
		if _, ok := iface.Signals[net]; ok {
			continue
		}
		if len(b.PadsOfNet(net)) < 2 {
			continue
		}
		iface.Unclassified = append(iface.Unclassified, net)
	}
	sort.Strings(iface.Unclassified)
	sort.Strings(iface.Notes)
	return iface, nil
}

func slicesContainsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// orderChain puts the memory devices in the order the fly-by chain visits them,
// and returns a sentence saying how it decided.
//
// This matters more than it looks. Address, command, control and clock are
// routed in series -- controller to the first device, that device to the next,
// and on to the termination at the end -- and each hop is matched against the
// clock over that same hop. An order taken from the reference designators is a
// guess that happens to be right when somebody numbered the parts along the
// chain, and silently compares the wrong spans when they did not.
//
// So the order comes from where the parts are: the chain runs outward from the
// controller, so the devices are sorted by how far their own DDR pads sit from
// the controller's. That is a statement about this board rather than about its
// naming, and where the copper joining the devices exists, BuildPlan checks it
// against that and says so.
func orderChain(b *board.Board, iface *Interface, ddrNets map[string]bool) string {
	if len(iface.Devices) < 2 {
		sort.Strings(iface.Devices)
		return ""
	}
	// The centre of each part's DDR pads, rather than the footprint origin,
	// which on a BGA can sit anywhere relative to the ball field.
	centre := func(ref string) (geom.Pt, bool) {
		var sum geom.Pt
		n := 0
		for _, p := range b.Pads {
			if p.Ref != ref {
				continue
			}
			if !ddrNets[p.Net] {
				continue
			}
			sum = sum.Add(p.Centre)
			n++
		}
		if n == 0 {
			return geom.Pt{}, false
		}
		return geom.Pt{X: sum.X / float64(n), Y: sum.Y / float64(n)}, true
	}

	from, ok := centre(iface.Controller)
	if !ok {
		sort.Strings(iface.Devices)
		return "the chain order follows the reference designators: the controller has no DDR pads to measure from"
	}
	dist := map[string]float64{}
	for _, d := range iface.Devices {
		c, ok := centre(d)
		if !ok {
			dist[d] = math.Inf(1)
			continue
		}
		dist[d] = from.Dist(c)
	}
	sort.Slice(iface.Devices, func(i, j int) bool {
		a, b := iface.Devices[i], iface.Devices[j]
		if dist[a] != dist[b] {
			return dist[a] < dist[b]
		}
		return a < b
	})
	parts := make([]string, 0, len(iface.Devices))
	for _, d := range iface.Devices {
		parts = append(parts, fmt.Sprintf("%s at %.0f mm", d, dist[d]))
	}
	return fmt.Sprintf("the chain order is taken from how far each device sits from %s (%s)",
		iface.Controller, strings.Join(parts, ", "))
}
