// Command pcb-trace-length-analyzer reports on and length-matches the DDR routing in a
// KiCad board.
//
// It runs in two halves, on purpose. The first reads the board and prints what
// it found: which nets make up the interface, which of them are actually
// routed, how far each sits from the length it should be, and what it would
// change. Nothing is written. The second applies those changes, and only after
// the first half has been read and confirmed.
//
// The split is not ceremony. The analysis is where the judgement is -- whether
// the nets were grouped correctly, whether the tolerances are the right ones
// for the speed grade, whether an unrouted leg means the board is not ready --
// and none of that can be checked from a diff afterwards.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/ddr"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
	"github.com/embeddedci-com/pcb-autorouter/pkglen"
	"github.com/embeddedci-com/pcb-autorouter/proto"
	"github.com/embeddedci-com/pcb-autorouter/report"
	"github.com/embeddedci-com/pcb-autorouter/route"
	"github.com/embeddedci-com/pcb-autorouter/tune"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "pcb-trace-pcb-trace-length-analyzer: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	prefix     string
	controller string
	out        string
	apply      bool
	yes        bool
	dryRun     bool

	dataTolMM     float64
	strobeClockMM float64
	chipDeltaMM   float64
	dataTolPS     float64
	pairTolMM     float64
	pairTolPS     float64
	addrTolMM     float64
	addrTolPS     float64
	clockPct      float64
	withCtrl      bool
	maxPairFix    float64

	expand  bool
	maxDisp float64
	minOpen float64

	route       bool
	routeLayers string
	viaCost     float64
	ripUp       int

	openClr    float64
	openMargin float64

	// dru holds the board's custom design rules, loaded once and handed to
	// everything that asks the geometry a question.
	dru *board.Rules

	maxAmp    float64
	minAmp    float64
	gap       float64
	chamfer   float64
	minRun    float64
	padKeep   float64
	onlyNets  string
	onlyGroup string
	onlyIface string
	areas     areaList
	groupTol  groupTolList
}

