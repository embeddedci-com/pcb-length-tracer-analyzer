package proto

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// Kind is a family of interface.
type Kind string

const (
	DDR      Kind = "ddr"
	RGMII    Kind = "rgmii"
	MIIRMII  Kind = "rmii"
	PCIe     Kind = "pcie"
	USB2     Kind = "usb2"
	USBSS    Kind = "usb-ss"
	MIPI     Kind = "mipi"
	SDMMC    Kind = "sdmmc"
	DiffOnly Kind = "differential"
)

// Tolerance is a matching limit. Either unit may be zero; where both are given
// the tighter one governs, which is how the DDR side has always worked.
type Tolerance struct {
	MM float64
	PS float64
}

// Group is a set of nets that have to match one reference.
type Group struct {
	// Name is what the report calls it, e.g. "ETH1 transmit".
	Name string

	// Reference is the net everything in the group is matched to. Empty means
	// the group is matched to its own longest member.
	Reference string

	Members   []string
	Tolerance Tolerance

	// Why says what this group is and where its default limit comes from, so
	// the number can be argued with rather than just obeyed.
	Why string
}

// Interface is one recognised interface on the board.
type Interface struct {
	Kind Kind

	// Name identifies it to a person: "Ethernet RGMII (ETH1)".
	Name string

	// Instance is the interface's own name where the board gave it one.
	Instance string

	// Nets is every net belonging to it, sorted.
	Nets []string

	// Pairs are its differential pairs.
	Pairs []Pair

	// IntraPair is how closely the two halves of a pair must match. Zero when
	// the interface has no pairs.
	IntraPair Tolerance

	// Groups are the sets that have to match a reference. An interface may be
	// only pairs, with no groups at all -- PCIe is exactly that.
	Groups []Group

	// Evidence is how it was recognised, in the user's own net names. This is
	// a reading, not a proof, and it is shown so the user can disagree.
	Evidence string

	// Planner names the analyser that handles this interface properly, where
	// one exists. An interface with a planner is not described by its groups
	// here -- DDR's requirement is not one set against one reference, and a
	// group that pretended otherwise would produce a number rather than an
	// answer.
	Planner string

	// Routed and Total count the nets with any copper on them, and all of
	// them, so a half-drawn interface is obvious before anything is measured.
	Routed, Total int
}

