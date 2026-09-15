// Package report renders what the analyser found, for a terminal or a log.
//
// The output is meant to be read before anything is changed. It is the half of
// this tool that answers "which traces would you touch, and why", and it is
// deliberately explicit about what it cannot do: a shortfall that is reported
// is a shortfall the layout engineer has to deal with, and burying it would be
// worse than not running the tool.
package report

import (
	"fmt"
	"github.com/embeddedci-com/pcb-autorouter/pkglen"
	"io"
	"math"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/ddr"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
	"github.com/embeddedci-com/pcb-autorouter/proto"
	"github.com/embeddedci-com/pcb-autorouter/route"
	"github.com/embeddedci-com/pcb-autorouter/tune"
)

func short(net string) string {
	if i := strings.LastIndexByte(net, '/'); i >= 0 {
		net = net[i+1:]
	}
	return strings.TrimPrefix(net, "DDR_")
}

// Board prints what the board is, without reference to any one interface. It is
// what a board with no DDR on it still gets.
func Board(w io.Writer, b *board.Board, proj *board.Project) {
	fmt.Fprintf(w, "Board:       %s\n", b.Path)
	fmt.Fprintf(w, "Layers:      %d copper (%s), %.3f mm copper-to-copper\n",
		len(b.CopperLayers), strings.Join(b.CopperLayers, ", "), b.Stackup.Thickness)
	tracks, vias := 0, 0
	for _, t := range b.Tracks {
		if t.Kind == board.KindVia {
			vias++
			continue
		}
		tracks++
	}
	fmt.Fprintf(w, "Copper:      %d tracks, %d vias, %d pads on %d footprints\n",
		tracks, vias, len(b.Pads), len(b.Footprints))
	rules(w, proj)
}

func rules(w io.Writer, proj *board.Project) {
	if proj.HasCustomRules {
		fmt.Fprintf(w, "Rules:       %s plus a custom .kicad_dru\n", proj.Path)
	} else if proj.Path != "" {
		fmt.Fprintf(w, "Rules:       %s\n", proj.Path)
	}
}

// CustomRules says what was made of the board's own design rules.
//
// Worth a section of its own because a rule that relaxes a clearance changes
// where copper may go, and the reader needs to know which of theirs were read
// and which were not. A rule left alone is not a rule ignored quietly: the tool
// then works to the net classes at their strictest there, which is safe and
// sometimes means declining to route somewhere the board allows.
func CustomRules(w io.Writer, r *board.Rules) {
	if r == nil || len(r.List) == 0 {
		return
	}
	applied, skipped := r.Understood()
	fmt.Fprintf(w, "\nCustom design rules: %d read, %d left alone\n", applied, skipped)
	for _, rule := range r.List {
		if !rule.Understood {
			continue
		}
		fmt.Fprintf(w, "  %-28s clearance %.3f mm where %s\n",
			rule.Name, rule.ClearanceMin, rule.Condition)
	}
	for _, rule := range r.Skipped() {
		fmt.Fprintf(w, "  %-28s NOT applied: %s\n", rule.Name, rule.Why)
	}
	if skipped > 0 {
		fmt.Fprintln(w, "  Where a rule is not applied the net classes at their strictest are used")
		fmt.Fprintln(w, "  instead, which can only be stricter than the board asks for.")
	}
	fmt.Fprintln(w, "  KiCad is the authority on its own rule language. Verify with its DRC.")
}