func run(args []string, stdout *os.File, stdin *os.File) error {
	var o options
	fs := flag.NewFlagSet("pcb-trace-length-analyzer", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprint(stdout, `pcb-trace-length-analyzer -- a PCB trace length analyzer for KiCad boards

  pcb-trace-length-analyzer [flags] <board.kicad_pcb>

Reads the board and reports the interfaces on it, how far each net is from the
length it should be, and what to do about it. It writes nothing: -apply is the
experimental half that folds the meanders in, and it asks before it does.

DDR is read in full, down to each memory of the fly-by chain. Every other interface
it recognises -- Ethernet, USB, PCIe, MIPI, SD, and any pair of complements --
is measured and reported.

`)
		fs.PrintDefaults()
		fmt.Fprint(stdout, `
Examples:
  pcb-trace-length-analyzer board.kicad_pcb
        report only

  pcb-trace-length-analyzer -apply -out tuned.kicad_pcb board.kicad_pcb
        report, ask, then write the result to a new file

  pcb-trace-length-analyzer -apply -only-group "byte lane 3" -yes board.kicad_pcb
        tune one group without asking, writing board.tuned.kicad_pcb

  pcb-trace-length-analyzer -clock-offset 2.5 board.kicad_pcb
        aim the address and command group 2.5% longer than the clock

  pcb-trace-length-analyzer -apply -expand board.kicad_pcb
        where a bus is too tight to meander, spread it sideways into clear
        space first. This moves every trace in that bus, not only the ones
        selected; each keeps its own endpoints and the traces keep their order.

  pcb-trace-length-analyzer -apply -route board.kicad_pcb
        create the copper for the fly-by hops the board is missing. This is the
        only thing here that adds a connection rather than lengthening one, so
        it is opt-in and it says exactly what it laid. Until the chain exists
        there is no topology to match.

  pcb-trace-length-analyzer -apply -area 60,60,95,95 board.kicad_pcb
        only add copper inside that region. Room beside a trace is not the same
        as somewhere you want copper -- it can be in a BGA fanout, under a
        connector, or across a split in a plane, and a clearance check cannot
        tell the difference. Repeat -area for several. The web front end lets
        you draw them on the board instead.

  pcb-trace-length-analyzer -open-clearance 0.25 board.kicad_pcb
        keep 0.25 mm between nets away from the components, whatever the board
        says. A fine-pitch BGA forces a tight clearance onto the whole board
        because that is what makes the escape possible; in the open there is
        usually more room, and using it means less coupling and more space to
        meander into. Around the components the board's own rules still stand,
        and a differential pair is exempt in both directions. Pass 0 to hold
        to the board's rules everywhere.

After applying, verify with KiCad's own design rule check:
  kicad-cli pcb drc --format json --output drc.json tuned.kicad_pcb
`)
	}
	fs.StringVar(&o.prefix, "prefix", "", "only consider nets with this prefix, e.g. /ddr4/ (default: guess)")
	fs.StringVar(&o.controller, "controller", "", "reference of the memory controller (default: infer)")
	fs.StringVar(&o.out, "out", "", "where to write the result (default: <board>.tuned.kicad_pcb)")
	fs.BoolVar(&o.apply, "apply", false, "make the changes; without this nothing is written")
	fs.BoolVar(&o.yes, "yes", false, "skip the confirmation prompt (for scripts)")
	fs.BoolVar(&o.dryRun, "dry-run", false, "with -apply, tune in memory and report but do not write")

	fs.Float64Var(&o.dataTolMM, "data-tol-mm", ddr.DefaultRules().DataToStrobe.MM, "DQ/DM to strobe tolerance in mm (0 to disable)")
	fs.Float64Var(&o.dataTolPS, "data-tol-ps", 0, "DQ/DM to strobe tolerance in ps (tighter of the two wins)")
	fs.Float64Var(&o.pairTolMM, "pair-tol-mm", 0.127, "intra-pair tolerance in mm")
	fs.Float64Var(&o.pairTolPS, "pair-tol-ps", 0, "intra-pair tolerance in ps")
	fs.Float64Var(&o.addrTolMM, "addr-tol-mm", ddr.DefaultRules().AddressToClock.MM, "address/command to clock tolerance in mm")
	fs.Float64Var(&o.strobeClockMM, "strobe-clock-tol-mm", ddr.DefaultRules().StrobeToClock.MM,
		"each byte lane's DQS pair to the clock at its device, in mm (0 to skip the check)")
	fs.Float64Var(&o.chipDeltaMM, "chip-delta-mm", ddr.DefaultRules().MaxChipDeltaMM,
		"most one device's byte lanes may differ from another's, in mm (0 to skip the check)")
	fs.Float64Var(&o.addrTolPS, "addr-tol-ps", 0, "address/command to clock tolerance in ps")
	fs.Float64Var(&o.clockPct, "clock-offset", 0, "make address/command this % longer than the clock")
	fs.BoolVar(&o.withCtrl, "with-control", false, "include reset-like control lines in the address group")
	fs.Float64Var(&o.maxPairFix, "max-pair-fix", 1.0, "most length that may be padded into one half of a pair, mm")

	fs.BoolVar(&o.expand, "expand", false,
		"spread the buses sideways into clear space first, to make room for meandering where there is none")
	fs.Float64Var(&o.maxDisp, "max-shift", 2.0, "with -expand, how far one trace may be moved sideways, mm")
	fs.Float64Var(&o.minOpen, "min-open", 0.2, "with -expand, the least a bus must promise to open before it is disturbed, mm")
	fs.BoolVar(&o.route, "route", false,
		"create the copper for the fly-by hops the board is missing, so the chain exists to be matched")
	fs.StringVar(&o.routeLayers, "route-layers", "",
		"comma-separated layers the router may use (default: the layers the board already routes on)")
	fs.Float64Var(&o.viaCost, "via-cost", 6.0, "how much detour, in mm, is worth taking to avoid a via")
	fs.IntVar(&o.ripUp, "rip-up", 4, "how many times a route may be torn up and laid again to let another through")

	fs.Float64Var(&o.openClr, "open-clearance", 0.2,
		"clearance to hold to away from the components, mm, overriding the board's own rules there (0 to use them)")
	fs.Float64Var(&o.openMargin, "open-margin", 1.0,
		"how far outside a component's pads still counts as being around it, mm")

	fs.Float64Var(&o.maxAmp, "max-amplitude", 1.5, "how far a meander may stray from the track, mm")
	fs.Float64Var(&o.minAmp, "min-amplitude", 0.12, "smallest meander excursion worth drawing, mm")
	fs.Float64Var(&o.gap, "meander-gap", 3.0, "clear space between meander legs, in track widths")
	fs.Float64Var(&o.chamfer, "meander-chamfer", 1.0, "corner mitre, in track widths")
	fs.Float64Var(&o.minRun, "min-run", 0.8, "shortest straight track worth meandering, mm")
	fs.Float64Var(&o.padKeep, "pad-keepout", 0.3, "keep meanders this far from pads, mm")
	fs.StringVar(&o.onlyNets, "only-nets", "", "comma-separated net names or suffixes to tune")
	fs.StringVar(&o.onlyGroup, "only-group", "", "tune only this group, e.g. \"byte lane 3\"")
	fs.Var(&o.groupTol, "group-tol",
		"hold one group to its own tolerance, as \"byte lane 0=0.2\" in mm; repeatable")
	fs.Var(&o.areas, "area",
		"only add copper inside this region, as x0,y0,x1,y1 in board millimetres; repeatable")
	fs.StringVar(&o.onlyIface, "interface", "",
		"report one interface in detail, by name or kind, e.g. \"pcie\" or \"Ethernet RGMII (ETH1)\"")

	if err := fs.Parse(args); err != nil {
		return nil // flag package already reported it
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one board file")
	}
	return analyse(fs.Arg(0), o, stdout, stdin)
}