// Detect reads a board and returns the interfaces on it, most substantial
// first.
//
// Recognition is by net name. That is not a limitation to apologise for: the
// name is what the designer wrote down to say what the net is, and no amount of
// geometry will tell you that a pair is PCIe rather than SATA. What it does
// mean is that every interface carries its evidence, and that a board naming
// nothing usefully will be reported as having nothing recognisable rather than
// being guessed at.
func Detect(b *board.Board) []*Interface {
	nets := b.Nets()
	routed := map[string]bool{}
	for _, n := range nets {
		if len(b.TracksOfNet(n)) > 0 {
			routed[n] = true
		}
	}

	// Each net belongs to at most one interface: the detectors run in order of
	// how specific they are, and a net claimed by one is not offered to the
	// next.
	taken := map[string]bool{}
	var out []*Interface
	for _, d := range detectors {
		var avail []string
		for _, n := range nets {
			if !taken[n] && n != "" {
				avail = append(avail, n)
			}
		}
		for _, iface := range d(avail) {
			for _, n := range iface.Nets {
				taken[n] = true
			}
			iface.Total = len(iface.Nets)
			for _, n := range iface.Nets {
				if routed[n] {
					iface.Routed++
				}
			}
			out = append(out, iface)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Nets) != len(out[j].Nets) {
			return len(out[i].Nets) > len(out[j].Nets)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// detectors run most specific first.
var detectors = []func([]string) []*Interface{
	detectDDR,
	detectRGMII,
	detectMIPI,
	detectPCIe,
	detectUSB,
	detectSDMMC,
	detectLeftoverPairs,
}

// detectLeftoverPairs reports the differential pairs no interface claimed.
//
// A pair nobody recognised is still a pair: its two halves have to match each
// other whatever the signal turns out to be, and that is a real check the user
// can act on. Naming it as an unrecognised interface rather than guessing at
// one keeps the guess out of the report while leaving the work available.
func detectLeftoverPairs(nets []string) []*Interface {
	pairs := FindPairs(nets)
	if len(pairs) == 0 {
		return nil
	}
	byScope := map[string][]Pair{}
	for _, p := range pairs {
		key := strings.TrimSuffix(strings.TrimPrefix(scope(p.P), "/"), "/")
		byScope[key] = append(byScope[key], p)
	}
	var out []*Interface
	for sc, ps := range byScope {
		var ns []string
		for _, p := range ps {
			ns = append(ns, p.P, p.N)
		}
		sort.Strings(ns)
		name := "Differential pairs"
		if sc != "" {
			name += " (" + sc + ")"
		}
		out = append(out, &Interface{
			Kind: DiffOnly, Instance: sc, Nets: ns, Pairs: ps,
			Name:      name,
			IntraPair: Tolerance{MM: 0.127},
			Evidence: fmt.Sprintf("%d pair(s) whose two halves are named as complements, "+
				"belonging to no interface this recognizes (%s)", len(ps), sample(ns, 4)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// byInstance groups the nets a predicate accepts by their interface instance.
func byInstance(nets []string, want func(sig string) bool) map[string][]string {
	out := map[string][]string{}
	for _, n := range nets {
		if want(signal(n)) {
			key := instance(n)
			if key == "" {
				key = strings.TrimSuffix(strings.TrimPrefix(scope(n), "/"), "/")
			}
			out[key] = append(out[key], n)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// ---- the interfaces ----

var ddrRE = regexp.MustCompile(`^(DQ[0-9]+|DQS[0-9]*(_[PNTC])?|DQM[0-9]*|DM[0-9]*|A[0-9]+|BA[0-9]*|BG[0-9]*|CK[E]?[0-9]*(_[PNTC])?|CLK(_[PNTC])?|CS[N]?[0-9]*|RAS[N]?|CAS[N]?|WE[N]?|ACT[N]?|ODT[0-9]*|PAR|RESET[N]?|ALERT[N]?|TEN|ZQ)$`)

func detectDDR(nets []string) []*Interface {
	groups := byInstance(nets, func(s string) bool {
		s = strings.TrimPrefix(s, "DDR_")
		return ddrRE.MatchString(s)
	})
	var out []*Interface
	for inst, ns := range groups {
		// A handful of matching names is a coincidence; a DDR bus is dozens.
		if len(ns) < 16 {
			continue
		}
		iface := &Interface{
			Kind: DDR, Instance: inst, Nets: ns,
			Name:  strings.TrimSpace("DDR memory " + bracketFor(inst, "DDR")),
			Pairs: FindPairs(ns),
			// No intra-pair limit: ST's AN5724 sets none for DQS or CLK, and
			// forbids lengthening one half. The skew is shown for information.
			Evidence: fmt.Sprintf("%d nets named like a DDR bus (%s)", len(ns),
				sample(ns, 4)),
		}
		// No generic group for DDR on purpose. Its members are not one set
		// matched to one reference: each byte lane goes to its own strobe, the
		// address bus to the clock, and both per leg of the fly-by chain. A
		// group here would produce a number -- the spread of all 71 nets
		// against the longest -- that is arithmetic rather than a
		// requirement. The DDR analyser does this properly and is what the
		// report uses.
		iface.Planner = "the DDR analyser, which matches each byte lane to its own strobe " +
			"and the address bus to the clock, per leg of the fly-by chain"
		out = append(out, iface)
	}
	return out
}

var (
	rgmiiTX = regexp.MustCompile(`^(TXD[0-3]|TX_?(CTL|EN|ER)|GTX_?CLK|TX_?CLK)$`)
	rgmiiRX = regexp.MustCompile(`^(RXD[0-3]|RX_?(CTL|DV|ER)|RX_?CLK)$`)
	rgmiiMD = regexp.MustCompile(`^(MDC|MDIO|MDINT|NRST|CLK125|INT|RESET[N]?)$`)
)

func detectRGMII(nets []string) []*Interface {
	groups := byInstance(nets, func(s string) bool {
		return rgmiiTX.MatchString(s) || rgmiiRX.MatchString(s) || rgmiiMD.MatchString(s)
	})
	var out []*Interface
	for inst, ns := range groups {
		var tx, rx []string
		for _, n := range ns {
			switch {
			case rgmiiTX.MatchString(signal(n)):
				tx = append(tx, n)
			case rgmiiRX.MatchString(signal(n)):
				rx = append(rx, n)
			}
		}
		// Both halves of the bus, or it is not RGMII.
		if len(tx) < 4 || len(rx) < 4 {
			continue
		}
		iface := &Interface{
			Kind: RGMII, Instance: inst, Nets: ns,
			Name:     "Ethernet RGMII " + bracket(inst),
			Evidence: fmt.Sprintf("%d transmit and %d receive nets named RGMII-style (%s)", len(tx), len(rx), sample(append(tx, rx...), 4)),
		}
		// Each direction is a source-synchronous bus: the data travels with its
		// own clock, so each is matched to that clock and to nothing else.
		iface.Groups = []Group{
			{
				Name: "transmit", Reference: clockOf(tx, `^(GTX_?CLK|TX_?CLK)$`), Members: tx,
				Tolerance: Tolerance{MM: 10.0},
				Why: "RGMII transmit is source-synchronous: TXD and TX_CTL travel with GTX_CLK, " +
					"so they are matched to it. 10 mm is a widely published starting point; the " +
					"figure that matters is the setup and hold budget in the PHY's data sheet",
			},
			{
				Name: "receive", Reference: clockOf(rx, `^RX_?CLK$`), Members: rx,
				Tolerance: Tolerance{MM: 10.0},
				Why: "RGMII receive travels with RX_CLK from the PHY, and is matched to it. " +
					"Same caveat as transmit: the PHY's data sheet governs",
			},
		}
		out = append(out, iface)
	}
	return out
}

var mipiRE = regexp.MustCompile(`^(CK|CLK|D[0-9]+)(CON)?(_[PN]|[+-])$`)

func detectMIPI(nets []string) []*Interface {
	groups := byInstance(nets, func(s string) bool { return mipiRE.MatchString(s) })
	var out []*Interface
	for inst, ns := range groups {
		pairs := FindPairs(ns)
		if len(pairs) < 2 {
			continue
		}
		var clocks, lanes []string
		for _, p := range pairs {
			if strings.HasPrefix(signal(p.P), "CK") || strings.HasPrefix(signal(p.P), "CLK") {
				clocks = append(clocks, p.P)
				continue
			}
			lanes = append(lanes, p.P)
		}
		if len(clocks) == 0 || len(lanes) == 0 {
			continue
		}
		// The plainest clock name wins. A board with a connector in the link
		// has the same lane twice -- CSI.CK_P on one side and CSI.CKcon_P on
		// the other -- and matching the lanes against whichever happened to
		// sort first would compare two different segments.
		clock := clocks[0]
		for _, c := range clocks {
			if n := signal(c); n == "CK_P" || n == "CLK_P" || n == "CK+" || n == "CLK+" {
				clock = c
				break
			}
		}
		extra := ""
		if len(clocks) > 1 {
			extra = fmt.Sprintf("; %d clock pairs here, so this link is probably split across a "+
				"connector -- the lanes are matched against %s", len(clocks), leaf(clock))
		}
		iface := &Interface{
			Kind: MIPI, Instance: inst, Nets: ns, Pairs: pairs,
			Name:      "MIPI D-PHY " + bracket(inst),
			IntraPair: Tolerance{MM: 0.1},
			Evidence:  fmt.Sprintf("a clock pair and %d data lane pair(s) (%s)%s", len(lanes), sample(ns, 4), extra),
			Groups: []Group{{
				Name: "data lanes to clock", Reference: clock, Members: lanes,
				Tolerance: Tolerance{MM: 0.5},
				Why: "D-PHY data lanes are sampled against the clock lane, so each lane is " +
					"matched to it. Intra-pair is the tighter requirement and is checked separately",
			}},
		}
		out = append(out, iface)
	}
	return out
}

func detectPCIe(nets []string) []*Interface {
	groups := byInstance(nets, func(s string) bool {
		return strings.HasPrefix(s, "PCIE") || strings.HasPrefix(s, "RX") ||
			strings.HasPrefix(s, "TX") || strings.HasPrefix(s, "COMBO") ||
			strings.Contains(s, "CLK")
	})
	var out []*Interface
	for inst, ns := range groups {
		if !strings.Contains(strings.ToUpper(inst), "PCIE") {
			// Only claim nets that say PCIe somewhere: a bare TX/RX pair could
			// be anything, and guessing is worse than not reporting.
			var says bool
			for _, n := range ns {
				if strings.Contains(strings.ToUpper(n), "PCIE") {
					says = true
					break
				}
			}
			if !says {
				continue
			}
		}
		pairs := FindPairs(ns)
		if len(pairs) == 0 {
			continue
		}
		var paired []string
		for _, p := range pairs {
			paired = append(paired, p.P, p.N)
		}
		sort.Strings(paired)
		out = append(out, &Interface{
			Kind: PCIe, Instance: inst, Nets: paired, Pairs: pairs,
			Name:      strings.TrimSpace("PCI Express " + bracketFor(inst, "PCIE")),
			IntraPair: Tolerance{MM: 0.127},
			Evidence:  fmt.Sprintf("%d differential pair(s) named for PCIe (%s)", len(pairs), sample(paired, 4)),
			// Deliberately no group: each lane recovers its own clock, so
			// lane-to-lane matching is not a requirement the way it is on a
			// source-synchronous bus. The pairs are the whole job.
		})
	}
	return out
}

var usbRE = regexp.MustCompile(`^(USB_?)?(D|DATA)(\+|-|_P|_N|P|N|M)$`)

func detectUSB(nets []string) []*Interface {
	byScope := map[string][]string{}
	for _, n := range nets {
		s := signal(n)
		if usbRE.MatchString(s) || strings.HasPrefix(s, "USB_D") || strings.HasPrefix(s, "USB_DATA") {
			key := instance(n)
			if key == "" {
				key = strings.TrimSuffix(strings.TrimPrefix(scope(n), "/"), "/")
			}
			byScope[key] = append(byScope[key], n)
		}
	}
	var out []*Interface
	for inst, ns := range byScope {
		sort.Strings(ns)
		pairs := FindPairs(ns)
		if len(pairs) == 0 {
			continue
		}
		var paired []string
		for _, p := range pairs {
			paired = append(paired, p.P, p.N)
		}
		sort.Strings(paired)
		out = append(out, &Interface{
			Kind: USB2, Instance: inst, Nets: paired, Pairs: pairs,
			Name:      strings.TrimSpace("USB " + bracketFor(inst, "USB")),
			IntraPair: Tolerance{MM: 0.15},
			Evidence:  fmt.Sprintf("a D+/D- pair (%s)", sample(paired, 2)),
		})
	}
	return out
}

var sdRE = regexp.MustCompile(`^(SDMMC|SD|EMMC|MMC)([0-9]*)_?(CK|CLK|CMD|D[0-7]|DS|RST[N]?)$`)

func detectSDMMC(nets []string) []*Interface {
	// Matched against the whole leaf name rather than the instance-stripped
	// one. "SDMMC1_CK" looks like instance SDMMC1 carrying a signal called CK,
	// and "GPIO.SDMMC3_CK" like instance GPIO carrying SDMMC3_CK -- the same
	// interface written two ways, and stripping the instance found only one of
	// them.
	groups := map[string][]string{}
	for _, n := range nets {
		m := sdRE.FindStringSubmatch(strings.ToUpper(leaf(n)))
		if m == nil {
			m = sdRE.FindStringSubmatch(signal(n))
		}
		if m == nil {
			continue
		}
		groups[m[1]+m[2]] = append(groups[m[1]+m[2]], n)
	}
	var out []*Interface
	for inst, ns := range groups {
		sort.Strings(ns)
		if len(ns) < 3 {
			continue
		}
		clock := clockLeaf(ns, `^(SDMMC|SD|EMMC|MMC)[0-9]*_?(CK|CLK)$`)
		if clock == "" {
			continue
		}
		var members []string
		for _, n := range ns {
			if n != clock {
				members = append(members, n)
			}
		}
		out = append(out, &Interface{
			Kind: SDMMC, Instance: inst, Nets: ns,
			Name:     "SD/eMMC " + bracket(inst),
			Evidence: fmt.Sprintf("a clock, and %d command and data net(s) (%s)", len(members), sample(ns, 4)),
			Groups: []Group{{
				Name: "command and data to clock", Reference: clock, Members: members,
				Tolerance: Tolerance{MM: 2.5},
				Why: "command and data are clocked by CLK, so they are matched to it. " +
					"2.5 mm suits the slower modes; HS400 eMMC is far tighter and the " +
					"controller's guide governs",
			}},
		})
	}
	return out
}

// ---- helpers ----

func clockOf(nets []string, pattern string) string {
	re := regexp.MustCompile(pattern)
	for _, n := range nets {
		if re.MatchString(signal(n)) {
			return n
		}
	}
	return ""
}

// bracket names the instance after the interface, unless the instance says
// nothing the name has not already said: "USB (USB)" helps nobody.
func bracket(s string) string {
	return bracketFor(s, "")
}

func bracketFor(s, family string) string {
	if s == "" {
		return ""
	}
	u := strings.ToUpper(s)
	if u == strings.ToUpper(family) {
		return ""
	}
	return "(" + s + ")"
}

// clockLeaf finds the clock by its whole leaf name.
func clockLeaf(nets []string, pattern string) string {
	re := regexp.MustCompile(pattern)
	for _, n := range nets {
		if re.MatchString(strings.ToUpper(leaf(n))) || re.MatchString(signal(n)) {
			return n
		}
	}
	return ""
}

func sample(nets []string, n int) string {
	var out []string
	for i, x := range nets {
		if i >= n {
			out = append(out, "...")
			break
		}
		out = append(out, leaf(x))
	}
	return strings.Join(out, ", ")
}

// Reassign applies a family to a set of nets, whatever they are called.
//
// Detection proposes; this is the user disposing. A board that names its PCIe
// lanes after the connector, or its camera link after the sensor, is not a
// board this tool can recognise -- and the user knows what it is. So the
// family's own structure is looked for in the names first, and where it is not
// there the nets are still given that family's limits and impedance targets,
// with the difference said plainly rather than papered over.
func Reassign(nets []string, kind Kind, name string) *Interface {
	sorted := append([]string{}, nets...)
	sort.Strings(sorted)

	// Try the family's own detector on these nets: if the structure is there,
	// the groups come out right.
	for _, d := range detectors {
		for _, got := range d(sorted) {
			if got.Kind != kind || len(got.Nets) < len(sorted) {
				continue
			}
			got.Nets = sorted
			got.Total = len(sorted)
			if name != "" {
				got.Name = name
			}
			got.Evidence = "assigned by hand; the names do carry this interface's structure, so its groups were read from them"
			return got
		}
	}

	// The structure is not in the names. The pairs still are -- a complement
	// suffix is a complement suffix -- and the family's limits still apply.
	f, known := FamilyOf(kind)
	iface := &Interface{
		Kind: kind, Nets: sorted, Total: len(sorted),
		Pairs: FindPairs(sorted),
		Name:  name,
	}
	if iface.Name == "" && known {
		iface.Name = f.Label
	}
	if known {
		iface.IntraPair = Tolerance{MM: f.IntraPairMM}
	}
	switch {
	case len(iface.Pairs) > 0:
		iface.Evidence = fmt.Sprintf(
			"assigned by hand. The names do not carry this interface's structure, so there are no "+
				"groups to match against a clock; its %d differential pair(s) are checked against "+
				"the family's intra-pair limit", len(iface.Pairs))
	default:
		iface.Evidence = "assigned by hand. The names carry neither this interface's structure nor " +
			"any differential pairs, so there is nothing here to match automatically -- say which " +
			"net is the reference to change that"
	}
	return iface
}

// WithReference turns a flat set of nets into a group matched to one of them.
//
// This is the smallest thing a user can say that makes an unrecognised
// interface actionable: which net is the clock. Everything else follows.
func (i *Interface) WithReference(ref string, limit Tolerance, name string) {
	var members []string
	for _, n := range i.Nets {
		if n != ref {
			members = append(members, n)
		}
	}
	if name == "" {
		name = "matched to " + leaf(ref)
	}
	i.Groups = []Group{{
		Name: name, Reference: ref, Members: members, Tolerance: limit,
		Why: "the reference was named by hand; every other net here is matched to it",
	}}
}

// ID identifies an interface for selection and for the user's overrides. It has
// to survive a re-read of the same board, so it is built from what the
// interface is rather than from where it came in a list.
func (i *Interface) ID() string {
	if i.Instance != "" {
		return string(i.Kind) + ":" + i.Instance
	}
	return string(i.Kind) + ":" + i.Name
}