// Interface prints what the classifier made of the board.
func Interface(w io.Writer, b *board.Board, proj *board.Project, iface *ddr.Interface) {
	fmt.Fprintf(w, "Board:       %s\n", b.Path)
	fmt.Fprintf(w, "Layers:      %d copper (%s), %.3f mm copper-to-copper\n",
		len(b.CopperLayers), strings.Join(b.CopperLayers, ", "), b.Stackup.Thickness)
	fmt.Fprintf(w, "Controller:  %s\n", iface.Controller)
	fmt.Fprintf(w, "Memory:      %s\n", strings.Join(iface.Devices, ", "))
	fmt.Fprintf(w, "Interface:   x%d in %d byte lanes, %d nets classified\n",
		iface.Width, iface.Lanes, len(iface.Signals))
	if proj.HasCustomRules {
		fmt.Fprintf(w, "Rules:       %s plus a custom .kicad_dru\n", proj.Path)
		fmt.Fprintln(w, "             Custom rules are not interpreted; clearance checks here use the")
		fmt.Fprintln(w, "             net classes at their strictest. Verify with KiCad's own DRC.")
	} else if proj.Path != "" {
		fmt.Fprintf(w, "Rules:       %s\n", proj.Path)
	}
	if len(iface.Unclassified) > 0 {
		fmt.Fprintf(w, "\nNot classified (%d):\n", len(iface.Unclassified))
		for _, n := range iface.Unclassified {
			fmt.Fprintf(w, "  %s\n", n)
		}
	}
	for _, n := range iface.Notes {
		fmt.Fprintf(w, "\nNote: %s\n", n)
	}
}

// Routing prints which nets are complete, and what is missing on the rest.
//
// This comes before the length report on purpose. A length is only meaningful
// on a routed net, and KiCad's own DRC does not flag these gaps: it reports no
// unconnected items for any DDR net on the demo board while a whole leg of the
// fly-by bus has no copper at all.
func Routing(w io.Writer, e *netlen.Engine, iface *ddr.Interface) (incomplete int) {
	nets := make([]string, 0, len(iface.Signals))
	for n := range iface.Signals {
		nets = append(nets, n)
	}
	sort.Strings(nets)

	type gap struct {
		net      string
		islands  [][]string
		measured float64
		total    float64
	}
	var gaps []gap
	for _, n := range nets {
		m := e.Measure(n)
		if m.Complete {
			continue
		}
		gaps = append(gaps, gap{n, m.Islands, m.Longest.Length, m.KiCadLength})
	}
	fmt.Fprintf(w, "\nRouting: %d of %d nets fully routed", len(nets)-len(gaps), len(nets))
	if len(gaps) == 0 {
		fmt.Fprintln(w)
		return 0
	}
	fmt.Fprintf(w, ", %d incomplete\n", len(gaps))

	// Group the incomplete nets by the shape of the gap, so that 25 nets with
	// the same problem read as one finding rather than 25.
	//
	// The component references are generalised first: every termination
	// resistor has a different designator, and grouping on those would split
	// one fact about the board into a list as long as the bus.
	role := map[string]string{iface.Controller: iface.Controller}
	for _, d := range iface.Devices {
		role[d] = d
	}
	generalise := func(ref string) string {
		if r, ok := role[ref]; ok {
			return r
		}
		return "termination"
	}
	byShape := map[string][]string{}
	for _, g := range gaps {
		var parts []string
		for _, isle := range g.islands {
			seen := map[string]bool{}
			var rs []string
			for _, id := range isle {
				r := generalise(id[:strings.IndexByte(id+".", '.')])
				if !seen[r] {
					seen[r] = true
					rs = append(rs, r)
				}
			}
			sort.Strings(rs)
			parts = append(parts, strings.Join(rs, "+"))
		}
		sort.Strings(parts)
		key := strings.Join(parts, " | ")
		byShape[key] = append(byShape[key], short(g.net))
	}
	keys := make([]string, 0, len(byShape))
	for k := range byShape {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(byShape[keys[i]]) > len(byShape[keys[j]]) })
	for _, k := range keys {
		nets := byShape[k]
		sort.Strings(nets)
		fmt.Fprintf(w, "  %d net(s) split into: %s\n", len(nets), k)
		fmt.Fprintf(w, "      %s\n", wrap(nets, 68, "      "))
	}
	fmt.Fprintln(w, "  A length can only be matched over copper that exists. Nets above are")
	fmt.Fprintln(w, "  measured over the legs that are routed, and their missing legs are not")
	fmt.Fprintln(w, "  counted. KiCad's DRC does not report these gaps.")
	return len(gaps)
}