func analyse(path string, o options, w *os.File, in *os.File) error {
	b, err := board.Load(path)
	if err != nil {
		return err
	}
	proj, err := board.LoadProject(path)
	if err != nil {
		return err
	}
	b.UseHeightForLength = proj.UseHeightForLength

	// The board's own custom rules, where they can be read. Without them the
	// tool works to the net classes at their strictest, which is safe and,
	// once it is creating copper rather than only lengthening it, sometimes
	// impossible: a BGA ball cannot be escaped at 0.2 mm when the board says
	// 0.1 mm is allowed there.
	dru, err := board.LoadRules(path, b)
	if err != nil {
		return err
	}

	o.dru = dru
	pkgs := pkglen.Apply(b)
	engine := netlen.New(b)

	// What the board carries, whatever it carries. This runs first and on
	// every board: a board with no DDR on it is not a board this tool has
	// nothing to say about, and refusing it was the last thing left over from
	// when DDR was the only interface it knew.
	found := proto.Detect(b)

	prefix, scoped := ddr.Scope(b, o.prefix)
	if prefix == "" && scoped == nil {
		report.Board(w, b, proj)
		report.CustomRules(w, dru)
		report.Interfaces(w, found, engine)
		reportInterfaceDetail(w, found, engine, o.onlyIface)
		fmt.Fprintln(w, "\nNo DDR on this board, so there is no length matching to do yet: the")
		fmt.Fprintln(w, "interfaces above are measured but only DDR is planned and tuned so far.")
		return nil
	}
	if o.prefix == "" && prefix != "" {
		fmt.Fprintf(w, "Using net prefix %q (override with -prefix)\n", prefix)
	} else if scoped != nil {
		fmt.Fprintf(w, "Using the %d nets recognised as DDR (override with -prefix)\n", len(scoped))
	}

	iface, err := ddr.Classify(b, ddr.Options{NetPrefix: prefix, Nets: scoped, Controller: o.controller})
	if err != nil {
		return err
	}

	report.Interface(w, b, proj, iface)
	report.PackageLengths(w, pkgs, iface.Controller)
	report.CustomRules(w, dru)
	report.Routing(w, engine, iface)

	// Everything else the board carries. DDR is the rest of this report; these
	// are the buses that want the same treatment and have not had it yet.
	report.Interfaces(w, found, engine)
	reportInterfaceDetail(w, found, engine, o.onlyIface)

	rules := ddr.Rules{
		DataToStrobe:       ddr.Tolerance{MM: o.dataTolMM, PS: o.dataTolPS},
		IntraPair:          ddr.Tolerance{MM: o.pairTolMM, PS: o.pairTolPS},
		AddressToClock:     ddr.Tolerance{MM: o.addrTolMM, PS: o.addrTolPS},
		ClockOffsetPercent: o.clockPct,
		IncludeControl:     o.withCtrl,
		MaxIntraPairFix:    o.maxPairFix,
		GroupToleranceMM:   o.groupTol.m,
		StrobeToClock:      ddr.Tolerance{MM: o.strobeClockMM},
		MaxChipDeltaMM:     o.chipDeltaMM,
	}
	plan, err := ddr.BuildPlan(iface, engine, rules)
	if err != nil {
		return err
	}

	style := tune.Style{
		MaxAmplitude:    o.maxAmp,
		MinAmplitude:    o.minAmp,
		Chamfer:         o.chamfer,
		Gap:             o.gap,
		MinRunLength:    o.minRun,
		KeepClearOfPads: o.padKeep,
	}
	// Measuring the headroom probes the design rules along every candidate
	// track, so it is the slow half. It runs anyway: without it a shortfall
	// gives no clue whether the answer is to open space or to reroute.
	surveyor := newTuner(b, proj, style, iface, o)
	plan.MeasureHeadroom(surveyor)

	report.Topology(w, plan.Chain)
	if len(o.areas) > 0 {
		fmt.Fprintf(w, "\nCopper may be added inside %d region(s) and nowhere else:\n", len(o.areas))
		for i, r := range o.areas {
			fmt.Fprintf(w, "  %d. %.1f x %.1f mm at (%.1f, %.1f)%s\n",
				i+1, r.MaxX-r.MinX, r.MaxY-r.MinY, r.MinX, r.MinY, spacingNote(r))
		}
		fmt.Fprintln(w, "  Room outside them is not counted and nothing will be written there.")
	}
	report.Plan(w, plan)
	report.IntraPairSkew(w, plan)
	report.Checks(w, plan)
	report.Layers(w, b, plan)
	report.Headroom(w, plan, surveyor)

	if o.route {
		reqs := ddr.MissingHops(b, iface, plan)
		if len(reqs) == 0 {
			fmt.Fprintln(w, "\nNothing to route: the fly-by chain is complete.")
		} else {
			fmt.Fprintf(w, "\nWould route %d missing connection(s) of the fly-by chain.\n", len(reqs))
		}
	}

	if o.expand {
		// Work out what spreading would open on a throwaway copy, so a report
		// can say so without the board having been touched.
		preview, err := board.Load(path)
		if err != nil {
			return err
		}
		preview.UseHeightForLength = proj.UseHeightForLength
		results, err := expandBuses(preview, proj, iface, plan, style, o)
		if err != nil {
			return err
		}
		report.Expansion(w, results, !o.apply)
	}

	// What would change.
	targets := selectTargets(plan, o)
	if len(targets) == 0 {
		fmt.Fprintln(w, "\nNothing to change: every selected net is already within tolerance.")
		return nil
	}
	var total float64
	for _, t := range targets {
		total += t.need
	}
	fmt.Fprintf(w, "\nWould lengthen %d net(s) by %.3f mm in total.\n", len(targets), total)

	if !o.apply {
		fmt.Fprintln(w, "\nNothing was written. Re-run with -apply to make these changes.")
		return nil
	}

	if !o.yes {
		ok, err := confirm(w, in, len(targets), total)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(w, "Cancelled. Nothing was written.")
			return nil
		}
	}

	if o.route {
		reqs := ddr.MissingHops(b, iface, plan)
		if len(reqs) > 0 {
			fmt.Fprintln(w, "\nRouting the missing hops:")
			results, err := routeHops(b, proj, reqs, o, w)
			if err != nil {
				return err
			}
			report.Routes(w, results)

			// New copper changes every length on those nets, so everything
			// after this has to be measured again rather than carried over.
			plan, err = ddr.BuildPlan(iface, netlen.New(b), rules)
			if err != nil {
				return err
			}
			report.Topology(w, plan.Chain)
			targets = selectTargets(plan, o)
		}
	}

	if o.expand {
		fmt.Fprintln(w, "\nSpreading the buses:")
		results, err := expandBuses(b, proj, iface, plan, style, o)
		if err != nil {
			return err
		}
		report.Expansion(w, results, false)

		// Moving a trace lengthens it, so every requirement has changed. Measure
		// again rather than tuning against figures that are now stale.
		plan, err = ddr.BuildPlan(iface, netlen.New(b), rules)
		if err != nil {
			return err
		}
		targets = selectTargets(plan, o)
		if len(targets) == 0 {
			fmt.Fprintln(w, "\nNothing left to change: spreading the buses was enough.")
			return finish(w, b, path, o, nil)
		}
	}

	tuner := newTuner(b, proj, style, iface, o)
	fmt.Fprintln(w, "\nApplying:")
	var results []*tune.Result
	for _, t := range targets {
		res, err := tuner.Tune(t.net, t.need, t.tracks)
		if err != nil {
			return fmt.Errorf("tuning %s: %w", t.net, err)
		}
		res.Leg = legOf(plan, t.group)
		results = append(results, res)
	}
	report.Changes(w, results)

	// Re-measure from the edited board, so the numbers reported are the
	// board's and not the tuner's own bookkeeping.
	fmt.Fprintln(w, "\nAfter tuning, re-measured:")
	after, err := ddr.BuildPlan(iface, netlen.New(b), rules)
	if err != nil {
		return err
	}
	for _, g := range after.Groups {
		fmt.Fprintf(w, "  %-28s spread %.3f mm, %d of %d out of tolerance\n",
			g.Name, g.SpreadBefore(), g.OutOfTolerance(), len(g.Members))
	}

	return finish(w, b, path, o, results)
}

// legOf names the span a group measures, for reporting a net that is matched
// over more than one. Empty for a group that is not a leg of a chain, which is
// every byte lane: there is nothing to disambiguate.
func legOf(p *ddr.Plan, group string) string {
	for _, g := range p.Groups {
		if g.Name == group {
			return g.Leg
		}
	}
	return ""
}

// finish writes the result and says how to check it.
func finish(w *os.File, b *board.Board, path string, o options, results []*tune.Result) error {
	var added, missing float64
	shortCount := 0
	for _, r := range results {
		added += r.Added
		missing += r.Shortfall
		if r.Shortfall > 1e-4 {
			shortCount++
		}
	}
	if o.dryRun {
		fmt.Fprintln(w, "\n-dry-run: nothing written.")
		return nil
	}
	out := o.out
	if out == "" {
		ext := filepath.Ext(path)
		out = strings.TrimSuffix(path, ext) + ".tuned" + ext
	}
	if err := b.Save(out); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nWritten to %s (%.3f mm added", out, added)
	if missing > 1e-4 {
		fmt.Fprintf(w, ", %.3f mm short on %d net(s)", missing, shortCount)
	}
	fmt.Fprintln(w, ")")
	fmt.Fprintf(w, "\nVerify with KiCad's own design rule check before using this board:\n")
	fmt.Fprintf(w, "  kicad-cli pcb drc --format json --output drc.json %s\n", out)
	return nil
}

// expandBuses spreads the buses the out-of-tolerance nets run in.
//
// It works group by group, not over the whole interface at once. A bus that
// happens to run two byte lanes side by side is not a unit for this purpose:
// its traces are matched against different references, so spreading them
// together opens room beside nets that did not want any while the ones that did
// are left in a bundle that has already been claimed.
//
// The requirements come from the plan, because they cap how far each trace may
// be moved: moving one lengthens it, and lengthening the longest member of a
// group raises the target for every other member of it.
func expandBuses(b *board.Board, proj *board.Project, iface *ddr.Interface, plan *ddr.Plan,
	style tune.Style, o options) ([]*tune.ExpandResult, error) {
	opt := tune.DefaultExpandOptions()
	opt.MaxDisplacement = o.maxDisp
	opt.MinGain = o.minOpen
	opt.Coupled = pairsOf(iface)

	tuner := newTuner(b, proj, style, iface, o)
	var out []*tune.ExpandResult
	for _, g := range plan.Groups {
		var nets []string
		need := map[string]float64{}
		for _, m := range g.Members {
			if !m.Routed {
				continue
			}
			nets = append(nets, m.Net)
			if m.Need > need[m.Net] {
				need[m.Net] = m.Need
			}
		}
		if len(nets) == 0 {
			continue
		}
		res, err := tuner.Expand(nets, need, opt)
		if err != nil {
			return out, err
		}
		for _, r := range res {
			r.Group = g.Name
		}
		out = append(out, res...)
	}
	return out, nil
}

type target struct {
	net   string
	need  float64
	group string

	// tracks restricts the meander to the copper the measured route runs
	// over, so length is added where it shortens the path being matched and
	// not to a stranded island.
	tracks map[string]bool
}

// selectTargets picks the nets to tune, applying the -only filters.
//
// A net can appear in more than one group -- a clock is in the address group of
// every leg -- so the largest requirement wins, which is the one that satisfies
// them all.
func selectTargets(p *ddr.Plan, o options) []target {
	want := func(g *ddr.Group, net string) bool {
		if o.onlyGroup != "" && !strings.EqualFold(g.Name, o.onlyGroup) {
			return false
		}
		if o.onlyNets == "" {
			return true
		}
		for _, s := range strings.Split(o.onlyNets, ",") {
			s = strings.TrimSpace(s)
			if s != "" && (net == s || strings.HasSuffix(net, s)) {
				return true
			}
		}
		return false
	}
	// One target per member, not per net.
	//
	// A fly-by net is measured over each leg of the chain separately, so one
	// net can be short on U3->U4 and short again on U4->U5, with different
	// copper to fold the length into each time. Keeping only its worst leg --
	// which is what this did -- fixes that leg and leaves the other exactly as
	// short as it was, on a bus where the whole point is that each leg matches
	// the clock over that same span. On the second demo board that was 8 nets
	// and 16.781 mm of length the board had room for and never got.
	best := map[string]target{}
	for _, g := range p.Groups {
		for _, m := range g.Members {
			if !m.Routed || m.Need <= 1e-6 || m.InTolerance {
				continue
			}
			if !want(g, m.Net) {
				continue
			}
			key := m.Net + "\x00" + g.Name
			if cur, ok := best[key]; !ok || m.Need > cur.need {
				on := make(map[string]bool, len(m.PathTracks))
				for _, u := range m.PathTracks {
					on[u] = true
				}
				best[key] = target{net: m.Net, need: m.Need, group: g.Name, tracks: on}
			}
		}
	}
	out := make([]target, 0, len(best))
	for _, t := range best {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].need != out[j].need {
			return out[i].need > out[j].need
		}
		return out[i].net < out[j].net
	})
	return out
}