// Topology states the fly-by chain and what the board has of it.
//
// This goes above everything about lengths, because it decides whether any of
// those lengths mean anything. DDR3 and DDR4 address, command, control and
// clock are fly-by: one net leaves the controller, reaches the first device,
// carries on to the next, and ends in a termination resistor. It is not a
// preference. The topology is what makes write levelling work -- the controller
// compensates for the skew the chain deliberately introduces -- and a T or a
// star instead puts a stub on every one of those nets, whose reflections are
// what the fly-by arrangement exists to avoid.
//
// So a board missing a hop has not routed its address bus, however finished the
// copper that is there looks. And it is easy to miss: KiCad's DRC reports no
// unconnected item for any of the 27 nets on the demo board, because each one
// is a net with copper on it. Saying it plainly is the point of this section.
func Topology(w io.Writer, chain *ddr.Chain) {
	if chain == nil || len(chain.Hops) == 0 {
		return
	}
	fmt.Fprintf(w, "\nFly-by chain: %s\n", strings.Join(chain.Order, " -> ")+" -> termination")

	done, missing := 0, 0
	for _, h := range chain.Hops {
		switch {
		case h.Routed():
			done++
			fmt.Fprintf(w, "  %-24s routed on all %d net(s)\n", h.From+" -> "+h.To, h.Of)
		case h.Nets == 0:
			missing++
			fmt.Fprintf(w, "  %-24s NOT ROUTED on any of %d net(s)\n", h.From+" -> "+h.To, h.Of)
		default:
			missing++
			fmt.Fprintf(w, "  %-24s routed on %d of %d net(s)\n", h.From+" -> "+h.To, h.Nets, h.Of)
		}
	}
	if chain.OrderFrom != "" {
		fmt.Fprintf(w, "  %s.\n", chain.OrderFrom)
	}
	if chain.Note != "" {
		fmt.Fprintf(w, "  %s.\n", chain.Note)
	}
	if missing == 0 {
		return
	}
	fmt.Fprintln(w, "  Address, command, control and clock have to be fly-by: through each device in")
	fmt.Fprintln(w, "  turn and terminated at the end. It is what makes write levelling work, and a T")
	fmt.Fprintln(w, "  or a star instead leaves a stub on every one of these nets. So the hops above")
	fmt.Fprintln(w, "  that are not routed are not an omission to tidy up later -- until they exist")
	fmt.Fprintln(w, "  this bus has no topology to match, and what is reported below is the matching")
	fmt.Fprintln(w, "  of the hops that do exist. Those figures stay true as far as they go; they do")
	fmt.Fprintln(w, "  not become the board's address timing until the chain is finished.")
	fmt.Fprintln(w, "  KiCad's DRC reports no unconnected item for any of these nets: each one has")
	fmt.Fprintln(w, "  copper on it, and connectivity between the pieces is not what it checks.")
}

// Interfaces lists everything on the board this tool recognises.
//
// The tool began as a DDR matcher, and DDR is the hardest case rather than the
// only one: Ethernet, USB, PCIe, MIPI and SD all want the same measurement over
// simpler shapes. What is not the same is knowing what to measure against what,
// which is what recognising the interface buys.
//
// Recognition is by net name, so every line says what it matched on. Nothing in
// the geometry says a pair is PCIe rather than SATA, and a tool that hid that
// would be claiming more than it knows.
func Interfaces(w io.Writer, found []*proto.Interface, m proto.Measurer) {
	if len(found) == 0 {
		return
	}
	fmt.Fprintf(w, "\nInterfaces on this board: %d\n", len(found))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, i := range found {
		a := i.Assess(m)
		mark := " "
		if a.Actionable() {
			mark = "*"
		}
		fmt.Fprintf(tw, "  %s %-28s\t%-13s\t%3d nets, %d routed\t%s\t\n",
			mark, i.Name, i.Kind, i.Total, i.Routed, a.Summary)
	}
	tw.Flush()
	fmt.Fprintln(w, "  Recognized from the net names, so each one is a reading rather than a proof:")
	for _, i := range found {
		fmt.Fprintf(w, "    %-28s %s\n", i.Name, i.Evidence)
	}
}