func confirm(w *os.File, in *os.File, n int, total float64) (bool, error) {
	// Reading a board and writing to one are not the same kind of claim, and
	// this is the half that writes. It is checked against KiCad's own DRC on
	// the boards here and behaves; what it has not had is a fabricated board
	// coming back and measuring right.
	fmt.Fprint(w, "\nWriting copper is the experimental half of this tool: run KiCad's DRC on\n"+
		"the result and look at the copper before you build anything from it.\n")
	fmt.Fprintf(w, "\nApply these changes to %d net(s), adding %.3f mm? [y/N] ", n, total)
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

// pairsOf maps each half of a differential pair to the other.
func pairsOf(iface *ddr.Interface) map[string]string {
	out := map[string]string{}
	for net, sig := range iface.Signals {
		if sig.Pair != "" {
			out[net] = sig.Pair
		}
	}
	return out
}

// newTuner builds a tuner holding to the open-field clearance, if one was asked
// for. Every tuner in this flow is built here, so the room measured in the
// report is the room the tuner will actually have.
func newTuner(b *board.Board, proj *board.Project, style tune.Style,
	iface *ddr.Interface, o options) *tune.Tuner {
	t := tune.NewTuner(b, proj, style)
	t.SetRules(o.dru)
	t.SetAreas(o.areas.areas())
	if o.openClr > 0 {
		t.SetOpenSpacing(drc.OpenSpacing{
			Clearance: o.openClr,
			Margin:    o.openMargin,
			Coupled:   pairsOf(iface),
		})
	}
	return t
}

// routeHops lays the copper.
func routeHops(b *board.Board, proj *board.Project, reqs []route.Request, o options, w *os.File) ([]*route.Result, error) {
	opt := route.DefaultOptions()
	opt.ViaCost = o.viaCost
	opt.MaxRipUp = o.ripUp
	if o.routeLayers != "" {
		for _, l := range strings.Split(o.routeLayers, ",") {
			if l = strings.TrimSpace(l); l != "" {
				opt.Layers = append(opt.Layers, l)
			}
		}
	}
	opt.Rules = o.dru
	r, err := route.New(b, proj, reqs, opt)
	if err != nil {
		return nil, err
	}
	nx, ny, nl := r.Cells()
	fmt.Fprintf(w, "  searching %d x %d cells at %.3f mm on %d layer(s)\n", nx, ny, r.Pitch(), nl)
	return r.Route(reqs)
}

// reportInterfaceDetail prints one interface in full, when asked for by name or
// by kind.
func reportInterfaceDetail(w *os.File, found []*proto.Interface, m proto.Measurer, want string) {
	if want == "" {
		return
	}
	for _, i := range found {
		if strings.EqualFold(i.Name, want) || strings.EqualFold(string(i.Kind), want) {
			report.InterfaceDetail(w, i, m)
		}
	}
}

// groupTolList collects repeated -group-tol flags.
//
// Written name=mm, because a group name has spaces and slashes in it
// ("address/command U3->U4") and the last = is the only separator that cannot
// appear in one.
type groupTolList struct{ m map[string]float64 }

func (g *groupTolList) String() string { return fmt.Sprintf("%d group tolerance(s)", len(g.m)) }

func (g *groupTolList) Set(v string) error {
	i := strings.LastIndexByte(v, '=')
	if i <= 0 {
		return fmt.Errorf("expected \"group name=mm\", got %q", v)
	}
	name := strings.TrimSpace(v[:i])
	mm, err := strconv.ParseFloat(strings.TrimSpace(v[i+1:]), 64)
	if err != nil || mm <= 0 {
		return fmt.Errorf("the tolerance in %q is not a positive number of millimetres", v)
	}
	if g.m == nil {
		g.m = map[string]float64{}
	}
	g.m[name] = mm
	return nil
}

// areaList collects repeated -area flags.
//
// In the board's own coordinates, which is what a command line user has in
// front of them in KiCad. The web front end works in the viewer's and converts;
// here there is nothing to convert from.
type areaList []tune.Area

// areas is the list as the tuner wants it.
func (a areaList) areas() tune.Areas { return tune.Areas(a) }

func (a *areaList) String() string { return fmt.Sprintf("%d area(s)", len(*a)) }

func (a *areaList) Set(v string) error {
	// Split rather than scan. Sscanf with four verbs takes the first four
	// numbers and says nothing about the rest, so "10,20,30,40,50" came
	// through as a rectangle -- and somebody who typed five numbers has made a
	// mistake, which silently using four of them turns into a restriction they
	// did not ask for.
	parts := strings.Split(strings.TrimSpace(v), ",")
	if len(parts) != 4 {
		return fmt.Errorf("expected x0,y0,x1,y1 in millimetres, got %q", v)
	}
	var n [4]float64
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return fmt.Errorf("expected x0,y0,x1,y1 in millimetres, got %q", v)
		}
		n[i] = f
	}
	x0, y0, x1, y1 := n[0], n[1], n[2], n[3]
	r := geom.Rect{
		MinX: math.Min(x0, x1), MinY: math.Min(y0, y1),
		MaxX: math.Max(x0, x1), MaxY: math.Max(y0, y1),
	}
	if r.MaxX-r.MinX < 1e-6 || r.MaxY-r.MinY < 1e-6 {
		return fmt.Errorf("%q has no extent", v)
	}
	*a = append(*a, tune.Area{Rect: r})
	return nil
}

// spacingNote describes an area's own spacing, where it has one.
func spacingNote(a tune.Area) string {
	if a.MinClearance <= 0 {
		return ""
	}
	return fmt.Sprintf(", at least %.3f mm between traces", a.MinClearance)
}