// InterfaceDetail prints one interface's measurements.
func InterfaceDetail(w io.Writer, i *proto.Interface, m proto.Measurer) {
	a := i.Assess(m)
	fmt.Fprintf(w, "\n%s -- %s\n", i.Name, a.Summary)
	if i.Planner != "" {
		fmt.Fprintf(w, "  Handled by %s.\n", i.Planner)
	}
	for gi, g := range a.Groups {
		fmt.Fprintf(w, "  %s, matched to %s (%.3f mm), limit %.3f mm: spread %.3f mm, %d out of tolerance",
			g.Name, short(g.Reference), g.ReferenceMM, g.LimitMM, g.SpreadMM, g.OutOfTol)
		if g.Unroutable > 0 {
			fmt.Fprintf(w, ", %d not fully routed", g.Unroutable)
		}
		fmt.Fprintln(w)
		if gi < len(i.Groups) && i.Groups[gi].Why != "" {
			fmt.Fprintf(w, "      %s.\n", i.Groups[gi].Why)
		}
	}
	if len(a.Pairs) == 0 {
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range a.Pairs {
		if !p.Routed {
			fmt.Fprintf(tw, "  %-18s\t%s\t\n", proto.Leaf(p.Base), "not joined end to end yet")
			continue
		}
		flag := ""
		if !p.InTolerance {
			flag = "  <- out of tolerance"
		}
		fmt.Fprintf(tw, "  %-18s\tskew %.3f mm\tlimit %.3f mm\t%s\t\n",
			proto.Leaf(p.Base), p.SkewMM, p.LimitMM, flag)
	}
	tw.Flush()
}

// Routes reports the copper the router laid, and what it could not.
//
// New copper is the one change here that adds a connection rather than
// lengthening one, so the report names every piece of it: how long, how many
// vias, and how many paths were tried before the geometry accepted one. A route
// that took several attempts is not a worse route -- the grid proposes, the
// clearance checker disposes -- but it does say the corridor was tight, which
// is worth knowing before trusting the result to a fabricator.
func Routes(w io.Writer, results []*route.Result) {
	if len(results) == 0 {
		return
	}
	var routed, tries, vias int
	var length float64
	for _, r := range results {
		tries += r.Attempts
		if !r.Routed {
			continue
		}
		routed++
		vias += r.Vias
		length += r.LengthMM
	}
	fmt.Fprintf(w, "  %d of %d connection(s) made: %.3f mm of new copper and %d via(s), over %d path(s) tried.\n",
		routed, len(results), length, vias, tries)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range results {
		if !r.Routed {
			continue
		}
		note := ""
		if r.Attempts > 1 || r.RippedUp > 0 {
			note = fmt.Sprintf("  (%d paths tried", r.Attempts)
			if r.RippedUp > 0 {
				note += fmt.Sprintf(", %d route(s) moved aside", r.RippedUp)
			}
			note += ")"
		}
		fmt.Fprintf(tw, "  %-10s\t%s -> %s\t%.3f mm\t%d via(s)\t%s\t\n",
			short(r.Net), r.From, r.To, r.LengthMM, r.Vias, note)
	}
	tw.Flush()

	var failed []*route.Result
	for _, r := range results {
		if !r.Routed {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		return
	}
	fmt.Fprintf(w, "  %d connection(s) could not be made:\n", len(failed))
	for _, r := range failed {
		fmt.Fprintf(w, "    %-10s %s -> %s: %s\n", short(r.Net), r.From, r.To, r.Reason)
	}
	fmt.Fprintln(w, "  Nothing was written for those. A hop the router cannot find a way through is")
	fmt.Fprintln(w, "  a placement or a layer decision, not a routing one.")
}

// Plan prints the matching plan, group by group.
//
// The "room" column is what separates a shortfall that could be fixed from one
// that cannot. It is only filled in when the caller measured the headroom,
// which is the slow half of the analysis.
func Plan(w io.Writer, p *ddr.Plan) {
	fmt.Fprintf(w, "\nMatching rules: DQ/DM to strobe %s, within a pair %s, address/command to clock %s",
		p.Rules.DataToStrobe, p.Rules.IntraPair, p.Rules.AddressToClock)
	if p.Rules.ClockOffsetPercent != 0 {
		fmt.Fprintf(w, ", clock offset %+.2f%%", p.Rules.ClockOffsetPercent)
	}
	fmt.Fprintln(w)

	for _, g := range p.Groups {
		fmt.Fprintf(w, "\n%s\n", strings.ToUpper(g.Name))
		// The target first, and what it is made of: every offset below is
		// against it, so a reader has to be able to see where it comes from.
		if len(g.ReferenceMembers) > 0 {
			var parts []string
			for _, r := range g.ReferenceMembers {
				parts = append(parts, fmt.Sprintf("%s %.3f", short(r.Net), r.Length))
			}
			how := "the mean of " + strings.Join(parts, " and ")
			if len(parts) == 1 {
				how = parts[0]
			}
			offset := ""
			if math.Abs(g.Target-g.ReferenceLength) > 1e-9 {
				offset = fmt.Sprintf(", with the clock offset %+.3f mm", g.Target-g.ReferenceLength)
			}
			fmt.Fprintf(w, "  target %.3f mm = %s%s; tolerance +/-%.3f mm; spread now %.3f mm\n",
				g.Target, how, offset, g.ToleranceMM, g.SpreadBefore())
		} else {
			fmt.Fprintf(w, "  target %.3f mm = the longest member (no reference routed); tolerance +/-%.3f mm; spread now %.3f mm\n",
				g.Target, g.ToleranceMM, g.SpreadBefore())
		}

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  net\tlength mm\tdelay ps\tvs target\tadd mm\troom mm\t")
		ms := append([]ddr.Member{}, g.Members...)
		sort.Slice(ms, func(i, j int) bool {
			if ms[i].Routed != ms[j].Routed {
				return ms[j].Routed
			}
			return ms[i].Length < ms[j].Length
		})
		reroute, long := 0, 0
		toAdd := 0.0
		for _, m := range ms {
			if !m.Routed {
				fmt.Fprintf(tw, "  %s\t-\t-\tnot routed to this device\t-\t-\t\n", short(m.Net))
				continue
			}
			flag := ""
			room := "-"
			switch {
			case m.Reference:
				flag = "  <- reference"
			case m.InTolerance:
			case m.Excess > 0:
				long++
				flag = fmt.Sprintf("  <- %.3f mm too long; route it shorter", m.Excess)
			case m.NeedsReroute():
				reroute++
				flag = fmt.Sprintf("  <- needs %.0f%% of its own length; reroute", 100*m.Need/m.Length)
			default:
				flag = "  <- out of tolerance"
				if m.Headroom > 0 || m.Need > 0 {
					room = fmt.Sprintf("%.3f", m.Headroom)
					if m.Headroom+1e-6 < m.Need {
						flag = "  <- out of tolerance, not enough room"
					}
				}
			}
			// Nothing to add to a member already inside the band.
			add := "-"
			if !m.InTolerance && m.Need > 0 {
				add = fmt.Sprintf("%.3f", m.Need)
				toAdd += m.Need
			}
			fmt.Fprintf(tw, "  %s\t%.3f\t%.1f\t%+.3f\t%s\t%s\t%s\n",
				short(m.Net), m.Length, m.Delay, m.Deviation, add, room, flag)
		}
		tw.Flush()
		if toAdd > 1e-6 {
			fmt.Fprintf(w, "  %d of %d out of tolerance; %.3f mm to add\n",
				g.OutOfTolerance(), len(g.Members), toAdd)
		} else {
			fmt.Fprintf(w, "  %d of %d out of tolerance\n", g.OutOfTolerance(), len(g.Members))
		}
		if reroute > 0 {
			fmt.Fprintf(w, "  %d net(s) are asking for more than a quarter of their own length. A meander folds\n", reroute)
			fmt.Fprintf(w, "  extra path into the space beside a track; it cannot make a route that much longer.\n")
			fmt.Fprintf(w, "  Those need routing differently, not tuning.\n")
		}
		if long > 0 {
			fmt.Fprintf(w, "  %d net(s) are longer than the target; nothing here shortens a track.\n", long)
		}
		for _, n := range g.Notes {
			fmt.Fprintf(w, "  note: %s\n", n)
		}
	}

	if len(p.Skipped) > 0 {
		fmt.Fprintln(w, "\nNot matched:")
		reasons := map[string][]string{}
		for net, why := range p.Skipped {
			reasons[why] = append(reasons[why], short(net))
		}
		keys := make([]string, 0, len(reasons))
		for k := range reasons {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sort.Strings(reasons[k])
			fmt.Fprintf(w, "  %s: %s\n", k, strings.Join(reasons[k], ", "))
		}
	}
}

// Headroom prints, per group, what is needed against what the board can hold,
// and how much spare room the buses have.
//
// This is the part that tells a layout engineer what to do next. A group short
// of room in a bus with spare space is worth re-spacing; a group asking for
// more length than its routes could ever carry is a reroute, and no setting
// here changes that.
func Headroom(w io.Writer, p *ddr.Plan, sp BundleSparer) {
	type row struct {
		name                              string
		need, room, gettable, spare, span float64
		reroute                           int
	}
	var rows []row
	for _, g := range p.Groups {
		r := row{name: g.Name}
		var nets []string
		for _, m := range g.Members {
			if m.Routed {
				nets = append(nets, m.Net)
			}
			if !m.Routed || m.InTolerance || m.Need <= 1e-6 {
				continue
			}
			r.need += m.Need
			r.room += m.Headroom
			// Room is not transferable between nets. A lane where one net has
			// 24 mm of room and seven have none is not nearly covered, however
			// the totals add up, so what is actually gettable is summed per net.
			r.gettable += math.Min(m.Need, m.Headroom)
			if m.NeedsReroute() {
				r.reroute++
			}
		}
		if r.need <= 1e-6 {
			continue
		}
		if sp != nil {
			r.spare, r.span = sp.BundleSpare(nets)
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "\nHow much of this is achievable")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  group\tneeded mm\tgettable mm\tcovered\tbus spare mm\treroute\t")
	for _, r := range rows {
		fmt.Fprintf(tw, "  %s\t%.1f\t%.1f\t%.0f%%\t%.2f\t%d\t\n",
			r.name, r.need, r.gettable, 100*r.gettable/r.need, r.spare, r.reroute)
	}
	tw.Flush()
	fmt.Fprintln(w, "  gettable: the room each net that needs length actually has, capped at what it needs,")
	fmt.Fprintln(w, "  and summed. Room beside one net cannot be lent to another, so the totals are not")
	fmt.Fprintln(w, "  interchangeable. bus spare: clear space the buses could be spread into, which bounds")
	fmt.Fprintln(w, "  what re-spacing them could add. reroute: nets asking for more than a quarter of their")
	fmt.Fprintln(w, "  own length, which no meander can supply.")
}

// BundleSparer can say how much room the buses a set of nets run in have to
// spare. tune.Tuner satisfies it.
type BundleSparer interface {
	BundleSpare(nets []string) (spare, span float64)
}

// IntraPairSkew prints the measured skew inside each differential pair.
func IntraPairSkew(w io.Writer, p *ddr.Plan) {
	skew := p.IntraPairSkew()
	if len(skew) == 0 {
		return
	}
	fmt.Fprintln(w, "\nDifferential pair skew (for information: AN5724 sets no limit, and one half of a pair is never lengthened)")
	keys := make([]string, 0, len(skew))
	for k := range skew {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %-24s %.3f mm\n", short(k), skew[k])
	}
}

// Changes prints what tuning achieved, and what it could not.
func Changes(w io.Writer, results []*tune.Result) (met, short int, added, missing float64) {
	perLeg := false
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  net\tasked mm\tadded mm\tshort mm\tmeanders\t")
	sort.Slice(results, func(i, j int) bool { return results[i].Shortfall > results[j].Shortfall })
	for _, r := range results {
		if r.Requested <= 0 {
			continue
		}
		if r.Shortfall <= 1e-4 {
			met++
		} else {
			short++
		}
		added += r.Added
		missing += r.Shortfall
		name := short2(r.Net)
		if r.Leg != "" {
			name += " " + r.Leg
			perLeg = true
		}
		fmt.Fprintf(tw, "  %s\t%.3f\t%.3f\t%.3f\t%d\t\n",
			name, r.Requested, r.Added, r.Shortfall, r.Meanders)
	}
	tw.Flush()
	// Counted per leg where a net is matched over more than one, because that
	// is what was asked for and what was either met or not: a net can be
	// matched on one leg of the fly-by chain and still short on the next.
	unit := "net(s)"
	if perLeg {
		unit = "leg(s)"
	}
	fmt.Fprintf(w, "  %d %s met, %d short; %.3f mm added, %.3f mm still missing\n",
		met, unit, short, added, missing)
	seen := map[string]bool{}
	for _, r := range results {
		for _, n := range r.Notes {
			if !seen[n] {
				seen[n] = true
			}
		}
	}
	return met, short, added, missing
}

func short2(net string) string { return short(net) }

func wrap(items []string, width int, indent string) string {
	var b strings.Builder
	line := 0
	for i, s := range items {
		if line > 0 && line+len(s)+2 > width {
			b.WriteString("\n" + indent)
			line = 0
		}
		if line > 0 {
			b.WriteString(", ")
			line += 2
		}
		b.WriteString(s)
		line += len(s)
		_ = i
	}
	return b.String()
}

// Expansion prints what spreading the buses did, or would do.
//
// It names the traces that moved, because that is the part a reviewer needs to
// know: spreading a bus moves every trace in it, not only the ones that were
// selected for lengthening. Each keeps its own endpoints and the traces keep
// their order, so nothing is rerouted -- but a diff will show copper the user
// did not pick, and it is better to say so than to let them find it.
func Expansion(w io.Writer, results []*tune.ExpandResult, preview bool) {
	var opened, useful, added float64
	var spread int
	for _, r := range results {
		if r.Skipped != "" {
			continue
		}
		spread++
		opened += r.OpenedMM
		useful += r.UsefulMM
		added += r.AddedMM
	}
	if len(results) == 0 {
		fmt.Fprintln(w, "  no buses found among these nets to spread")
		return
	}
	verb := "were spread"
	if preview {
		verb = "would be spread"
	}
	fmt.Fprintf(w, "  %d of %d bus(es) %s. Stepping the traces aside adds %.3f mm of length outright and\n",
		spread, len(results), verb, added)
	fmt.Fprintf(w, "  opens %.3f mm of room between them for meanders, worth an estimated %.3f mm toward\n", opened, useful)
	fmt.Fprintf(w, "  what these traces needed; the rest falls on traces that did not need it or could\n")
	fmt.Fprintf(w, "  already reach the space anyway.\n")

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range results {
		if r.Skipped != "" {
			fmt.Fprintf(tw, "  %s %s, %.2f mm span\t-\t%s\t\n", r.Group, r.Layer, r.SpanMM, r.Skipped)
			continue
		}
		var who []string
		for _, m := range r.Members {
			who = append(who, fmt.Sprintf("%s %+.2f", short(m.Net), m.DisplacementMM))
		}
		fmt.Fprintf(tw, "  %s %s, %.2f mm span\t+%.3f mm, %.3f mm room, worth %.3f\tmoved: %s\t\n",
			r.Group, r.Layer, r.SpanMM, r.AddedMM, r.OpenedMM, r.UsefulMM, strings.Join(who, ", "))
	}
	tw.Flush()
	if spread > 0 && !preview {
		fmt.Fprintln(w, "  Every trace in a spread bus moved, not only the selected ones. Each kept its own")
		fmt.Fprintln(w, "  endpoints and the traces kept their order, so nothing was rerouted.")
	}
}

// Checks prints the requirements across groups: each byte lane's strobe
// against the clock at its device, and one device's lanes against another's.
func Checks(w io.Writer, p *ddr.Plan) {
	cs := p.Checks()
	if len(cs) == 0 {
		return
	}
	fmt.Fprintln(w, "\nAcross groups")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  check\tdifference mm\tlimit mm\t\t")
	for _, c := range cs {
		verdict := "ok"
		if !c.OK {
			verdict = "<- outside the limit"
		}
		sign := fmt.Sprintf("%+.3f", c.ValueMM)
		if c.Kind == "chip-delta" {
			sign = fmt.Sprintf("%.3f", c.ValueMM)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%.2f\t%s\t\n", c.Name, sign, c.LimitMM, verdict)
	}
	tw.Flush()
	for _, c := range cs {
		fmt.Fprintf(w, "  %s: %s\n", c.Name, c.Detail)
	}
}

// Layers prints where the routes use other layers than the guide expects.
// Observations, not failures: a board may be built this way on purpose.
func Layers(w io.Writer, b *board.Board, p *ddr.Plan) {
	fs := p.LayerFindings(b)
	if len(fs) == 0 {
		return
	}
	fmt.Fprintln(w, "\nLayer use (for information; ST AN5724 expects otherwise)")
	for _, f := range fs {
		fmt.Fprintf(w, "  expected: %s\n", f.Expect)
		for _, n := range f.Nets {
			layers := make([]string, 0, len(n.ByLayer))
			for l := range n.ByLayer {
				layers = append(layers, l)
			}
			sort.Strings(layers)
			parts := make([]string, 0, len(layers))
			for _, l := range layers {
				parts = append(parts, fmt.Sprintf("%s %.1f mm", l, n.ByLayer[l]))
			}
			fmt.Fprintf(w, "    %-12s %s\n", short(n.Net), strings.Join(parts, ", "))
		}
	}
}

// PackageLengths says whose lengths include the package, or that the
// controller's do not.
func PackageLengths(w io.Writer, pkgs []pkglen.Applied, controller string) {
	fmt.Fprintln(w, "\nPackage lengths")
	has := false
	for _, a := range pkgs {
		if a.FromBoard > 0 {
			fmt.Fprintf(w, "  %s: %d pad(s) with a die length in the footprint\n", a.Ref, a.FromBoard)
		}
		if a.Pads > 0 {
			fmt.Fprintf(w, "  %s: %d pad(s) from the %s table (%s)\n", a.Ref, a.Pads, a.Part, a.Source)
		}
		if a.Ref == controller {
			has = true
		}
	}
	if !has {
		fmt.Fprintf(w, "  none for %s: lengths are board copper only, while DDR limits are on package plus board\n", controller)
	}
}
